# Relay Alpha Search 与 Responses Compact 契约

> 记录 standalone Alpha Search 的透明上游转发、重试、纯工具计费和日志安全，以及 OpenAI Responses Compact V1/V2、历史 SSE bridge 和 Responses WebSocket 契约。

## 场景：Alpha Search 上游透传与 Compact 字段转发

### 1. Scope / Trigger

- Trigger：修改 `POST /v1/alpha/search` 的路由、请求校验、渠道分发、上游 URL、响应处理、重试、工具计费，或修改 `POST /v1/responses/compact` 的请求字段转换。
- Alpha Search 是 Codex standalone Search 上游协议；它不等于 Responses hosted `web_search` tool，也不等于 Claude WebSearch 本地模拟。
- Responses Compact 已有独立路由、渠道限制、上游 URL、usage 解析和文本计费；新增字段时只扩展请求映射，不重做整条链路。

### 2. Signatures

- 入口与最小请求 DTO：

```go
POST /v1/alpha/search

type AlphaSearchRequest struct {
	ID              *string `json:"id,omitempty"`
	Model           string  `json:"model"`
	MaxOutputTokens *uint   `json:"max_output_tokens,omitempty"`
}

func GetAndValidateAlphaSearchRequest(c *gin.Context) (*dto.AlphaSearchRequest, error)
func AlphaSearchHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError
```

- 纯工具计费快照：

```go
func ComputeToolCallQuota(usage ToolCallUsage, groupRatio float64) ToolCallResult
func PostToolCallConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo)
```

- Compact 请求转换：

```go
POST /v1/responses/compact

func responsesRequestFromCompaction(req *dto.OpenAIResponsesCompactionRequest) *dto.OpenAIResponsesRequest
```

### 3. Contracts

#### Alpha Search 请求与渠道

- 路由必须经过现有 `SystemPerformanceCheck`、`TokenAuth`、`ModelRequestRateLimit` 和 `Distribute`；不得增加渠道类型白名单。
- 入口只解析调度和计费边界字段。上游 body 必须来自原始 `BodyStorage`，只写入最终模型映射并应用 Param Override；未知字段、嵌套结构和显式 `0` / `false` 必须保留。
- 顶层 `model` 必须是唯一的非空字符串；重复 `model` 必须拒绝，避免 `Distribute` 与 Relay 解析不同值。
- `max_output_tokens` 必须使用指针类型，并复用统一的 `maxTokensLimit` 上限。
- 上游路径：
  - Codex：`<base>/backend-api/codex/alpha/search`。
  - Advanced Custom：使用匹配 route 的 `upstream_path`。
  - 其它渠道：`<base>/v1/alpha/search`。
- 入站 query 中不存在冲突的键必须保留；渠道 URL 已有的固定 query 优先，客户端同名值不得追加，防止污染上游查询鉴权。
- 上游请求头由所选 adaptor 和 Header Override 构造；不得复制客户端 `Authorization`。

#### Alpha Search 响应、错误与日志

- 任意 `2xx` 都是成功：复制原始状态码、响应 body 和允许透传的响应头；过滤 hop-by-hop、`Content-Length` 和本实例 Request ID。
- 非 `2xx` 必须在响应提交前返回 `NewAPIError`，进入现有状态码映射、渠道处理和重试。
- Alpha Search 的内部错误和客户端错误消息统一为 `bad response status code <status>`；不得记录或回传上游响应 body、完整 URL 或 query。上游错误类型和错误码可以保留。
- 网络错误必须返回统一安全消息，传输层不得记录可能带 query 的完整错误 URL。
- Alpha Search 是单个非流式 JSON 值，只有显式加入非流式保活允许列表后才能写空白心跳；心跳不得阻止渠道重试。

#### Alpha Search 计费

- 每次最终成功请求固定计为一次 `web_search` 工具调用；不猜测或伪造 token usage。
- 价格通过 `operation_setting.GetToolPriceForModel("web_search", billingModelName)` 解析，并应用当前分组倍率。
- 请求上游前必须计算并保存 `ToolCallBilling` 快照，再执行预扣；结算必须使用该快照，禁止成功后重新读取可变价格配置。
- quota 换算必须使用 Checked helper。出现 `QuotaClamp` 时必须在调用现有 BillingSession `Reserve` 前返回 `400 model_price_error`。
- 跨渠道重试复用同一个 BillingSession；最终 `2xx` 才调用 `SettleBilling`，最终失败调用 `Refund`，重试后成功不得退款。
- 成功消费日志记录零 token、工具调用次数、单价、冻结额度和分组倍率；不得包含搜索请求、响应 body 或凭证。

#### Responses Compact

- `responsesRequestFromCompaction` 必须复制 `tools`、`parallel_tool_calls`、`reasoning`、`text`，以及原有 model/input/instructions/cache/service-tier 字段。
- Compact 只允许能解析为 `APITypeOpenAI` 或 `APITypeCodex` 的渠道；不得借此扩大 API 类型范围。
- 保持既有 URL：OpenAI-compatible `/v1/responses/compact`、Codex `/backend-api/codex/responses/compact`、Azure Responses Compact 规则。
- 保持 `OaiResponsesCompactionHandler` 的原始 body 转发和 usage 映射：`input_tokens -> prompt_tokens`、`output_tokens -> completion_tokens`，并保留 cache token 细节。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| Alpha Search `model` 缺失、空白或重复 | `400 invalid_request`，禁止发起上游请求 |
| `max_output_tokens` 超过统一上限 | `400 invalid_request` |
| 模型映射或 Param Override 非法 | 返回不可重试 Relay 错误 |
| 渠道路由固定 query 与客户端 query 同名 | 只发送渠道固定值 |
| quota 计算发生 NaN/溢出/饱和 | `400 model_price_error`，不得调用 `Reserve` |
| 上游网络错误 | 安全的 `do request failed` 错误；最终失败退款 |
| 上游非 `2xx` | 不提交原始响应；按状态码决定重试，错误体不进入日志 |
| 上游任意 `2xx` | 透传成功响应并按冻结的一次 WebSearch 费用结算 |
| Compact 使用不支持的 API type | `400 invalid_request`，跳过重试 |
| Compact response 含 usage | 映射 input/output/total/cache token 后进入既有文本结算 |

### 5. Good / Base / Bad Cases

- Good：Codex 请求带未来新增字段和显式零值；Relay 只改写映射后的模型，其余原始 JSON 不变，并转发到 `/backend-api/codex/alpha/search`。
- Good：第一次渠道返回 `503`，错误未提交且不泄露 body；第二次成功，最终只按冻结的一次 WebSearch 费用结算。
- Good：Advanced Custom route 用固定 `api_key` query；客户端同名 query 被忽略，其他 cursor query 保留。
- Base：分组倍率或工具价格为零；仍透传成功请求并记录请求次数，但实际 quota 为零。
- Base：Compact 继续使用现有 URL、响应 usage 和计费，仅新增官方字段映射。
- Bad：用封闭 DTO 重新 marshal Alpha Search body，导致未来字段或显式 `false` 丢失。
- Bad：每次重试重新预扣独立 BillingSession，或成功后重新读取工具价格计算结算额度。
- Bad：把 upstream URL、query、搜索请求或错误响应 body 写入日志。
- Bad：把 standalone Alpha Search 接入 Claude WebSearch 本地 provider 或 Responses hosted tool 统计。

### 6. Tests Required

- 路由与校验：覆盖路由注册、RelayMode、必填/重复 `model`、显式零值和 `max_output_tokens` 上限。
- 请求透传：覆盖未知字段、模型映射、Param Override、普通/Codex/Advanced Custom URL、query 冲突优先级和客户端 Authorization 替换。
- 响应与安全：覆盖成功状态/body/header、非 `2xx` 未提交、网络错误不记录 query、错误 body 不进入日志。
- 重试与计费：覆盖实际 Alpha Search `503` 可重试、最终失败退款、重试后成功不退款、饱和发生在 `Reserve` 前、成功使用冻结费用结算。
- Compact：覆盖全部官方映射字段、OpenAI/Codex/Azure URL，以及 compaction usage/cache token 映射。
- 回归命令：
  - `go test ./controller ./dto ./relay ./relay/channel/openai ./relay/helper ./router ./service`
  - `go test ./...`
  - 对任务相关测试运行定向 `-race`。

### 7. Wrong vs Correct

#### Wrong

```go
// 错误：成功后重新读取价格，可能与预扣阶段配置不同。
result := ComputeToolCallQuota(usage, groupRatio)
SettleBilling(ctx, relayInfo, result.TotalQuota)
```

```go
// 错误：把客户端值追加到渠道固定 query，可能泄露凭证并使上游鉴权失败。
for key, values := range c.Request.URL.Query() {
	for _, value := range values {
		query.Add(key, value)
	}
}
```

#### Correct

```go
// 预扣阶段冻结结果，成功结算只读取该快照。
relayInfo.ToolCallBilling = &result
if result.QuotaClamp != nil {
	return types.NewErrorWithStatusCode(
		result.QuotaClamp,
		types.ErrorCodeModelPriceError,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}
```

```go
// 渠道固定 query 优先，仅合并不存在冲突的客户端键。
for key, values := range c.Request.URL.Query() {
	if _, exists := query[key]; exists {
		continue
	}
	for _, value := range values {
		query.Add(key, value)
	}
}
```

## 场景：OpenAI Compact V1/V2 与 Responses WebSocket

### 1. Scope / Trigger

- Trigger：修改 `POST /v1/responses/compact`、`POST /v1/responses`、`GET /v1/responses`，或修改 Compact 协议检测、模型后缀、上游路径、Codex 元数据请求头、历史 SSE bridge、Responses WebSocket、多轮计费与审计日志。
- 协议事实以锁定并核实的 OpenAI Codex 源码为优先依据；sub2api 只作为网关 wire 兼容参考，不复制其账号调度或跨客户端连接池。
- Compact 模式必须贯穿分发、`RelayInfo`、模型映射、上游请求、响应处理、计费和日志，不能只根据 URL 或 relay mode 临时推断。

### 2. Signatures

- 入口与协议检测：

```go
POST /v1/responses/compact
POST /v1/responses
GET  /v1/responses // WebSocket Upgrade

type ResponsesCompactMode string

const (
	ResponsesCompactModeNone         ResponsesCompactMode = ""
	ResponsesCompactModeV1Path       ResponsesCompactMode = "v1_path"
	ResponsesCompactModeV1BodyBridge ResponsesCompactMode = "v1_body_bridge"
	ResponsesCompactModeV2HTTP       ResponsesCompactMode = "v2_http"
	ResponsesCompactModeV2WebSocket  ResponsesCompactMode = "v2_websocket"
)

func DetectResponsesCompactMode(method string, requestPath string, headers http.Header, body []byte, transport ResponsesTransport) relayconstant.ResponsesCompactMode
func (info *RelayInfo) IsResponsesCompact() bool
func (info *RelayInfo) UsesResponsesCompactEndpoint() bool
func (info *RelayInfo) UsesUpstreamStream() bool
```

- 历史 body-signal SSE bridge：

```go
func StartResponsesCompactSSEBridge(c *gin.Context, info *relaycommon.RelayInfo) func()
func PrepareResponsesCompactSSEFinal(c *gin.Context) (active bool, committed bool)
func WriteResponsesCompactSSECompleted(c *gin.Context, responseBody []byte, output json.RawMessage) error
func WriteResponsesCompactSSEFailed(c *gin.Context, openAIError types.OpenAIError) error
```

- Responses WebSocket 与元数据：

```go
func RelayResponsesWebSocket(c *gin.Context)
func DialResponsesWebSocket(c *gin.Context, info *relaycommon.RelayInfo) (*websocket.Conn, *http.Response, *types.NewAPIError)
func CopyResponsesMetadataHeaders(c *gin.Context, target *http.Header)
func CaptureResponsesMetadataHeaders(c *gin.Context, source http.Header)
func ClearResponsesCompactAudit(ctx *gin.Context)
```

### 3. Contracts

#### Compact 检测与路径

- `POST /v1/responses/compact` 是 `v1_path`。
- 裸 `POST /v1/responses` 只有同时满足 `stream:true`、`input[]` 含 `type=compaction_trigger`、`x-codex-beta-features` 的逗号分隔 token 精确包含 `remote_compaction_v2` 时才是 `v2_http`。
- 裸 `POST /v1/responses` 含 `compaction_trigger` 但不满足 V2 三项信号时是 `v1_body_bridge`；其上游使用 unary `/responses/compact`，下游流式意图单独保存在 `ResponsesClientStream`。
- `GET /v1/responses` 的 `response.create` 首帧满足相同三项信号时是 `v2_websocket`；普通 Responses WebSocket turn 保持 `none`。
- beta feature 必须对所有 header value 按逗号拆分、trim 后精确匹配，禁止 substring 判断。

#### 模型、请求体与计费标记

- 所有 Compact 模式的渠道选择与本地计费模型使用 `-openai-compact` 后缀；模型映射前必须移除后缀，上游 body 中只能出现映射后的真实模型名。
- V1 上游只发送当前 Codex canonical 字段：`model`、`input`、`instructions`、`tools`、`parallel_tool_calls`、`reasoning`、`service_tier`、`prompt_cache_key`、`text`。
- `v1_path` 和 `v1_body_bridge` 使用 `/responses/compact`；`v2_http` 和 `v2_websocket` 必须保持普通 `/responses`，不得因为本地计费后缀改写路径。
- V2 HTTP 的 Responses SSE data 和 WebSocket payload 原样写回；本地 DTO 解析只用于 usage、终态和 compaction item 观测，不能重组或丢弃 `encrypted_content` 等未知字段。

#### Codex 与 sub2api 元数据

- 安全 allowlist 包含 beta feature、turn state/metadata、installation/window/parent-thread、client request ID、originator、user-agent，以及 session/thread 标识。
- Codex 官方使用 `session-id` / `thread-id`，sub2api 历史入口使用 `session_id` / `thread_id`。连字符值优先，网关必须向上游同时发送两组别名。
- 客户端 `Authorization`、Cookie、Host、`Content-Length`、WebSocket 生成头和 query credential 不得进入上游；认证必须替换为 Channel API Key。
- Responses WebSocket 必须设置 `OpenAI-Beta: responses_websockets=2026-02-06`。Header Override 在安全默认头之后应用，但不得覆盖由 WebSocket dialer 生成的握手头。
- 上游握手返回的 `x-codex-turn-state`、`x-codex-turn-metadata` 可保存到当前请求上下文供重连使用；不得跨客户端或跨请求复用。

#### 历史 SSE bridge

- bridge 使用 `text/event-stream` 和 SSE 注释心跳；心跳、成功终态、失败终态共享同一 writer mutex，停止函数必须等待心跳协程退出。
- unary `output[]` 只有 JSON object 项可以生成 `response.output_item.done`；标量项跳过，`output_index` 按有效 object 连续编号。
- 成功终态最后写 `response.completed`，其中 response 保留上游字段；缺失/null `usage` 时补齐三个零值 token 字段。
- 已存在但缺少数字类型 `input_tokens`、`output_tokens` 或 `total_tokens` 的 usage 必须整体删除，避免 Codex 把 completed 事件判为不可解析。
- 心跳提交前的错误保留真实 HTTP/OpenAI JSON 错误；提交后只能写 `response.failed` SSE 终态。心跳不能阻止渠道重试。

#### Responses WebSocket 生命周期

- 路由复用性能检查、Token 鉴权和请求限流，但不挂载 Upgrade 前的 `Distribute`；控制器必须先读取首个 `response.create`，再校验 model 权限和选择渠道。
- 首帧及后续 `response.create` 只接受 text/binary JSON，必须包含 `type=response.create` 和非空 model；请求校验、模型映射、disabled fields、Param Override 与普通 Responses 使用同一套实现。
- 每个客户端连接只允许一个 active turn；前一轮收到成功或失败终态后才能发送下一轮。连接内保持所选渠道，后续 model 必须仍被该渠道和当前分组支持。
- 上游 URL 由 adaptor 的 Responses 路径生成，再执行 `https -> wss`、`http -> ws` 转换；OpenAI、Codex 和显式原生 Responses 的 Advanced Custom route 可用。
- 握手、首帧写入、上游关闭或业务错误只在尚未向客户端写出业务帧时允许 failover；写出任意上游业务帧后禁止切换渠道。
- text/binary payload、取消帧和关闭码必须按 WebSocket 语义转发；普通业务错误不能包装成成功 compaction。

#### 多轮计费与日志

- 每个 `response.create` turn 独立执行预扣、结算或退款；成功终态以 `response.completed.response.usage` 结算，缺失 usage、失败、取消或断连必须退款，BillingSession 保证幂等。
- 普通 turn 使用基础模型价格，V2 Compact turn 使用 Compact 后缀价格；每轮重新初始化 `RelayInfo` 的请求、Compact 模式、模型映射和计费字段。
- 新 turn 准备阶段必须调用 `ClearResponsesCompactAudit`，只删除上一轮 `admin_info.responses_compact`，保留视觉辅助、quota saturation 等其他日志字段，防止 `普通 -> Compact -> 普通` 串轮。
- Compact 审计只记录 mode、入站/上游路径、渠道、结局和上游 request ID；不得记录请求帧、对话、压缩密文、完整 URL/query 或凭证。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 普通 HTTP/WS Responses 不满足 Compact 信号 | mode=`none`，路径、模型价格和既有行为不变 |
| 非 Upgrade `GET /v1/responses` | `400 invalid_request`，提示需要 WebSocket Upgrade |
| 首帧不是 JSON `response.create` 或 model 为空 | WebSocket error event + policy close，不选择渠道 |
| Responses max-tokens 字段超过统一上限 | `400 invalid_request`，不得预扣或发送上游 |
| 渠道 API type 不支持 Responses WS | 渠道错误；可在首个下游业务帧前重试 |
| 同一连接存在 active turn 时再次 `response.create` | `409 invalid_request`，退款当前 turn 并关闭连接 |
| 上游握手/首帧/首个业务错误失败且下游未写业务帧 | 按现有 retry 策略切换支持 WS 的渠道，复用当前 turn 的计费生命周期 |
| 已向客户端写出业务帧后上游失败 | 禁止 failover；转发错误或关闭语义并退款未结算 turn |
| bridge output 含标量项 | 跳过标量，仅为 object item 生成连续编号事件 |
| bridge usage 缺失或 null | 补 `{input_tokens:0,output_tokens:0,total_tokens:0}` |
| bridge usage 存在但三个必需数字字段不完整 | 从 `response.completed.response` 删除 usage |
| WebSocket completed 缺失 usage | 记录异常、退款该 turn，不猜测 token 费用 |

### 5. Good / Base / Bad Cases

- Good：Codex V2 HTTP 请求保持 `/v1/responses` 和原始 SSE，模型后缀只用于本地选择/计费，`compaction` item 与 `response.completed` 原样到达客户端。
- Good：Codex 客户端只发送 `session-id`，new-api 到 sub2api 的握手同时带 `session-id` 和 `session_id`，且客户端 Authorization 被 Channel API Key 替换。
- Good：同一 WS 连接依次执行普通、Compact、普通 turn；三轮独立计费，第三轮消费日志不继承第二轮 `responses_compact` 审计。
- Base：普通 `/v1/responses` HTTP/SSE 或 WS 请求没有 `compaction_trigger`；detector 返回 `none`，继续原有 Responses 行为。
- Base：历史 body-signal 客户端请求 `stream:true` 但不声明 V2 beta；上游 unary Compact，客户端收到合法 SSE 终态。
- Bad：看到 `compaction_trigger` 就把原生 V2 改写到 `/responses/compact`。
- Bad：把 `-openai-compact` 后缀写入上游 model，或让 Compact 标记跨 WS turn 复用。
- Bad：通配复制客户端 header/query，导致 Authorization、Cookie 或 credential 泄露到上游。

### 6. Tests Required

- detector：覆盖 V1 path、V1 body bridge、V2 HTTP、V2 WS、普通 Responses、多 header value、逗号 token 和 substring 误匹配。
- 模型与 HTTP：覆盖 Compact 后缀不泄漏、V2 保持 `/responses`、V1 canonical allowlist、OpenAI/Codex/Azure URL、usage/cache token 和普通 Responses 隔离。
- header 与握手：使用真实 `httptest` WebSocket server 断言 path、query 合并、Channel Authorization、OpenAI-Beta、两组 session/thread 别名、turn state 回收和客户端凭证过滤。
- bridge：覆盖 object/scalar output、连续 `output_index`、缺失/null/不完整 usage、心跳前后错误、取消、并发 writer 和停止幂等。
- WebSocket：覆盖首帧鉴权、单 active turn、首个业务错误前 failover、输出后不重试、cancel/pong/close、多轮普通/Compact 交替、缺失 usage 退款和计费只结算一次。
- 日志：覆盖 Compact 审计持久化、敏感信息过滤，以及 `ClearResponsesCompactAudit` 保留其他 `admin_info` 字段。
- 回归命令：
  - `go test ./controller ./middleware ./relay ./relay/channel ./relay/channel/openai ./relay/helper ./router ./service`
  - `go test -race ./relay/helper -run '^TestResponsesCompactSSEBridge'`
  - `go test -race ./controller -run 'ResponsesWebSocket'`
  - `go test -race ./relay ./service -run '(DialResponsesWebSocket|BillingSession|ResponsesCompactAudit|ClearResponsesCompactAudit)'`
  - `go test ./...`
  - `go vet` 至少覆盖本任务修改的包；全仓既有告警必须证明与本任务无关。
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong

```go
// 错误：仅凭 body 字符串判断，会混淆 V1 bridge、V2 和普通请求。
if strings.Contains(string(body), "compaction_trigger") {
	requestPath = "/v1/responses/compact"
}
```

```go
// 错误：通配复制会泄露客户端认证和握手凭证。
for key, values := range c.Request.Header {
	targetHeader[key] = values
}
```

#### Correct

```go
mode := helper.DetectResponsesCompactMode(
	c.Request.Method,
	c.Request.URL.Path,
	c.Request.Header,
	body,
	helper.ResponsesTransportHTTP,
)
if mode.IsCompact() {
	selectionModel = ratio_setting.WithCompactModelSuffix(modelName)
}
```

```go
// 明确 allowlist，并由渠道 adaptor/Channel Key 负责上游认证。
channel.CopyResponsesMetadataHeaders(c, &targetHeader)
```
