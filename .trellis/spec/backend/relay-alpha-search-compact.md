# Relay Alpha Search 与 Responses Compact 契约

> 记录 standalone Alpha Search 的透明上游转发、重试、纯工具计费和日志安全，以及 Responses Compact 基础模型选渠、渠道能力门禁、原始透传、计费和 WebSocket 契约。

## 场景：Alpha Search 上游透传

### 1. Scope / Trigger

- Trigger：修改 `POST /v1/alpha/search` 的路由、请求校验、渠道分发、上游 URL、响应处理、重试或工具计费。
- Alpha Search 是 Codex standalone Search 上游协议；它不等于 Responses hosted `web_search` tool，也不等于 Claude WebSearch 本地模拟。

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

### 5. Good / Base / Bad Cases

- Good：Codex 请求带未来新增字段和显式零值；Relay 只改写映射后的模型，其余原始 JSON 不变，并转发到 `/backend-api/codex/alpha/search`。
- Good：第一次渠道返回 `503`，错误未提交且不泄露 body；第二次成功，最终只按冻结的一次 WebSearch 费用结算。
- Good：Advanced Custom route 用固定 `api_key` query；客户端同名 query 被忽略，其他 cursor query 保留。
- Base：分组倍率或工具价格为零；仍透传成功请求并记录请求次数，但实际 quota 为零。
- Bad：用封闭 DTO 重新 marshal Alpha Search body，导致未来字段或显式 `false` 丢失。
- Bad：每次重试重新预扣独立 BillingSession，或成功后重新读取工具价格计算结算额度。
- Bad：把 upstream URL、query、搜索请求或错误响应 body 写入日志。
- Bad：把 standalone Alpha Search 接入 Claude WebSearch 本地 provider 或 Responses hosted tool 统计。

### 6. Tests Required

- 路由与校验：覆盖路由注册、RelayMode、必填/重复 `model`、显式零值和 `max_output_tokens` 上限。
- 请求透传：覆盖未知字段、模型映射、Param Override、普通/Codex/Advanced Custom URL、query 冲突优先级和客户端 Authorization 替换。
- 响应与安全：覆盖成功状态/body/header、非 `2xx` 未提交、网络错误不记录 query、错误 body 不进入日志。
- 重试与计费：覆盖实际 Alpha Search `503` 可重试、最终失败退款、重试后成功不退款、饱和发生在 `Reserve` 前、成功使用冻结费用结算。
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

## 场景：Responses Compact 渠道能力透传与基础模型计费

### 1. Scope / Trigger

- Trigger：修改 `POST /v1/responses/compact`、`POST /v1/responses`、`GET /v1/responses`、管理端 Compact 渠道测试，或修改 Compact 检测、渠道选择、渠道 `setting`、HTTP/WS 原始透传、usage 结算、重试、亲和性和审计日志。
- new-api 只承接 Compact 协议透传、渠道能力门禁和基础模型计费；sub2api 负责历史 body bridge 的协议提升与 SSE 合成。
- build 分支实现必须遵循 `../guides/build-upstream-friendly-customization.md`：核心逻辑放在独立新文件，原有 Relay、WebSocket 和前端大表单只保留最薄分派或挂载。

### 2. Signatures

```go
POST /v1/responses/compact
POST /v1/responses
GET  /v1/responses // WebSocket Upgrade
GET  /api/channel/test/:id?model=<基础模型>&endpoint_type=openai-response-compact

type ChannelSettings struct {
	ResponsesCompactPassthroughEnabled bool `json:"responses_compact_passthrough_enabled,omitempty"`
}

func DetectResponsesCompactMode(method string, requestPath string, headers http.Header, body []byte, transport ResponsesTransport) relayconstant.ResponsesCompactMode
func ShouldHandleResponsesCompactPassthrough(info *relaycommon.RelayInfo) bool
func PrepareResponsesCompactPassthrough(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError
func ResponsesCompactPassthroughHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError
func ParseResponsesCompactPassthroughUsage(raw json.RawMessage) (*dto.Usage, bool)

func SelectResponsesWebSocketChannel(c *gin.Context, modelName string) (*model.Channel, *types.NewAPIError)
func ValidateResponsesWebSocketModelAccess(c *gin.Context, modelName string) *types.NewAPIError
func ResponsesWebSocketChannelSupportsModel(channel *model.Channel, modelName string) bool

func normalizeResponsesCompactChannelTestModel(modelName, endpointType string) string
func testResponsesCompactPassthroughChannel(c *gin.Context, channel *model.Channel, testUserID int, startedAt time.Time, info *relaycommon.RelayInfo, request dto.Request) testResult
```

前端表单字段与存储 JSON 键必须完全一致：

```text
responses_compact_passthrough_enabled: boolean
```

### 3. Contracts

#### 检测、选渠与能力门禁

- `POST /v1/responses/compact` 是 `v1_path`；裸 `POST /v1/responses` 根据 `compaction_trigger`、`stream:true` 和精确 beta token 区分 `v1_body_bridge` / `v2_http`；`GET /v1/responses` 的 `response.create` 使用同一规则检测 `v2_websocket`。
- 所有 Compact 模式的 Token 权限、分组、亲和性、首次选渠和真实上游失败后的重试都使用请求中的基础模型，不得生成、查询或要求配置 `*-openai-compact` 模型。
- 能力开关不能参与选渠筛选。必须先按普通分发和亲和性选定渠道，再读取所选渠道的 `responses_compact_passthrough_enabled`。
- 开关关闭返回 `503 responses_compact_passthrough_disabled`，同时设置 `skipRetry` 和 `noRecordErrorLog`；不得换渠、清理亲和性、auto-ban、发起上游请求或预扣。
- 只有门禁通过后的真实上游错误继续服从现有状态码映射、渠道处理和重试语义。

#### 原始请求与路径

- Compact 跳过 `ModelMappedHelper`、Param Override、disabled fields 和请求 DTO 重组；HTTP 从原始 `BodyStorage` 读取，WebSocket 返回原始 frame 副本。未知字段、显式 `0` / `false`、加密内容和字段顺序不得被本地重组丢失。
- 基础模型同时写入 `OriginModelName`、`UpstreamModelName` 和计费快照；`IsModelMapped=false`，渠道 `use_upstream_model_for_billing` 对 Compact 透传不生效。
- 路径矩阵：
  - V1 path：OpenAI-compatible `/v1/responses/compact`；Codex/sub2api `/backend-api/codex/responses/compact`。
  - 历史 body bridge：保持普通 `/responses`；Codex/sub2api `/backend-api/codex/responses`，由 sub2api 提升协议并生成 SSE。
  - V2 HTTP / WebSocket：保持普通 `/responses`；不得改写到 Compact 端点。
- Compact 请求不得启动 new-api 的旧 `StartResponsesCompactSSEBridge`；该旧实现不能补 usage、重组 output 或伪造成功终态。
- 上游认证由所选 adaptor 和 Channel Key 构造；客户端 `Authorization`、Cookie、Host、hop-by-hop 头和 query credential 不得透传。Responses metadata 只通过现有安全 allowlist 复制。

#### 响应、usage 与计费

- JSON 响应写回原始字节；SSE 按读到的原始行字节写回并及时 flush；WebSocket text/binary payload 按原始 frame 写回。旁路 observer 只能读取终态、usage 和工具计数，不能修改 payload。
- 只有成功终态和完整合法 usage 才能调用 `PostTextConsumeQuota`。合法 usage 必须同时包含数字 `input_tokens`、`output_tokens`、`total_tokens`，数值非负、不超过 `common.MaxQuota`，且 `input_tokens + output_tokens == total_tokens`；cache token 也必须非负且不超过上限。
- usage 缺失、null、字段不完整、负数、溢出、总数不一致，或请求失败、取消、断连、流不完整时，必须调用 BillingSession `Refund`；不得按输出文本估算，也不得补零后收费。
- Compact 价格、预扣、结算和消费日志主模型均使用基础模型；不新增 Compact 工具价格、固定调用费或独立计费体系。已有真实 WebSearch 等工具调用仍按现有工具规则统计。
- `ChannelSettings` 缺失新字段时默认 `false`，不需要数据库迁移。Default/Classic 保存 `setting` 时必须保留未知 JSON 字段。

#### Responses WebSocket 生命周期

- 路由先读取首个 `response.create`，再校验基础 model 权限和选择渠道；同一连接只允许一个 active turn，后续 turn 继续使用当前渠道并校验该渠道是否支持基础模型。
- build 分支的 Responses WebSocket 渠道选择必须由 `middleware/responses_websocket_channel.go` 独立承载，不得为了与 HTTP 共用而重新抽取或改写整个 `middleware.Distribute`。允许保留受测试保护的局部重复。
- WS 独立实现必须与 HTTP 分发同步复核 Token 模型权限、指定渠道状态、亲和性命中与失效清理、auto group、随机选渠、Advanced Custom `/v1/responses` path/model 约束，以及 `SetupContextForSelectedChannel` 初始化语义。
- 普通 HTTP 分发错误响应只有无可用渠道返回 `model_not_found`；无效请求、Token 权限、禁用指定渠道和渠道上下文初始化错误的 `error.code` 保持为空。WS 内部返回标准 `NewAPIError`，不得用 HTTP code 清洗逻辑替代。
- Compact turn 在 dial 和预扣前执行渠道门禁；门禁错误不是 channel error，不能进入 connector failover。
- 普通 turn 继续使用原有模型映射、转换、disabled fields 和 Param Override；只有 Compact turn 分派到独立准备和终态 observer。
- 每个 turn 独立预扣、结算或退款；`ClearResponsesCompactAudit` 只清理上一轮 `admin_info.responses_compact`，不能删除 quota saturation 等其他管理员字段。

#### 管理端渠道测试

- `endpoint_type=openai-response-compact` 的渠道测试必须使用基础模型。管理员仍传入历史 `*-openai-compact` 测试模型时，只在 Compact 测试入口移除一次旧后缀；普通端点测试不得改写模型。
- 渠道测试已经由管理员明确选择渠道，不执行第二次选渠。生成 `RelayInfo` 后必须先调用 `PrepareResponsesCompactPassthrough`，能力关闭时在查价和网络请求前返回同一个专用 `503`。
- Compact 渠道测试只将测试 DTO marshal 一次作为合成原始 body，随后直接调用 adaptor `DoRequest`；不得调用 `ModelMappedHelper`、请求 converter、Param Override 或 `DoResponse`，避免测试通过的 wire 行为与生产透传不一致。
- Compact 渠道测试的价格查询、消费日志主模型和严格 usage 解析均使用基础模型；未配置任何后缀价格时仍可完成测试。
- 原生测试只支持 OpenAI、Codex 和 Advanced Custom `converter=none` route。Codex 使用 `/backend-api/codex/responses/compact`；Advanced Custom 使用匹配 `/v1/responses/compact` 的 `upstream_path`。
- 非 `2xx` 响应使用不记录错误 body 的 Relay 错误解析，成功响应仅解析完整合法 usage；不得把 Compact 响应正文、压缩密文或凭证写入渠道测试日志。
- 普通渠道测试继续使用原有模型映射、converter、Param Override、`DoResponse` 和响应校验路径，不得被 Compact 提前分派影响。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 普通 HTTP/WS Responses 不满足 Compact 信号 | mode=`none`，普通路径、映射、计费和重试不变 |
| 未配置任何 `*-openai-compact` 模型或价格 | 使用基础模型完成权限、亲和选渠、预扣和结算 |
| 已选渠道关闭 Compact 透传 | `503 responses_compact_passthrough_disabled`；不换渠、不清亲和性、不 auto-ban、不预扣、不记上游错误日志 |
| V1 path 使用 Codex/sub2api | 上游 `/backend-api/codex/responses/compact` |
| 历史 bridge 或 V2 使用 Codex/sub2api | 上游 `/backend-api/codex/responses` |
| Compact 请求含模型映射、Param Override 或 disabled fields | 全部跳过，原始请求体/frame 保持不变 |
| 上游任意非 `2xx` 或网络错误 | 响应提交前返回标准 Relay 错误；真实上游错误可按现有策略重试 |
| 成功终态含完整合法 usage | 原始响应不变，按基础模型结算一次 |
| 成功终态 usage 缺失或非法 | 原始响应仍返回客户端，退款且不猜测 token |
| SSE/WS 失败、取消、断连或无成功终态 | 退款未结算 turn/request |
| 非 Upgrade `GET /v1/responses` | `400 invalid_request` |
| WS 亲和渠道已禁用、分组能力失效或 Advanced Custom route/model 不匹配 | 按配置清理当前亲和性，再使用基础模型和 `/v1/responses` 进行普通随机选渠 |
| WS 后续 turn 的基础模型不被当前渠道或 Advanced Custom route 支持 | 返回 `model_not_found`，不得把不兼容 turn 写入当前上游连接 |
| HTTP 分发无效请求、Token 权限拒绝、禁用指定渠道或 context 初始化失败 | 保持对应 HTTP 状态，`error.code` 为空 |
| HTTP 分发无可用渠道 | `503` 且 `error.code=model_not_found` |
| Compact 渠道测试传入基础模型 | 不追加后缀，按基础模型查价并原生发送 |
| Compact 渠道测试传入历史后缀模型 | 仅移除一次 `-openai-compact`，随后按基础模型执行 |
| Compact 渠道测试所选渠道关闭能力 | 在查价和网络调用前返回 `503 responses_compact_passthrough_disabled` |
| Compact 渠道测试配置模型映射或 Param Override | 两者均不生效，上游 body 的 `model` 保持基础模型 |
| Advanced Custom Compact 渠道测试使用非 `none` converter | 返回请求错误，不把透传 body 送入转换 route |

### 5. Good / Base / Bad Cases

- Good：亲和性命中 sub2api 渠道，渠道只配置 `gpt-5.6-sol` 且开启能力；V1 到 `/backend-api/codex/responses/compact`，V2/历史 bridge 到 `/backend-api/codex/responses`，全程按 `gpt-5.6-sol` 计费。
- Good：请求含未知字段、`false` 和 `0`；上游收到的 HTTP body/WS frame 与下游原始字节一致，响应中的 `encrypted_content` 和未来字段原样返回。
- Good：所选亲和渠道关闭能力；立即返回专用 503，未调用上游、未预扣、未重选或清理亲和性。
- Good：WS 亲和性命中一个不支持 `/v1/responses` 的 Advanced Custom 渠道；独立选渠逻辑清理失效亲和性，并回落到支持基础模型的普通渠道。
- Good：管理端用基础模型测试 OpenAI、Codex 或 Advanced Custom 原生 Compact route；即使渠道配置模型映射和 Param Override，上游仍收到基础模型合成 body。
- Base：旧渠道没有新字段；开关默认为 false，普通 Responses 不受影响。
- Base：完整 usage 的三个 token 字段显式为零；视为结构合法并按零实际额度结算，不能误判为字段缺失。
- Base：旧渠道的默认测试模型仍带 `-openai-compact`；选择 Compact 端点时归一为基础模型，选择普通端点时保持原值以避免扩大兼容变更。
- Bad：给基础模型追加 `-openai-compact` 后再做 Token 权限、选渠、模型映射或计费。
- Bad：历史 bridge 发到 `/responses/compact` 后由 new-api 把 unary JSON 重组为 SSE。
- Bad：能力开关参与候选渠道过滤，导致亲和渠道被静默替换。
- Bad：为了让 WS 复用 HTTP 选渠而把 `Distribute` 主体抽成通用 helper，导致上游核心文件被大面积重排。
- Bad：completed 缺 usage 时按本地 tokenizer 估算收费，或补零 usage 后记录为正常成功计费。
- Bad：生产 Compact 已走原始透传，但管理端渠道测试仍追加后缀并调用 converter，导致测试结果与真实请求语义相反。

### 6. Tests Required

- detector：覆盖 V1 path、历史 bridge、V2 HTTP、V2 WS、普通 Responses、多 header value、逗号 token 和 substring 误匹配。
- 分发与门禁：断言四种 Compact 模式使用基础模型；关闭能力返回专用 503、`skipRetry`、`noRecordErrorLog`，且 Billing 未创建/预扣。
- HTTP：使用真实 `httptest` 上游断言 OpenAI/Codex 路径矩阵、原始 body、Channel Authorization、客户端 Cookie/Authorization 过滤、JSON/SSE 原始响应和安全响应头。
- usage：覆盖完整非零、完整显式零、缺失/null、不完整、负数、超过 `common.MaxQuota`、总数不一致、失败终态和不完整流；断言只结算一次或退款。
- WebSocket：覆盖原始 frame、基础模型计费、开关关闭不 failover、多 turn 普通/Compact 交替、completed 非法 usage 退款、失败/取消/断连退款。
- WebSocket 选渠：覆盖 Token 权限、指定渠道启用/禁用、亲和性命中、亲和性失效清理、auto group、随机选渠的 Advanced Custom path/model 过滤、context 初始化和后续 turn 当前渠道能力。
- HTTP 分发：覆盖无效请求、`shouldSelectChannel=false` 时仍执行 Token 权限、禁用指定渠道、无可用渠道的 `model_not_found`，以及 `SetupContextForSelectedChannel` 错误不泄露内部 error code。
- 管理端渠道测试：覆盖基础模型不追加后缀、历史后缀归一、能力关闭先于查价和网络、模型映射与 Param Override 跳过、OpenAI/Codex/Advanced Custom 原生路径，以及普通端点模型不被归一化。
- 前端：Default 表单 round-trip 覆盖旧值默认 false、新值保存和未知 `setting` 字段保留；Default/Classic 构建或类型检查验证组件挂载，所有 locale 包含标签和说明。
- 回归命令：
  - `go test ./dto ./middleware ./relay ./relay/channel/openai ./relay/channel/codex ./controller ./service -count=1`
  - `go test -race ./relay -run 'ResponsesCompactPassthrough' -count=1`
  - `go test -race ./controller -run 'ResponsesCompactPassthrough|ResponsesWebSocket' -count=1`
  - `go test ./... -count=1`
  - `go vet ./...`；全仓既有告警必须与任务修改包区分记录。
  - `cd web/default && bun test src/features/channels/lib/channel-form.test.ts && bun run typecheck && bun run build`
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong

```go
// 错误：后缀模型会在 new-api 查价阶段触发 model_price_error。
if mode.IsCompact() {
	selectionModel = ratio_setting.WithCompactModelSuffix(modelName)
}
```

```go
// 错误：在亲和性选渠前按 Compact 能力过滤候选渠道。
channels = filterCompactEnabledChannels(channels)
```

```go
// 错误：历史 bridge 由 new-api 重组响应并补 usage。
StartResponsesCompactSSEBridge(c, relayInfo)
```

```go
// 错误：管理端 Compact 测试重新制造虚拟模型并落回旧转换链。
testModel = ratio_setting.WithCompactModelSuffix(testModel)
_ = helper.ModelMappedHelper(c, info, request)
```

```go
// 错误：为复用 WS 而重写 HTTP 分发主流程。
channel, apiErr := SelectAndSetupChannel(c, request, true)
```

#### Correct

```go
// 所有 Compact 模式先按基础模型完成普通分发和亲和性选择。
selectionModel = modelName
```

```go
// 渠道已选定、预扣尚未发生时执行能力门禁。
if relay.ShouldHandleResponsesCompactPassthrough(relayInfo) {
	if apiErr := relay.PrepareResponsesCompactPassthrough(c, relayInfo); apiErr != nil {
		return apiErr
	}
}
```

```go
// Compact 独立模块读取原始 BodyStorage；历史 bridge 的出站路径视图保持普通 Responses。
outboundInfo := responsesCompactPassthroughOutboundInfo(relayInfo)
response, err := adaptor.DoRequest(c, outboundInfo, common.ReaderOnly(storage))
```

```go
// Compact 渠道测试在旧主函数中只做提前分派，完整测试逻辑位于独立文件。
testModel = normalizeResponsesCompactChannelTestModel(testModel, endpointType)
if relay.ShouldHandleResponsesCompactPassthrough(info) {
	return testResponsesCompactPassthroughChannel(c, channel, testUserID, startedAt, info, request)
}
```

```go
// HTTP 保持上游顺序式 Distribute，WS 使用独立领域入口。
channel, apiErr := middleware.SelectResponsesWebSocketChannel(c, turn.selectionModel)
```
