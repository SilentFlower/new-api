# Brief — Claude Code WebSearch 渠道支持

## Goal

- 为不原生支持 Claude Code `web_search` 的三方渠道增加渠道级 WebSearch 模拟能力，可选择 Tavily 或 AnySearch，并保证不破坏请求体稳定性和缓存命中。

## Scope

- 在 `model.Channel.Setting` / `dto.ChannelSettings` 下新增渠道级 `web_search` 配置，包括启用状态、供应商、可选/必填 API Key、最大结果数和供应商参数。
- 管理 API 返回渠道列表/详情时脱敏 `web_search.api_key`，只返回 `api_key_configured`；编辑渠道时空 key 沿用旧 key，显式清空必须通过明确字段表达。
- 复制渠道必须保留 WebSearch 供应商、参数和已配置的真实 API Key，复制后的渠道可继续执行 WebSearch。
- 新增 Tavily 与 AnySearch provider，统一输出 `url`、`title`、`snippet`、可选 `page_age`。
- 在 Claude relay 上游请求体构造前识别并短路“纯 web_search 请求”，调用 provider 后本地构造 Claude Messages 响应。
- 设置 `claude_web_search_requests=1` 并复用现有 Claude WebSearch 工具计费逻辑。
- 默认前端渠道表单增加 WebSearch 配置控件，并补齐 `en`、`zh`、`fr`、`ja`、`ru`、`vi` 翻译。

## Non-Goals

- 不支持 WebSearch 与普通函数工具混合请求的本地工具循环编排。
- 不要求所有非 Claude Code 格式一次性支持 WebSearch。
- 不接入 AnySearch 的 extract、batch_search、vertical_search。
- 不复刻 sub2api 的全局供应商池、账号级三态覆盖、额度权重和代理故障切换。
- 不做数据库迁移，复用渠道 `setting` JSON。

## Key Context

- `dto/claude.go` 已有 `ClaudeWebSearchTool` 和 `ClaudeUsage.ServerToolUse.WebSearchRequests`。
- `relay/channel/claude/relay-claude.go` 已把 Claude 响应中的 `server_tool_use.web_search_requests` 写入 `claude_web_search_requests`，`service/text_quota.go` 已据此计费。
- `relay/claude_handler.go` 非透传路径是 `ConvertClaudeRequest` → `common.Marshal` → `RemoveDisabledFields` → `ApplyParamOverride` → `NewOutboundJSONBody`；WebSearch 模拟必须在这之前短路，不能把搜索结果、时间戳或随机 ID 注入待转发 body。
- `controller/channel.go` 的 `CopyChannel` 使用 `model.GetChannelById(id, true)` 读取完整渠道并浅拷贝；脱敏只能发生在响应副本上，不能写回模型或数据库。
- JSON 编解码必须使用 `common.*` 包装；管理 API 错误是 HTTP 200 + `{success:false,message:"..."}`，relay 错误按 Claude/OpenAI 兼容格式返回。
- Tavily 启用时必须配置 API Key；AnySearch API Key 可选。配置后都通过 `Authorization: Bearer <api_key>` 请求头发送，不放入供应商 JSON 请求体。

## Acceptance

- 可以按渠道开启/关闭 Claude Code WebSearch 增强，并选择 Tavily 或 AnySearch。
- 渠道未开启、provider 配置错误或查询提取失败时返回清晰且不可重试的 relay 错误。
- 只有 `tools` 恰好包含一个搜索工具时才触发本地模拟；混合工具请求保持原转发路径。
- 纯 WebSearch 短路不生成上游请求体；混合工具路径不被改变，保证请求体稳定。
- 管理 API 不返回明文 WebSearch API Key；编辑不输入 key 不会丢失旧 key；复制渠道保留已配置的真实 key。
- Tavily 与 AnySearch provider 响应能规范化为统一结果结构。
- 前端表单可配置 WebSearch，且 i18n 六种语言完整。
- 单元测试覆盖配置解析、请求体稳定、纯请求识别、provider 规范化、错误路径、脱敏、编辑 key 保留和复制保留。

## Next Step

- 等用户确认 planning artifacts 和本 brief 后，运行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/07-01-claude-code-websearch-channel-support`，再进入 Phase 2 的实现路由门禁。
