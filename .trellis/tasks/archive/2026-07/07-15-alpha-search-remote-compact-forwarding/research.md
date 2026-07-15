# Alpha Search 与远程压缩字段补全调研

## 官方契约

- Codex live web search 通过 provider Base URL 请求相对路径 `alpha/search`。
- Search 请求包含 `model`，并可能携带持续演进的 `input`、`commands`、`settings`、`max_output_tokens`，适合最小解析加原始 body 透传。
- 官方当前 `CompactionInput` 包含 `model`、`input`、`instructions`、`tools`、`parallel_tool_calls`、`reasoning`、`service_tier`、`prompt_cache_key`、`text`。

## 当前代码事实

- `/v1/alpha/search` 未注册，因此 issue #6114 返回本地 404。
- `Distribute` 已能从 JSON body 提取模型并执行现有渠道选择。
- `/v1/responses/compact` 已具备路由、校验、OpenAI/Codex 上游 URL、响应 usage 和计费。
- Compact DTO 已声明 `tools`、`reasoning`、`text`，但 `ResponsesHelper` 构造的 `OpenAIResponsesRequest` 未复制这三个字段。
- `setting/operation_setting/tools.go` 已定义 `web_search` 默认 `$10 / 1000 次`。
- `ComputeToolCallQuota` 可计算固定工具费用但尚无调用方；`PostTextConsumeQuota` 会在总 token 为 0 时把 quota 归零，因此 Alpha Search 需要独立的纯工具结算。

## 已确认决策

- Alpha Search 不设置渠道 API 类型白名单，使用原始 body 透传、现有渠道鉴权和错误重试。
- Alpha Search 最终 `2xx` 按 1 次现有 `web_search` 收费，不收 token，失败退款。
- Compact 仅补 `tools`、`reasoning`、`text` 三个字段赋值。
- Compact 不改 API 类型白名单、Base URL、原始 JSON 透传、响应解析、usage 或计费。
- 不为任何特定中转服务增加专项逻辑。
