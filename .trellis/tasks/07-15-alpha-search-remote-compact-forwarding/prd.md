# 补全 Alpha Search 与远程压缩上游透传

## Goal

新增 Codex CLI standalone `alpha/search` 上游转发和按次计费，并修复现有 Responses Compact 请求遗漏三个官方字段的问题；保持 Compact 既有路由、渠道范围、URL、响应解析和 usage 计费不变。

## Background

- GitHub issue #6114 说明 Codex CLI 在 `web_search = "live"` 下会请求 `POST /v1/alpha/search`；当前 [router/relay-router.go](../../../router/relay-router.go) 未注册该路径，因此请求落入 `RelayNotFound` 并返回 404。
- 官方 Codex `SearchRequest` 至少包含 `id`、`model`，并可能携带仍在演进的 `input`、`commands`、`settings`、`max_output_tokens` 等字段。Alpha Search 不能通过封闭 DTO 重建完整 body。
- [middleware/distributor.go](../../../middleware/distributor.go) 已能从 JSON body 读取 `model` 并执行令牌模型限制、分组、渠道亲和性和渠道选择，Alpha Search 应复用该链路。
- `/v1/responses/compact` 已完成路由、请求校验、OpenAI/Codex 上游 URL、响应 usage 解析和计费，不需要重做。
- 当前 Compact DTO 已声明官方字段 `tools`、`reasoning`、`text`，但 [relay/responses_handler.go](../../../relay/responses_handler.go) 重建上游请求时没有复制它们，导致字段被静默丢弃。
- standalone `alpha/search`、Responses hosted `web_search` tool、Claude WebSearch 本地模拟是三条独立链路，本任务不得混用。

## Requirements

### R1. Alpha Search 路由与分发

- 注册 `POST /v1/alpha/search`，经过现有 `SystemPerformanceCheck`、`TokenAuth`、`ModelRequestRateLimit` 和 `Distribute`。
- 从请求体读取并校验非空字符串 `model`，使用原始模型完成令牌模型限制与渠道选择。
- 请求体中的 `max_output_tokens` 必须复用项目现有上限，避免无界数值绕过 Relay 校验。
- 不设置渠道 API 类型白名单；凡是通过 `Distribute` 模型能力筛选的渠道都尝试转发，由上游判断协议支持。

### R2. Alpha Search 请求与响应透传

- 待转发 body 来自原始 `BodyStorage`；只写入最终模型映射并应用现有 Param Override，其余未知字段、嵌套结构和显式零值保持。
- Codex 渠道转发到 `/backend-api/codex/alpha/search`；Advanced Custom 使用匹配 route；其它渠道沿用自身 Base URL 约定转发 `/v1/alpha/search`。
- 保留入站 query 并安全合并到上游 URL。
- 使用所选渠道的认证、Header Override、代理和超时设置；不得把客户端 Authorization 原样泄露给上游。
- 上游 `2xx` 成功响应保留原始状态码、body 和允许透传的响应头；跳过 hop-by-hop、`Content-Length` 和本实例 Request ID。
- 非 `2xx` 在响应提交前进入现有 Relay 错误解析、状态码映射和渠道重试，最终错误保持统一 Relay 格式。

### R3. Responses Compact 官方字段补全

- 保留现有 `POST /v1/responses/compact` 路由、请求 DTO、APITypeOpenAI/APITypeCodex 限制和 adaptor 调用链。
- 将 Compact DTO 中已解析的 `tools`、`reasoning`、`text` 复制到现有 `OpenAIResponsesRequest` 上游请求对象。
- 保持现有 `model`、`input`、`instructions`、`previous_response_id`、`parallel_tool_calls`、`service_tier`、`prompt_cache_key`、`prompt_cache_options` 和 `prompt_cache_retention` 转发行为。
- 不改变 Codex `/backend-api/codex/responses/compact`、普通 OpenAI-compatible `/v1/responses/compact` 或 Azure Compact URL 规则。
- 不改变 Compact 响应解析、usage 提取、预扣、结算和消费日志。

### R4. Alpha Search 计费与日志

- `alpha/search` 不从上游响应猜测 token usage，也不收输入或输出 token 费用。
- 上游最终返回 `2xx` 时按 1 次现有 `web_search` 工具单价收费，默认沿用 `$10 / 1000 次`。
- 请求上游前按固定工具费用预扣；非 `2xx`、网络失败、校验失败和最终重试失败必须退款且不记录成功搜索费用。
- 复用 BillingSession、用户/渠道用量更新和消费日志；quota 换算使用 Checked helper 并保留饱和审计。
- 日志只记录模型、渠道、状态、耗时和工具计费元数据，不记录搜索查询、响应 body 或凭证。

### R5. 回归测试

- Alpha Search 覆盖路由模式、模型必填、`max_output_tokens` 上限、模型映射、未知字段、显式零值、query、鉴权头替换和安全响应头。
- Alpha Search 覆盖普通渠道、Codex 渠道、Advanced Custom、成功响应、可重试错误、最终失败退款和成功按次收费。
- Compact 覆盖 `tools`、`reasoning`、`text` 均发送到上游，并验证原有字段、URL、usage 和计费不回归。
- 普通 `/v1/responses`、Responses hosted WebSearch 和 Claude WebSearch 行为不回归。

## Acceptance Criteria

- [ ] Codex CLI 请求 `POST /v1/alpha/search` 不再返回本地 404，并能按 `model` 选中渠道后访问正确上游路径。
- [ ] Alpha Search 保留未知字段、query 和显式零值，复用模型映射、请求头覆盖和重试，且不泄露客户端认证头。
- [ ] Alpha Search 仅对最终 `2xx` 请求按 1 次现有 `web_search` 单价收费，不虚构 token，失败不收费。
- [ ] `/v1/responses/compact` 将 `tools`、`reasoning`、`text` 与原有字段一并发送到上游。
- [ ] Compact 既有渠道限制、URL、响应 usage 和计费行为保持不变。
- [ ] 定向测试、`go test ./...`、`go vet ./...` 和 `git diff --check` 通过。
- [ ] 最终提交标题携带 `[build]`，用于触发 build 构建。

## Out Of Scope

- 修改 Compact 渠道 API 类型白名单、Base URL 拼接、请求透传架构、响应解析或计费。
- 为 Compact 保留 DTO 未声明的任意未来字段；未来官方字段按实际契约单独补充。
- 修改 Claude WebSearch 本地模拟或 Responses hosted `web_search` tool。
- 为不支持 `alpha/search` 的上游实现本地搜索降级。
- 新增 Alpha Search token 计费公式、独立价格配置或无 `/v1` 入站别名。
- 针对特定中转服务修改 Base URL、账号调度、计费或部署配置。
