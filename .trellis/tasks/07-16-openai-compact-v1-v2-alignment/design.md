# OpenAI Compact V1/V2 完整对齐设计

## 1. 设计目标

在不复制 sub2api 账号体系和连接池的前提下，让 new-api 对外兼容 OpenAI Codex 当前 Compact V1、V2 HTTP/SSE 与 Responses WebSocket，并能把 WebSocket 上游连接发送到配置为 Channel Base URL 的 sub2api。

核心不变量：

1. Compact 协议版本不能只靠路径判断。
2. 本地 Compact 价格后缀只能用于渠道选择和计费，不能发给上游。
3. V2 原生请求必须保持普通 `/responses` 流式协议。
4. WebSocket 在读取首个 `response.create` 前不能选择渠道。
5. 已向客户端写出业务事件后不能切换渠道。
6. 每个请求或 WS turn 只能预扣、结算或退款一次。

## 2. 协议模型

### 2.1 Compact 模式

新增稳定领域类型，建议放在 `relay/constant`：

```go
type ResponsesCompactMode string

const (
	ResponsesCompactModeNone         ResponsesCompactMode = ""
	ResponsesCompactModeV1Path       ResponsesCompactMode = "v1_path"
	ResponsesCompactModeV1BodyBridge ResponsesCompactMode = "v1_body_bridge"
	ResponsesCompactModeV2HTTP       ResponsesCompactMode = "v2_http"
	ResponsesCompactModeV2WebSocket  ResponsesCompactMode = "v2_websocket"
)
```

`RelayInfo` 增加 Compact 模式和下游流式意图。V1 path 继续使用 `RelayModeResponsesCompact`；V2 HTTP 保持 `RelayModeResponses`；V1 body bridge 的上游模式是 Compact，但需要单独保存客户端原始 `stream:true`。

### 2.2 统一检测

在 `relay/helper` 建立可直接测试的协议检测函数，输入为 path、transport、请求头和原始 body，输出 Compact 模式。检测顺序：

1. `POST /v1/responses/compact` -> `v1_path`。
2. 裸 `POST /v1/responses`，存在 `compaction_trigger`：
   - `stream:true` 且 beta feature 精确包含 `remote_compaction_v2` -> `v2_http`。
   - 否则 -> `v1_body_bridge`。
3. `GET /v1/responses` WebSocket 首帧，存在相同三个信号 -> `v2_websocket`。
4. 其他请求 -> `none`。

beta feature 必须按多个 header value 和逗号拆分后 trim 精确匹配，禁止 substring 判断。

请求 JSON 使用 `common.Unmarshal` 或现有结构化 JSON API；不得直接调用 `encoding/json` marshal/unmarshal。输入 item 保持原始 JSON，检测只读取 `type`。

## 3. HTTP 路径

### 3.1 分发前识别

`middleware.getModelRequest` 已读取可复用 BodyStorage。其返回前执行 Compact 检测：

- 将模式写入 context。
- V1/V2 Compact 的渠道选择模型统一追加 `-openai-compact`。
- token 模型限制和渠道能力继续按现有 Compact 后缀契约执行。
- 普通 Responses 不加后缀。

为避免 detector、controller 和 relay 重复解析，context 只保存枚举结果，不保存请求体或对话内容。

### 3.2 RelayInfo 与模型映射

`GenRelayInfoResponses` 和 `GenRelayInfoResponsesCompaction` 从 context 读取 Compact 模式。

`ModelMappedHelper` 从“仅判断 RelayMode”调整为“判断 RelayInfo 是否属于任一 Compact 模式”：

- 映射输入前去除本地 Compact 后缀。
- 链式模型映射使用真实模型名。
- `UpstreamModelName` 保持真实模型名。
- `OriginModelName`/冻结计费模型保持 Compact 后缀。

V2 的 URL 仍由 `RelayModeResponses` 决定，不能因计费标记改成 `/responses/compact`。

### 3.3 V1 canonical body

V1 上游请求使用明确 allowlist，和锁定的 OpenAI Codex `CompactionInput` 一致：

```text
model, input, instructions, tools, parallel_tool_calls,
reasoning, service_tier, prompt_cache_key, text
```

当前额外字段按兼容策略处理：

- `previous_response_id`：只在有明确目标上游兼容证据或渠道显式 Param Override 时发送；Codex canonical body 默认不发送。
- `prompt_cache_options`、`prompt_cache_retention`：不属于当前 Codex canonical V1，默认不发送到 Codex `/compact`；普通 OpenAI-compatible 渠道若历史行为必须保留，应由定向兼容测试证明并在 adaptor 分支处理。

V1 仍使用 `OaiResponsesCompactionHandler` 原样返回 JSON并提取 usage。

### 3.4 V2 原生 HTTP/SSE

V2 保持普通 Responses DTO、普通 `/responses` 上游路径和 `stream:true`。请求构造必须保留当前官方字段及未知 input item。

请求头增加安全的 Codex allowlist：

- `x-codex-beta-features`
- `x-codex-turn-state`
- `x-codex-turn-metadata`
- `x-codex-installation-id`
- `x-codex-window-id`
- `x-codex-parent-thread-id`
- `x-client-request-id`
- `session-id`
- `thread-id`
- `session_id`
- `thread_id`
- `originator`
- `user-agent`

Codex 官方客户端使用 `session-id` / `thread-id`，sub2api 历史入口使用
`session_id` / `thread_id`。网关必须以官方连字符值优先，并向上游同时发送两组别名，
避免客户端到 sub2api 的会话粘性信息丢失。

客户端 `Authorization`、Cookie、Host、Content-Length 和 WebSocket 握手专用头不进入通用透传。

现有 Responses SSE handler继续转发原始 `data`；解析 DTO 只用于 usage、工具调用和观测，不能重新 marshal item。对 V2 增加以下观测状态：

- `response.output_item.done` 总数。
- `item.type=compaction` 数量。
- 是否看到 `response.completed`。

原生 V2 的协议合法性最终由 Codex 客户端判断。网关在已流式写出后不伪造替代 compaction；只记录异常并按流式错误契约结束。

## 4. 历史 body-signal 桥接

### 4.1 上游请求

`v1_body_bridge` 在 relay 内转换为 V1 upstream：

- 上游路径改为 `/responses/compact`。
- canonical allowlist 移除 `stream`、`store`、`include`、`client_metadata` 等普通 Responses 请求级字段。
- 保存客户端原始流式意图，不能让 `RelayInfo.IsStream` 同时代表上下游两个不同协议。

### 4.2 下游 SSE writer

新增请求级 Compact SSE bridge，不能复用 JSON 空白保活 writer：

- 首次心跳写 `Content-Type:text/event-stream`、`X-Accel-Buffering:no` 和 SSE 注释行。
- 心跳、最终事件、错误事件共享同一 mutex。
- 停止函数幂等，并等待 goroutine 退出后才允许 Gin 回收 context。
- 心跳字节不计入“是否已经写业务响应”的 failover 判断。

成功 JSON 转换为：

1. 每个 JSON object 类型的合法 `output[]` item 对应一个 `response.output_item.done`；标量项跳过。
2. 最后发送携带完整 response 的 `response.completed`。

`response.completed.response.usage` 缺失时可补零；若 usage 已存在但缺少 Codex 必需的
`input_tokens`、`output_tokens`、`total_tokens` 数字字段，则删除整个 usage，避免 Codex
把 completed 事件判为不可解析。

若心跳已提交 200 后发生失败，发送 `response.failed` 终态；若尚未提交，保留现有 HTTP 错误状态和 OpenAI JSON 错误。`response.completed.response.id` 和 usage 必填字段缺失时只做客户端解析所需的最小修补，不修改 compaction 密文。

## 5. Responses WebSocket

### 5.1 路由和握手

在 `router/relay-router.go` 增加 `GET /v1/responses`。该路由复用：

- `RouteTag("relay")`
- `SystemPerformanceCheck`
- `TokenAuth`
- `ModelRequestRateLimit`

不挂载现有 `Distribute`，因为 model 尚未出现。

新增独立导出控制器，例如：

```go
func RelayResponsesWebSocket(c *gin.Context)
```

控制器职责：

1. 校验 Upgrade。
2. Upgrade 客户端连接并设置读大小和首帧超时。
3. 读取首个 text/binary JSON 帧，要求 `type=response.create` 和非空 model。
4. 检查 token 模型权限、分组、渠道亲和性和 Compact 后缀。
5. 选择并初始化 Channel context。
6. 建立上游 WS，转发保留的首帧。

现有 `middleware.Distribute` 中“检查 token 模型权限 + 首次渠道选择 + context 初始化”应抽取为可复用的稳定分发能力，HTTP middleware 和 Responses WS controller 共用，避免两套选择语义漂移。

### 5.2 上游 URL

支持范围：

- OpenAI-compatible Channel：`<base>/v1/responses`。
- Codex Channel：`<base>/backend-api/codex/responses`。
- Advanced Custom：仅当显式 route 支持 `/v1/responses` 且能构造 WS URL 时使用。

URL scheme：

- `https -> wss`
- `http -> ws`
- `wss/ws` 保持
- 其他 scheme 拒绝

Base URL 指向 sub2api 时，默认 OpenAI-compatible 规则得到其 `GET /v1/responses` 入口。query 合并沿用渠道固定 query 优先原则。

### 5.3 上游请求头

上游请求头按 adaptor 构造，然后应用 Header Override。必须：

- 用 Channel API Key 设置 `Authorization`。
- 设置 `OpenAI-Beta: responses_websockets=2026-02-06`。
- 转发 V2 beta feature 与安全 Codex session/turn 元数据。
- 保留或规范化 `originator`、`user-agent`。
- 不复制客户端 Authorization、Cookie 和由 Dialer 生成的握手头。

握手响应中的 `x-codex-turn-state`、`x-request-id` 等允许字段用于后续帧上下文和日志；需要回传客户端的字段在 Upgrade 响应阶段设置，不能等到业务帧之后修改 HTTP header。

### 5.4 双向转发

每个客户端连接对应一个上游连接。使用两个读取循环和每个方向单写者约束：

- client -> upstream：保留 text/binary payload；每个 `response.create` 做协议、model、计费和顺序状态校验。
- upstream -> client：先解析事件 envelope 做 usage/终态/失败判定，再原样写回 payload。
- Ping/Pong、close code 和 context 取消按 Gorilla WebSocket 约束处理，所有关闭路径幂等。

连接内只允许一个 active turn。下一条 `response.create` 只能在前一轮 `response.completed`、`response.failed` 或显式取消终态后开始；否则以 policy violation 关闭，防止多个 BillingSession 和 usage 交叉。

### 5.5 failover

可以切换渠道的阶段：

- 上游 WS 握手失败。
- 首帧写上游失败。
- 上游在任何业务事件写给客户端前返回可重试错误。

不能切换渠道的阶段：

- 已写任意上游业务事件给客户端。
- 当前 turn 已收到部分输出。
- 客户端已取消或连接已关闭。

重试复用缓存的首个 `response.create`，并复用同一预扣会话；最终失败退款。

## 6. WebSocket 多轮计费

每个 turn 建立独立状态：

```text
idle
  -> response.create 校验
  -> 解析/映射 model 与 Compact 模式
  -> 估算 token、冻结价格、Reserve
  -> 发上游
  -> 转发事件并累计 usage
  -> completed: Settle + 消费日志
  -> failed/cancel/disconnect: Refund/错误日志
  -> idle
```

Responses WS 使用 `dto.Usage` 和现有文本计费，不使用 Realtime audio 的 `RealtimeUsage`/`PreWssConsumeQuota`。

`response.completed.response.usage` 作为最终 usage；缓存读取/写入字段沿用普通 Responses 映射。缺失 usage 时不得猜测成功费用；按现有无 usage 错误策略退款并记录异常。

普通 Responses turn 使用基础模型价格；只有该 turn 同时满足 V2 Compact 信号时才使用 Compact 后缀。连接级状态不能把 Compact 标记泄漏到下一轮。

## 7. 错误与日志

- Upgrade 前错误使用 OpenAI JSON 和真实 HTTP 状态。
- Upgrade 后客户端请求错误使用 OpenAI WS error event，并以合适 close code 关闭。
- 上游业务 `error`/`response.failed` 原样转发，同时触发本地退款和错误记录。
- 日志只记录 request id、Compact mode、model、channel、WS 阶段、事件类型、状态码、耗时和 token；不记录帧 body、input、encrypted_content、API Key、完整 URL/query。
- 可重试握手错误进入现有渠道错误和自动封禁判断；客户端主动关闭不应封禁渠道。

## 8. 兼容性和回滚

- 不需要数据库迁移或前端配置。
- 新 `GET /v1/responses` 与现有 POST 路由方法不同，可独立回滚。
- Compact detector 和 RelayInfo marker 可先落地并保持普通 Responses 行为不变。
- SSE bridge 独立于 JSON 非流式保活，可单独关闭/回滚。
- WS 直连不引入共享连接池，回滚时没有跨请求资源需要迁移。

## 9. 主要风险

1. 分发时序：WS model 在首帧，必须避免绕过 token 模型限制和渠道亲和性。
2. 模型后缀泄漏：V2 path 是普通 `/responses`，但价格是 Compact；模型映射必须分离两者。
3. writer 提交：历史 bridge 心跳提交 200 后只能使用 SSE 终态表达失败。
4. 多轮计费：连接级变量若未按 turn 清理，会重复结算或把 Compact 价格带入普通请求。
5. failover 边界：任何业务帧写出后重试都会形成两个不可合并的上游流。
6. 请求头：缺少 beta/session 元数据会让 sub2api 无法识别 V2；通配透传又可能泄露凭证。
