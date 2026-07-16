# Brief — 完全对齐 OpenAI Compact V1/V2

## Goal

- 让 new-api 完整代理 OpenAI Codex Compact V1、V2 HTTP/SSE 与 Responses WebSocket，并支持 `Codex 客户端 -> new-api -> sub2api` 的 WS 上游拓扑，同时保持现有渠道、模型映射、重试、计费和日志安全契约。

## Scope

- 建立统一 Compact 模式：V1 path、历史 V1 body bridge、V2 HTTP、V2 WebSocket，并贯穿分发、RelayInfo、模型映射、计费和日志。
- 校准 V1 `/v1/responses/compact` canonical body、安全 Codex 请求头、OpenAI/Codex/Azure URL、JSON 响应、usage/cache usage 和计费。
- 保持 V2 原生 `/v1/responses` 流式协议，完整转发 `compaction_trigger`、beta feature、原始 compaction item、`encrypted_content` 和 `response.completed`。
- 为没有 V2 feature 的历史 body-signal 请求提供 unary Compact 到 Responses SSE 的桥接、心跳和失败终态。
- 新增 `GET /v1/responses` WebSocket：Upgrade 后读取首个 `response.create`，再执行模型权限、Compact 识别、渠道选择、映射和预扣。
- 将每个客户端 WS 一对一直连到所选 Channel 的上游 Responses WS；Channel Base URL 指向 sub2api 时连接其 `/v1/responses`，替换认证并补齐 beta/turn/session 元数据。
- 支持连接内顺序多轮 `response.create`，每轮独立识别普通/Compact、预扣、usage 结算或退款；首个业务事件前允许切换支持 WS 的渠道，之后禁止 failover。
- 补齐 HTTP、SSE bridge、WS、多轮计费、取消、错误、并发和 race 回归测试，并更新 Relay Compact 规范。

## Non-Goals

- 不在 new-api 本地生成上下文摘要或 compaction 内容。
- 不修改 OpenAI Codex、OpenAI 服务端或 sub2api 代码。
- 不复制 sub2api 的账号数据库、能力探测、账号调度或跨客户端 WS 连接池。
- 不把普通 HTTP/SSE 请求自动转换为池化上游 WS，也不把已建立的客户端 WS 降级成单次 HTTP 请求。
- 不修改普通 Responses 的工具计费、视觉辅助或 Chat Completions 转换语义，不新增数据库迁移或前端配置。

## Key Context

- 当前 new-api 只有 `/v1/realtime` WebSocket，没有 `GET /v1/responses`；现有 WS 渠道选择依赖 query model，而 Responses WS 的 model 位于首个 `response.create` 帧。
- Compact 本地价格模型使用 `-openai-compact` 后缀；V2 上游仍是普通 `/responses`，必须把“上游路径”和“Compact 计费标记”分离，禁止后缀泄漏上游。
- 当前默认请求头不转发 `x-codex-beta-features`；sub2api 上游需要 `OpenAI-Beta: responses_websockets=2026-02-06` 及 beta/turn/session 元数据。
- 历史 bridge 的 SSE 心跳不能复用 JSON 空白保活；心跳提交 200 后只能用 `response.failed` 表达失败。
- WS 多轮必须每轮清理模型、Compact 模式和 BillingSession；已写业务帧后切换渠道会产生不可合并的双流。
- 协议事实基线为 OpenAI Codex commit `800715d201651a2a07c2706dca10400109dae3d3`；sub2api 只作为网关 wire 和故障处理参考。

## Acceptance

- V1 canonical 字段、必要请求头、URL、原始 JSON、usage/cache usage 和 Compact 计费在 OpenAI/Codex/Azure 链路中不丢失。
- V2 HTTP/SSE 保持原生 `/responses`，`compaction_trigger`、`remote_compaction_v2`、compaction item 和 `response.completed` 可端到端通过。
- `GET /v1/responses` 能完成 new-api 鉴权和首帧分发，并将 WS 安全转发到 sub2api；认证被替换，beta/turn/session 元数据和多轮语义保持。
- 普通与 Compact WS turn 可在同一连接顺序执行并分别计费；握手失败只在首个业务事件前切换 WS 渠道，取消/失败/断连不会重复结算。
- 历史 body-signal 客户端获得合法 SSE 终态，不因 JSON/SSE 错配无限重连。
- 普通 Responses、Chat Completions via Responses、视觉辅助、JSON 保活、Realtime WS 和渠道重试不回归。
- 定向测试、关键 `-race`、`go test ./...`、`go vet ./...` 和 `git diff --check` 通过；既有基线失败必须证明与任务无关。

## Next Step

- 用户确认 planning artifacts 与本 brief 后，运行 `task.py start` 激活任务；随后必须通过 `trellis-route(implement)` 选择实现执行方式，不能直接修改代码。
