# 完全对齐 OpenAI Compact V1/V2

## Goal

使 new-api 能稳定代理 OpenAI Codex 当前的两代远端上下文压缩协议，并保持现有渠道分发、模型映射、重试、计费和日志契约：

- V1：`POST /v1/responses/compact`，上游返回单个 JSON 响应。
- V2：`POST /v1/responses`，请求以 `stream:true`、`input[].type=compaction_trigger` 和 `x-codex-beta-features: remote_compaction_v2` 标识，响应使用原生 Responses 流。
- 对历史 body-signal 客户端提供可验证的兼容行为，避免把 V2 错误改写成 V1，或让客户端收到不符合其消费协议的响应。

本任务以规划时锁定的 OpenAI Codex 上游源码为协议事实源，以 `/root/project/my/sub2api` 为网关兼容实现参考；出现冲突时优先遵循 OpenAI 当前协议，再按 new-api 架构落地，禁止机械复制 sub2api 的账号模型。

## Background

- 当前 new-api 已注册 `/v1/responses` 与 `/v1/responses/compact`，但 Compact DTO 的 `IsStream` 固定返回 `false`，现有专用链路只覆盖 V1。
- 当前 V1 请求会复制 `model/input/instructions/tools/parallel_tool_calls/reasoning/service_tier/prompt_cache_key/prompt_cache_options/prompt_cache_retention/text/previous_response_id`，并分别转发到 OpenAI-compatible、Codex 和 Azure Compact URL。
- OpenAI Codex 当前 V1 canonical payload 包含 `model/input/instructions/tools/parallel_tool_calls/reasoning/service_tier/prompt_cache_key/text`。
- OpenAI Codex 当前 V2 会在普通 Responses 请求尾部加入 `compaction_trigger`，通过流式 Responses 请求获取结果，并要求在 `response.completed` 前恰好收到一个 `type=compaction` 的输出项。
- OpenAI Codex 会通过 `x-codex-beta-features` 宣告 `remote_compaction_v2`；当前 new-api 默认请求头构造只复制 `Content-Type` 和 `Accept`，该特性头必须依赖人工 Header Override 才可能到达上游。
- 当前 new-api 的普通 Responses DTO 可以承载 V2 的核心 body 字段，但没有 V2 显式识别、Compact 专用计费标记、兼容分支和端到端回归测试。
- sub2api 已区分原生 V2 与历史 body-signal：原生 V2 保持 `/responses` 流式链路；非 V2 body-signal 可提升为 `/responses/compact`，必要时把 unary JSON 合成为 Responses SSE。
- 实际部署拓扑包含 `Codex 客户端 -> new-api -> sub2api`：new-api 不仅要接受 Responses WebSocket，还要把所选渠道的上游连接建立到 sub2api 的 Responses WebSocket 端点；当前 new-api 只有 `/v1/realtime` WebSocket，尚无 `GET /v1/responses` 支持。
- 已归档的 `07-15-alpha-search-remote-compact-forwarding` 任务只补齐 V1 字段映射，并明确不重做 Compact 路由、响应和计费，本任务是后续完整协议对齐。
- 首轮实现把所有 Compact 模式统一追加 `-openai-compact` 后缀用于渠道选择，导致仅配置基础模型 `gpt-5.6-sol` 的普通 Responses 渠道无法承接 V2，请求在分发阶段以 `No available channel for model gpt-5.6-sol-openai-compact` 返回 `503`。这与 V2 复用普通 Responses 能力的要求冲突。

## Requirements

### R1. 协议事实源与版本边界

- 规划和实现必须记录用于对齐的 OpenAI Codex commit，相关请求字段、请求头、路径和事件语义均需有源码或测试依据。
- sub2api 只作为兼容行为和故障处理参考；不得照搬其账号表、调度器或与 new-api 分层不兼容的结构。
- 新增协议字段时优先使用原始 JSON 或 `json.RawMessage` 保持未知字段和显式零值；不得用封闭 DTO 静默丢失 Codex 正在演进的字段。

### R2. V1 `/responses/compact` 请求对齐

- 保留 `POST /v1/responses/compact`、OpenAI/Codex API 类型限制、模型映射、Compact 模型价格后缀和现有上游 URL 规则。
- V1 canonical 字段至少完整转发 `model`、`input`、`instructions`、`tools`、`parallel_tool_calls`、`reasoning`、`service_tier`、`prompt_cache_key`、`text`。
- 明确处理当前额外兼容字段 `previous_response_id`、`prompt_cache_options`、`prompt_cache_retention`：只有上游契约或既有兼容性证据支持时才发送，禁止无依据扩展官方请求。
- Compact 请求必须保持非流式 unary JSON 语义；成功响应原样返回，并继续解析 `input_tokens/output_tokens/total_tokens/cache tokens` 用于现有结算。
- V1 必须转发 Compact 所需的安全 Codex 元数据请求头，包括 beta feature、turn state、turn metadata、installation/session/thread 标识中经确认需要的部分，同时禁止泄露客户端认证头。
- V1 非流式等待继续遵守现有 JSON 空白保活、响应提交、重试和错误体规范。

### R3. V2 原生 Responses Compact

- 仅在裸 `/v1/responses` 上识别 V2；识别条件至少包含：`stream:true`、`input` 数组存在 `type=compaction_trigger`、`x-codex-beta-features` 的逗号分隔值包含 `remote_compaction_v2`。
- 原生 V2 必须保持 `/v1/responses` 上游路径和流式请求，不得重写为 `/v1/responses/compact`，不得删除 `stream`、`compaction_trigger`、`client_metadata`、`include`、`tool_choice`、`prompt_cache_key` 等正常 Responses 字段。
- 默认安全转发 `x-codex-beta-features` 及经确认的 Codex turn/session 元数据头，不要求管理员为官方客户端手工配置 Header Override。
- 上游 SSE 事件必须保留原始 JSON item，尤其是 `response.output_item.done.item.type=compaction`、`encrypted_content` 和 `response.completed`；不得因本地 DTO 缺少字段而丢失内容。
- V2 识别结果必须进入 RelayInfo，使模型映射、Compact 专用价格键、日志和测试可以区分普通 Responses 与远端压缩请求；V2 的权限校验、渠道亲和性、首次选择和重试使用基础模型，上游模型名不得携带本地计费后缀。
- 客户端取消、上游断流、缺少 `response.completed`、零个或多个 `compaction` 输出项必须有明确的转发/日志契约；网关不得制造成功结果或无限重试。

### R4. V2 Responses-over-WebSocket

- 本任务同时覆盖 Responses-over-WebSocket 入站与上游转发，不能只完成 HTTP/SSE 后宣称 V2 完全对齐。
- 注册 `GET /v1/responses` WebSocket 入口，与现有 `POST /v1/responses` 共存；非 Upgrade 请求必须返回明确的 `426` 或既定 OpenAI 错误，不得误入 HTTP POST handler。
- WebSocket 握手必须复用现有 Token 鉴权、模型限制、分组和渠道选择语义，并安全替换上游认证；不得透传客户端 `Authorization`、Cookie 或 hop-by-hop 握手凭证。
- `response.create` payload 中的 `stream:true`、`input[].type=compaction_trigger`、`client_metadata` 和其他 Responses 字段必须原样保留；V2 识别结果应与 HTTP/SSE 使用同一套协议判定和 Compact 计费标记。
- new-api 必须在完成客户端 WS Upgrade 后限时读取并校验首个 `response.create` 帧，从中取得 `model` 和 V2 Compact 信号，再执行令牌模型权限、渠道选择、模型映射和预扣；不能沿用 Realtime 在 Upgrade 前从 query 读取模型的假设。
- WebSocket V2 的令牌模型权限、渠道亲和性、首次选择、连接内后续 turn 能力校验和 failover 重试均使用 `response.create.model` 的基础模型；Compact 后缀只能在 RelayInfo 内形成计费模型，不能参与渠道可用性判断。
- 所选渠道指向 sub2api 时，上游使用 `ws://`/`wss://` 对应 scheme 连接其 `GET /v1/responses`（或渠道显式配置的等价 Responses WS 路径），并以渠道 API Key 替换客户端凭证。
- 对 sub2api 必须转发或补齐 `x-codex-beta-features`、`OpenAI-Beta: responses_websockets=2026-02-06`、turn state、turn metadata、originator、session/thread 等经证实需要的安全元数据；上游返回的新 turn state 等允许头必须回传或用于连接内后续帧。
- 入站 WebSocket 到上游 WebSocket 的消息、控制帧、关闭码、取消和错误终态必须有明确映射；不能把普通 Responses WebSocket 错误包装成成功 compaction。
- 每个客户端连接直接对应一个上游连接，并允许连接内多轮 `response.create`；不实现跨客户端共享连接池，也不把普通 HTTP/SSE 请求自动转换成上游 WebSocket。
- 上游不支持 WebSocket、握手失败或会话不可复用时，只能在尚未向客户端写出业务事件时切换到另一个支持 Responses WS 的渠道；不允许把已经建立的客户端 WebSocket 静默降级成无法继续双向通信的单次 HTTP 请求。
- WebSocket 会话中的 turn state、session/thread/window 元数据和粘性渠道必须在同一压缩请求生命周期内保持一致，禁止跨用户或跨会话复用。
- 若引入连接池，连接所有权、并发请求隔离、取消清理、凭证刷新和故障剔除必须可测试；不得为了复用 sub2api 结构而绕过 new-api 既有 Channel 生命周期。

### R5. 历史 body-signal 兼容

- 对没有显式宣告 `remote_compaction_v2`、但在裸 `/v1/responses` 中携带 `compaction_trigger` 的请求定义唯一兼容行为，禁止把原生 V2 和历史兼容流混为同一路径。
- 若选择提升为 V1，必须在上游请求前完成路径、模型、计费和 body 归一化，并保留当前 Codex Compact canonical 字段。
- 历史客户端原始请求为 `stream:true` 时，下游必须收到合法 Responses SSE 终态，至少包含每个有效输出的 `response.output_item.done` 和最终 `response.completed`；失败时不得留下“stream closed before response.completed”的无终态连接。
- unary-to-SSE 桥接、心跳和最终响应写入必须串行化，不能污染现有非流式 JSON 保活或渠道重试判定。

### R6. 渠道、模型映射与计费

- `v1_path` 与 `v1_body_bridge` 继续使用带 `-openai-compact` 后缀的选择模型，只选择支持 `/responses/compact` 的 OpenAI/Codex 渠道。
- `v2_http` 与 `v2_websocket` 复用基础模型的普通 Responses 渠道；令牌模型权限、渠道亲和性、首次选择和跨渠道重试必须始终使用同一个基础模型名，不能要求管理员在渠道模型列表中额外配置 `*-openai-compact`。
- V1/V2 的本地计费模型名使用统一 Compact 价格后缀或等价价格解析规则；模型映射完成后，`UpstreamModelName` 保持真实模型名，冻结计费模型使用映射结果对应的 Compact 后缀。
- 预扣与结算必须覆盖输入、输出、缓存读取和缓存写入 token，继续使用现有 Checked quota helper 与饱和审计。
- WebSocket 连接允许多轮 `response.create`；每轮独立提取终态 usage 并结算，但复用同一客户端连接、上游连接和粘性渠道。Compact 与普通 Responses 帧必须按各轮实际信号选择对应计费模型，禁止沿用上一轮状态。
- 跨渠道重试不得重复预扣；流式响应已经写出业务事件后不得切换渠道。
- 日志应标记 Compact 协议版本、入口路径、最终上游路径、渠道和结局，不记录对话、压缩密文、请求体或凭证。

### R7. 测试与回归

- V1 覆盖 canonical 字段、兼容字段策略、OpenAI/Codex/Azure URL、请求头、模型映射、非流式响应、usage/cache usage 和计费。
- V2 覆盖严格识别条件、逗号分隔 beta features、原生路径保持、请求体字段保持、请求头转发、SSE compaction item 原样输出、completed 终态、usage 和 Compact 计费。
- WebSocket 覆盖握手鉴权、`response.create` V2 识别、双向消息、控制帧、关闭码、取消、上游失败、粘性状态、HTTP/SSE 回退边界和计费只结算一次。
- V2 HTTP/WS 必须覆盖“分组和渠道仅配置基础模型、未配置 Compact 后缀模型”的真实分发形态，并验证首次选择、token model limit、亲和性、后续 turn 和 retry 均不会查询后缀模型。
- 历史 body-signal 覆盖 unary 提升、流式桥接、心跳前后错误、缺失/重复 compaction item、客户端取消和响应写入并发。
- 覆盖普通 `/v1/responses` 不误判、普通流式 Responses 不回归、V1/V2 不互相污染模型后缀和计费。
- 定向测试必须包含 `go test` 和关键 writer/流式路径的 `-race`；最终执行 `go test ./...`、`go vet ./...` 与 `git diff --check`。

## Acceptance Criteria

- [ ] 当前 OpenAI Codex 的 V1 `/responses/compact` 请求可以通过 new-api 到达 OpenAI、Codex 和 Azure 对应上游，canonical 字段、必要请求头、响应 body、usage 和计费均不丢失。
- [ ] 当前 OpenAI Codex 的 V2 请求保持普通 `/responses` 流式协议，`compaction_trigger`、beta feature、compaction 输出项和 `response.completed` 能端到端通过。
- [ ] 当 `vip` 等分组只为渠道配置 `gpt-5.6-sol` 时，V2 HTTP/WS Remote Compact 能正常选择和重试该渠道，不再因查询 `gpt-5.6-sol-openai-compact` 返回 `503`；结算仍使用 Compact 价格模型。
- [ ] Responses-over-WebSocket 的 V2 Compact 请求可以经过 new-api 完成鉴权、渠道选择、上游转发、终态回传和计费；上游握手失败时只在首个业务事件前切换支持 WS 的渠道，不执行 WS 到单次 HTTP 的协议降级。
- [ ] 当渠道 Base URL 指向 sub2api 时，new-api 能连接 sub2api 的 Responses WS 入口，替换认证并保持 beta/turn/session 元数据和多轮 `response.create` 语义。
- [ ] V2 不再依赖管理员手工 Header Override 才能工作，且客户端认证信息不会被透传到上游。
- [ ] V1/V2 都使用正确的 Compact 计费模型，预扣、结算、缓存 token 和失败退款符合现有安全不变量。
- [ ] 历史 body-signal 客户端获得确定且可测试的兼容响应，不会因 JSON/SSE 协议错配无限重连。
- [ ] 普通 Responses、Chat Completions via Responses、视觉辅助、非流式 JSON 保活和渠道重试行为不回归。
- [ ] 任务相关定向测试、race 测试、`go test ./...`、`go vet ./...` 和 `git diff --check` 通过；若存在既有基线失败，必须证明与本任务无关。

## Out Of Scope

- 实现本地上下文摘要或替代 OpenAI 服务端生成 `compaction` 内容。
- 修改 OpenAI Codex 客户端、OpenAI 服务端或 sub2api 代码。
- 照搬 sub2api 的账号数据库、账号探测、调度和管理端模型；new-api 只实现与自身 Channel 架构匹配的能力表达。
- 实现 sub2api 的跨客户端 Responses WS 连接池或把 HTTP/SSE 请求转换为池化上游 WS；本任务采用客户端连接到上游连接的一对一直连。
- 改变普通 Responses 的工具计费、视觉辅助或 Chat Completions 转换语义。
