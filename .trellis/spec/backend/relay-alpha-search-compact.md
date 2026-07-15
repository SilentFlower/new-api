# Relay Alpha Search 与 Responses Compact 契约

> 记录 standalone Alpha Search 的透明上游转发、重试、纯工具计费和日志安全，以及 Responses Compact 官方字段补全边界。

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
