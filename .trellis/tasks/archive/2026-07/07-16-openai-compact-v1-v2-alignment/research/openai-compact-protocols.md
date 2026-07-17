# OpenAI Compact V1/V2 与 Responses WebSocket 调研

## 1. 调研基线

- 调研日期：2026-07-16。
- OpenAI Codex 源码基线：`800715d201651a2a07c2706dca10400109dae3d3`。
- 网关兼容参考：`/root/project/my/sub2api` 当前 `build` 工作树。
- new-api 基线分支：`build-bak`。

协议事实以 OpenAI Codex 源码为第一优先级；sub2api 用于验证真实网关如何处理路径、请求头、SSE/WS 生命周期和历史兼容问题。

## 2. OpenAI Codex 当前协议

### 2.1 V1 `/responses/compact`

OpenAI Codex 的 canonical `CompactionInput` 当前包含：

- `model`
- `input`
- `instructions`
- `tools`
- `parallel_tool_calls`
- `reasoning`
- `service_tier`
- `prompt_cache_key`
- `text`

源码：`codex-rs/codex-api/src/common.rs:24-42`。

V1 请求使用 `POST /responses/compact`，是带完整请求超时的 unary JSON 调用。客户端从最终 JSON 的 `output` 中读取压缩结果，不使用 Responses SSE。

### 2.2 V2 原生 Responses

V2 在普通 Responses 请求历史末尾追加：

```json
{"type":"compaction_trigger"}
```

源码：`codex-rs/core/src/compact_remote_v2_attempt.rs:68-110`。

请求继续走普通 Responses stream，客户端收集 `response.output_item.done`，并要求：

- 必须收到 `response.completed`。
- 在所有 output item 中必须恰好存在一个 `type=compaction`。

源码：`codex-rs/core/src/compact_remote_v2.rs:333-425`。

`remote_compaction_v2` 在本次源码基线中为 stable 且默认开启。Session 会把该 feature 放入 `x-codex-beta-features`。

### 2.3 Responses WebSocket

OpenAI provider 当前声明 `supports_websockets:true`。WebSocket 使用 `GET /responses` 握手，握手请求头至少包括：

- `OpenAI-Beta: responses_websockets=2026-02-06`
- `x-codex-beta-features`，其中 V2 压缩包含 `remote_compaction_v2`
- `originator`
- `session-id`、`thread-id`；sub2api 兼容入口仍使用 `session_id`、`thread_id`
- `x-client-request-id`
- Codex turn/session 兼容元数据

源码：

- `codex-rs/model-provider-info/src/lib.rs:330-363`
- `codex-rs/core/src/client.rs:1071-1104`

首个业务帧是 `response.create`，payload 与 HTTP `ResponsesApiRequest` 基本同构，包含 model、input、tools、reasoning、store、stream、include、service_tier、prompt_cache_key、text、client_metadata 等字段。连接可以复用多个顺序执行的 `response.create` turn。

## 3. new-api 当前能力与缺口

### 3.1 已有能力

- 已有 `POST /v1/responses` 和 `POST /v1/responses/compact`。
- V1 已有 OpenAI-compatible、Codex、Azure 上游 URL、usage 解析、Compact 模型后缀和文本计费。
- 普通 Responses DTO 已能承载 V2 当前核心字段，SSE handler 写回时使用原始 data 字符串，因此未知的 `encrypted_content` 不会因 DTO 缺字段而被重新 marshal 丢失。
- 已有 Gorilla WebSocket 依赖、`/v1/realtime` 入站、上游拨号和双向转发基础设施。
- 已有跨重试 BillingSession、Compact 价格后缀和 Checked quota 机制。

### 3.2 确定缺口

- 没有 `GET /v1/responses` WebSocket 路由。
- 现有 WebSocket 仅处理 Realtime；其模型来自 query，并在 Upgrade 前由 `middleware.Distribute` 选渠道。Responses WS 的模型位于首个 `response.create` 帧，不能复用该假设。
- 默认上游请求头只复制 `Content-Type` 和 `Accept`；`x-codex-beta-features` 依赖人工 Header Override。
- V2 普通 `/responses` 不会获得 Compact 后缀，渠道选择、模型映射和计费会按普通 Responses 处理。
- `ModelMappedHelper` 仅按 `RelayModeResponsesCompact` 去除本地后缀；如果只给 V2 的模型加后缀，会把后缀错误发送给普通 `/responses` 上游。
- 没有历史 body-signal 提升、unary JSON 到 Responses SSE 桥接、桥接心跳和终态错误处理。
- 现有 `PostWssConsumeQuota` 面向 Realtime audio usage，不能直接作为多轮 Responses WS 的结算实现。

## 4. sub2api 可复用的协议经验

### 4.1 HTTP/SSE Compact

- 使用 `stream:true + compaction_trigger + remote_compaction_v2` 区分原生 V2，并保持 `/responses` 原生流式链路。
- 非 V2 body-signal 可提升到 `/responses/compact`。
- 历史流式客户端等待 unary Compact 时发送 SSE 注释心跳；最终把 JSON 合成为 `response.output_item.done` 和 `response.completed`。
- 心跳提交 200 后，失败必须转换成 `response.failed`，不能再写 JSON 状态码。

### 4.2 Responses WebSocket

- `GET /responses` Upgrade 后先读取首个 `response.create`，再解析 model、选择账号/渠道和建立上游连接。
- `https/http` Base URL 分别转换为 `wss/ws`。
- 上游添加 `OpenAI-Beta: responses_websockets=2026-02-06`，并安全透传 beta feature、turn state、turn metadata、session 等请求头。
- 上游认证使用自身账号/渠道凭证，不透传客户端 Authorization。
- 对握手失败且尚未写出下游业务帧的情况允许 failover；写出业务帧后不再切换上游。
- 同一连接按 `response.completed` 等终态逐轮提取 usage。

sub2api 还实现了跨 HTTP 请求的上游 WS 连接池、账号能力探测和复杂账号调度。new-api 没有对应账号模型，且当前需求是把客户端 WS 直连到作为上游的 sub2api，因此这些内部结构不应复制。

## 5. 选定方案

### 5.1 协议标记

在 new-api 内引入稳定的 Compact 模式概念，而不是只依赖 URL：

- `none`
- `v1_path`
- `v1_body_bridge`
- `v2_http`
- `v2_websocket`

该标记需要贯穿 middleware、RelayInfo、模型映射、上游 URL、响应处理、计费和日志。

### 5.2 WebSocket 拓扑

采用一对一直连：

```text
Codex Client WS
    -> GET /v1/responses on new-api
    -> 读取首个 response.create
    -> 选择 new-api Channel
    -> GET /v1/responses on sub2api via ws/wss
    -> 双向转发，多轮顺序 response.create
```

不实现：

- 跨客户端上游连接池。
- 把普通 HTTP/SSE 请求自动转换成上游 WS。
- sub2api 的账号探测和账号调度模型。

### 5.3 计费

- HTTP V1/V2 继续复用主 Relay BillingSession。
- WS 每个 `response.create` 是一个独立计费 turn：请求发上游前预扣，`response.completed` 后按 usage 结算，`response.failed`、取消或断连时退款/按已有违规费规则处理。
- 同一连接保持同一渠道；每轮重新解析 model、Compact 模式和价格，禁止继承上一轮计费状态。
- 首次上游握手或首帧发送失败可在未写下游业务帧前切换渠道；之后不 failover。

## 6. 相关项目规范

- `.trellis/spec/backend/relay-alpha-search-compact.md`
- `.trellis/spec/backend/relay-nonstream-keepalive.md`
- `.trellis/spec/backend/error-handling.md`
- `.trellis/spec/backend/logging-guidelines.md`
- `.trellis/spec/backend/quality-guidelines.md`
- `.trellis/spec/backend/directory-structure.md`
