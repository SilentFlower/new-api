# 扩展 WebSearch 模拟到 Chat Completions 技术设计

## Architecture

本任务在现有渠道级 WebSearch 模拟上增加 OpenAI Chat Completions 协议适配，不改变渠道配置、provider 协议和数据库结构。实现分为三层：

1. `relay/websearch` 负责 Chat 纯 WebSearch 识别、最后一条 user 消息查询提取、文本摘要复用和 Chat 响应对象构造。
2. `relay` 包负责渠道配置校验、代理 HTTP Client、provider 调用、超时、错误映射、流式输出和结算。
3. `relay/compatible_handler.go` 只保留一个窄入口，在正式转换或发送上游请求前决定是否本地短路。

为避免复制 Claude handler 中 provider 校验和调用逻辑，提取同 package 的通用渠道搜索执行函数，由 Claude 与 Chat 两个协议 handler 复用。Claude 和 Chat 的请求识别、响应构造仍分别保留，避免协议结构互相污染。

## Trigger Contract

Chat 本地模拟仅在以下条件全部成立时触发：

- `RelayMode` 为 `RelayModeChatCompletions`。
- `GeneralOpenAIRequest.WebSearchOptions != nil`。
- `Tools` 为空。
- 旧式 `Functions` 不包含实际函数定义；缺失、`null` 或空数组视为空。
- 渠道 `ChannelSetting.WebSearch.Enabled=true`。

渠道未启用、请求包含普通工具或不是 Chat Completions 时继续现有路径，不调用本地 provider，不返回本地配置错误。渠道启用后，即使全局或渠道开启请求透传，本地模拟仍优先执行，与现有 Claude 模拟的显式渠道开关语义一致。

## Data Flow

1. `TextHelper` 深拷贝并完成模型映射、stream options 归一化和 adaptor 初始化。
2. Chat WebSearch predicate 判断请求是否为纯搜索并检查渠道开关。
3. 查询提取器只读取最后一条 `role=user` 消息；字符串内容直接 trim，数组内容只拼接 `type=text` 的文本块。
4. 通用渠道搜索执行函数归一化并校验 `ChannelWebSearchSettings`，创建代理 Client，以 30 秒超时调用现有 Tavily 或 AnySearch provider。
5. 使用映射后的 `UpstreamModelName`，为空时回退请求模型；输入 Token 优先使用 `RelayInfo` 已估算值，输出 Token 复用当前文本摘要估算。
6. 非流式构造 `chat.completion`；流式依次输出 assistant 起始块、摘要文本块、`finish_reason=stop`、可选 usage 块和 `[DONE]`。
7. 成功后记录一次现有 `web_search` 工具调用并调用 `service.PostTextConsumeQuota`。

## Response Contract

非流式响应：

- `id` 使用当前请求 ID 构造稳定的 `chatcmpl-*` ID。
- `object=chat.completion`。
- `choices[0].message.role=assistant`。
- `choices[0].message.content` 使用现有 `BuildTextSummary` 文本。
- `choices[0].finish_reason=stop`。
- `usage` 使用本地估算值。

流式响应：

- 第一块设置 `delta.role=assistant` 和空 content。
- 第二块发送完整文本摘要，不人为拆分不稳定片段。
- 第三块发送 `finish_reason=stop`。
- `RelayInfo.ShouldIncludeUsage=true` 时发送现有 final usage chunk。
- 最后发送 `[DONE]`。

首版不返回 citation / annotations，也不暴露 Claude 的 `server_tool_use` 或 `web_search_tool_result` 内容块。

## Billing

计费严格复用现有 Claude 模拟行为：

- 输入 Token 使用请求预估，缺失时以查询字符数兜底。
- 输出 Token 使用稳定文本摘要长度估算。
- 工具附加费继续使用现有内部 `claude_web_search_requests` 计数入口记录一次 `web_search`，避免同时写入 Responses 工具计数造成重复收费。
- 成功响应后调用 `PostTextConsumeQuota`，provider 失败时不进入成功结算。

本任务不重命名现有内部计数键，避免扩大 Claude、Responses 和消费日志的回归范围；后续可单独治理命名。

## Compatibility And Safety

- 不修改原始 `GeneralOpenAIRequest`；本地结果只进入响应对象。
- 本地短路位于 Chat-to-Responses、参数覆盖和上游 body 构造前，防止结果污染 prompt cache 或透传 body。
- 关闭渠道 WebSearch 时保持当前原生上游能力和失败行为。
- provider 配置和查询提取错误返回 400 并跳过重试；provider 调用、解析或空响应返回 502 并跳过重试。
- 错误和日志不得包含 API Key、完整 provider body 或完整用户对话。
- `relaykit/` 只复用现有 DTO，不新增依赖根模块的代码，因此不改变其独立构建边界。

## Validation Strategy

- `relay/websearch` 表驱动测试：纯 Chat WebSearch 识别、`tools`/`functions` 排除、字符串与文本块查询提取、请求对象不变、非流式响应字段。
- `relay` 测试：渠道开关 predicate、流式事件顺序、usage 开关、模型名和 Token 兜底、成功计费只记录一次。
- 现有 Claude WebSearch 测试必须继续通过，确保通用 provider 调用提取没有改变 Claude 响应和错误行为。
- 回归命令：`go test ./relay/websearch ./relay ./service`，完成前运行 `go test ./...`。

## Rollback

回滚时移除 `TextHelper` 的 Chat WebSearch 窄入口和 Chat 协议文件即可恢复原行为。若通用 provider 执行函数导致 Claude 回归，可将 Claude handler 恢复为原内联调用；渠道配置和数据库没有迁移，无数据回滚要求。
