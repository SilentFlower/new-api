# Brief — 扩展 WebSearch 模拟到 Chat Completions

## Goal

- 为 OpenAI `/v1/chat/completions` 增加渠道级 WebSearch 本地模拟，使不原生支持联网搜索的渠道复用现有 Tavily / AnySearch 能力并返回 Chat Completions 兼容响应。

## Scope

- 仅识别携带 `web_search_options` 且不包含 `tools` 或旧式 `functions` 的纯 Chat WebSearch 请求。
- 渠道启用 WebSearch 后，在 Chat-to-Responses、adaptor 转换、参数覆盖和上游 body 构造前本地短路。
- 复用现有渠道 WebSearch 配置、代理、Tavily / AnySearch provider、30 秒超时、结果归一化和错误保护。
- 从最后一条 user 消息提取字符串或文本块查询，返回稳定文本摘要。
- 支持非流式 `chat.completion` 和流式 `chat.completion.chunk`、stop、可选 usage 与 `[DONE]`。
- 复用现有输入/输出 Token 估算、文本结算和单次 `web_search` 工具附加费。
- 补充触发、透传、查询提取、响应、流式、请求不变、错误和单次计费测试，并更新 WebSearch 模拟规范。

## Non-Goals

- 不扩展 OpenAI `/v1/responses` 格式。
- 不支持 WebSearch 与普通函数工具混合时的本地多轮工具编排。
- 不增加 citation / annotations，也不调用上游模型生成综合回答。
- 不新增搜索供应商、全局供应商池、数据库迁移或渠道前端配置。
- 不改变 Claude Messages WebSearch 模拟的客户端协议行为。

## Key Decisions

- Chat 首版严格保持现有 Claude 模拟语义：纯搜索、本地 provider、文本摘要、无模型二次生成。
- `web_search_options` 是 Chat 模拟的唯一明确触发字段；存在普通工具时继续现有上游路径。
- 渠道未启用 WebSearch 时始终透传；显式启用后本地模拟优先于透传与协议转换。
- 工具费用继续复用现有内部 `claude_web_search_requests` 计数入口，避免与 Responses 工具计数重复收费；本任务不治理该内部命名。

## Key Context

- Chat 入口位于 `relay/compatible_handler.go` 的 `TextHelper`；短路应接在模型映射、stream options 和 adaptor 初始化之后，Chat-to-Responses 分支之前。
- 当前 Claude 短路位于 `relay/claude_handler.go`，协议处理位于 `relay/claude_websearch_emulation.go`。
- provider、搜索结果和 Claude 响应辅助位于 `relay/websearch/`；实现将增加 Chat 协议辅助，并提取 Claude/Chat 共用的渠道 provider 执行逻辑。
- `relaykit/dto.GeneralOpenAIRequest` 已支持 `web_search_options`，本任务不修改 `relaykit/` 公共 API 或其独立构建边界。
- 必须遵守 `.trellis/spec/backend/relay-websearch-emulation.md` 的未启用透传、请求体稳定、密钥保护、错误和计费契约。

## Risks / Deferred

- 空或旧式 `functions` 判断错误可能误拦截混合工具请求。
- 短路位置过晚可能让搜索结果或转换状态污染上游请求路径。
- 同时写入 legacy WebSearch 计数与 Responses 工具计数会造成重复收费。
- 流式 stop、usage 或 `[DONE]` 缺失会导致客户端解析或等待异常。
- 通用 provider 执行逻辑的提取必须保持 Claude 当前状态码、跳过重试和响应行为；内部计数键重命名延后到独立治理任务。

## Acceptance

- 已启用渠道上的纯 Chat WebSearch 请求可以通过 Tavily 或 AnySearch 返回 Chat Completions 兼容文本摘要。
- 未启用渠道、包含普通工具或非 Chat 请求保持现有上游转发路径，不调用本地 provider。
- 字符串和文本块查询提取正确，非法查询返回明确 400；provider 失败返回受保护的 502。
- 非流式和流式响应结构完整，模型名、响应 ID、finish reason、usage 和 `[DONE]` 符合现有 Chat 行为。
- 本地模拟不修改原始请求或待转发 body，每次成功请求只结算一次 WebSearch 工具费。
- `go test ./relay/websearch ./relay ./service` 和 `go test ./...` 通过，现有 Claude WebSearch 测试无回归。

## Next Step

- 用户确认本 Brief 后运行 `task.py start`，再通过 `trellis-route(target=implement)` 进入实现。
