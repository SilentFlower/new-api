# 扩展 WebSearch 模拟到 Chat Completions

## Goal

为 OpenAI `/v1/chat/completions` 增加渠道级 WebSearch 本地模拟，使不原生支持联网搜索的渠道可以复用现有 Tavily / AnySearch 能力，并返回 Chat Completions 兼容响应。

## Background

- 当前渠道级 WebSearch 配置、密钥管理、Tavily / AnySearch provider 和结果归一化已经存在，无需新增数据库字段或供应商配置体系。
- 当前本地模拟只接入 Claude Messages：`relay/claude_handler.go` 在上游请求转换前识别纯 WebSearch 请求，并由 `relay/claude_websearch_emulation.go` 本地短路。
- `relaykit/dto.GeneralOpenAIRequest` 已定义 `web_search_options`，`relay/helper.GetAndValidateTextRequest` 已校验 `search_context_size`，但 `relay/compatible_handler.go` 当前只记录该参数，未执行本地搜索。
- OpenAI Chat 转 Claude 的转换器会把 `web_search_options` 转为 Claude `web_search_20250305` 工具，因此 Chat 请求已有稳定、明确的 WebSearch 触发字段。
- 现有 Claude 转 Chat 响应转换只保留文本内容，不会把 `server_tool_use` 和 `web_search_tool_result` 暴露为 Chat 结构化搜索结果。

## Key Decisions

- 首版严格复刻现有 Claude WebSearch 模拟语义：只处理纯 WebSearch 请求，本地调用搜索 provider 后直接返回稳定文本摘要，不再调用上游模型生成综合答案。
- Chat 的明确触发字段为 `web_search_options`；请求同时包含 `tools` 或旧式 `functions` 时不做本地模拟，继续现有转发路径。
- 首版不增加 citation / annotations 响应字段，不改变 Chat Completions DTO 的结构化引用协议。
- 渠道未启用 WebSearch 时继续透传；渠道显式启用后，本地模拟优先于 Chat-to-Responses、请求透传和 adaptor 转换。

## Requirements

- R1：渠道 `setting.web_search.enabled=true`，且 Chat 请求携带 `web_search_options`、不包含 `tools` 或旧式 `functions` 时，系统必须在上游请求转换和发送前调用现有渠道 WebSearch provider，并本地返回结果。
- R2：渠道未启用本地 WebSearch 模拟时，请求必须保持现有转发路径，不能被本地逻辑拦截为 400。
- R3：Chat WebSearch 必须复用现有 `ChannelWebSearchSettings`、代理配置、Tavily / AnySearch provider、超时、响应大小限制和密钥保护规则，不新增第二套配置或 provider 实现。
- R4：查询文本从最后一条 `role=user` 消息提取，支持字符串内容及文本内容块；无法提取查询时返回符合当前 OpenAI relay 错误模型的 400，并跳过重试。
- R5：本地短路必须发生在 Chat-to-Responses、`adaptor.ConvertOpenAIRequest`、参数覆盖和上游 body 构造之前。
- R6：搜索结果、响应 ID、时间戳和 provider 返回内容只能出现在响应侧，不能修改原始 `GeneralOpenAIRequest`、`RelayInfo.RequestBody` 或任何可能发送给上游的请求体。
- R7：非流式请求返回标准 `chat.completion` 响应；流式请求返回有效的 `chat.completion.chunk` 序列、结束块，并遵循 `TextHelper` 现有 `ShouldIncludeUsage` 语义决定是否返回 usage 块。
- R8：模型名优先使用映射后的上游模型名，缺失时回退到请求模型名；usage 使用当前请求 prompt token 估算和本地结果文本估算。
- R9：成功的本地 WebSearch 必须参与现有文本结算和工具附加费，且每次请求只能记录一次 WebSearch 工具调用，不能与 Claude/Responses 工具计数重复收费。
- R10：provider 配置错误返回 400，provider 调用或响应错误返回 502；错误信息和日志不得泄露 API Key、完整请求体或完整用户对话。
- R11：混合普通函数工具的本地工具循环编排不在本任务实现范围内；不满足本地模拟条件的请求继续走现有转发路径。
- R12：本任务不修改渠道管理前端、渠道配置存储和供应商参数，因为现有配置已经覆盖 Chat 模拟所需能力。

## Acceptance Criteria

- [ ] `/v1/chat/completions` 请求携带 `web_search_options`，目标渠道已启用 WebSearch 时，可以通过 Tavily 或 AnySearch 完成本地搜索并返回 Chat Completions 兼容响应。
- [ ] 请求同时包含 `tools` 或旧式 `functions` 时不触发本地模拟，继续现有转发路径。
- [ ] 渠道未启用 WebSearch 时，相同请求继续进入现有上游转发链路，不调用本地 provider。
- [ ] 最后一条 user 消息为字符串或文本块时可以稳定提取查询；非 user、空文本或无消息时返回明确的 400。
- [ ] 非流式响应包含稳定的响应 ID、模型、assistant 文本、`finish_reason=stop` 和 usage。
- [ ] 流式响应包含 assistant 起始块、文本内容、stop 结束块、可选 usage 块和 `[DONE]`。
- [ ] 本地模拟不会修改原始 Chat 请求对象，也不会把搜索结果写入任何待转发的上游请求体。
- [ ] provider、代理、认证和错误处理继续符合 `.trellis/spec/backend/relay-websearch-emulation.md` 的既有契约。
- [ ] 每次成功模拟只结算一次 WebSearch 工具费，消费日志中工具调用次数和价格正确。
- [ ] 单元测试覆盖触发条件、查询提取、未启用透传、provider 错误、非流式响应、流式响应、请求对象不变和单次计费。
- [ ] 相关 Go 定向测试通过；跨层改动完成前运行根模块全量测试。

## Non-Goals

- 不扩展 OpenAI `/v1/responses` 格式；Responses 已有独立的 hosted tool 和计费链路。
- 不实现 WebSearch 与普通函数工具混合时的本地多轮工具编排。
- 不增加 citation / annotations，也不让上游模型基于搜索结果生成综合回答。
- 不新增搜索供应商、全局供应商池、数据库迁移或渠道前端设置。
- 不改变 Claude Messages WebSearch 模拟的现有客户端协议行为。
