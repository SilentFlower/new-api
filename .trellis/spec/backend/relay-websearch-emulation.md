# Relay WebSearch 模拟契约

> 记录 Claude Messages 与 OpenAI Chat Completions 纯 WebSearch 请求在渠道级配置、管理 API、provider 调用、relay 短路、响应构造和计费之间的可执行契约，以及两个协议主链路的薄层接入边界。

## 场景：Claude Messages 与 Chat Completions 纯 WebSearch 渠道级模拟

### 1. Scope / Trigger

- Trigger: 修改 Claude Messages `/v1/messages` 或 OpenAI `/v1/chat/completions` 请求解析、渠道 `setting.web_search` 配置、WebSearch provider、管理 API 渠道脱敏/复制、本地响应构造、WebSearch 计费逻辑，或 Anthropic `output_config.effort` 日志同步逻辑。
- 适用范围: 渠道本身不支持原生联网搜索，但本系统需要按渠道调用 Tavily / AnySearch，并返回 Claude Messages 或 Chat Completions 兼容响应。
- 透传规则: 本地 WebSearch 模拟只由渠道 `web_search.enabled` 开关控制；未启用时纯 WebSearch 请求必须走原有上游转发路径，不能被本地配置拦截为 400。
- 风险背景: Claude prompt caching 对请求前缀敏感，Chat 请求还可能进入 Chat-to-Responses、透传或 adaptor 转换。WebSearch 模拟只能在这些转换前本地短路并构造响应，不能把搜索结果、时间戳、随机 ID 或 provider 返回内容写回待转发的上游请求体。

### 2. Signatures

- 渠道配置 DTO：

```go
type ChannelSettings struct {
    WebSearch ChannelWebSearchSettings `json:"web_search,omitempty"`
}

type ChannelWebSearchSettings struct {
    Enabled          bool     `json:"enabled,omitempty"`
    Provider         string   `json:"provider,omitempty"`
    APIKey           string   `json:"api_key,omitempty"`
    APIKeyConfigured bool     `json:"api_key_configured,omitempty"`
    ClearAPIKey      bool     `json:"clear_api_key,omitempty"`
    MaxResults       int      `json:"max_results,omitempty"`
    SearchDepth      string   `json:"search_depth,omitempty"`
    Freshness        string   `json:"freshness,omitempty"`
    ContentTypes     []string `json:"content_types,omitempty"`
}

func (s *ChannelWebSearchSettings) Normalize()
func (s ChannelWebSearchSettings) HasAPIKey() bool
func (s ChannelWebSearchSettings) ValidateForRelay() error
```

- Provider 契约：

```go
type SearchRequest struct {
    Query        string
    MaxResults   int
    SearchDepth  string
    Freshness    string
    ContentTypes []string
}

type SearchResult struct {
    URL     string `json:"url,omitempty"`
    Title   string `json:"title,omitempty"`
    Snippet string `json:"snippet,omitempty"`
    PageAge string `json:"page_age,omitempty"`
}

type Provider interface {
    Name() string
    Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)
}

func NewProvider(settings dto.ChannelWebSearchSettings, httpClient *http.Client) (Provider, error)
```

- Claude 纯 WebSearch 识别与响应构造：

```go
func IsPureClaudeWebSearchRequest(request *dto.ClaudeRequest) bool
func ExtractClaudeWebSearchQuery(request *dto.ClaudeRequest) string
func BuildClaudeWebSearchResponse(messageID string, toolUseID string, modelName string, query string, results []SearchResult, inputTokens int, outputTokens int) *dto.ClaudeResponse
func BuildClaudeWebSearchStreamEvents(messageID string, toolUseID string, modelName string, query string, results []SearchResult, inputTokens int, outputTokens int) []*dto.ClaudeResponse
```

- Chat Completions 纯 WebSearch 识别与响应构造：

```go
func IsPureChatWebSearchRequest(request *dto.GeneralOpenAIRequest) bool
func ExtractChatWebSearchQuery(request *dto.GeneralOpenAIRequest) string
func BuildChatWebSearchResponse(responseID string, created int64, modelName string, query string, results []SearchResult, inputTokens int, outputTokens int) *dto.OpenAITextResponse
func BuildChatUsage(inputTokens int, outputTokens int) *dto.Usage
```

- Relay 共用执行与协议插入点：

```go
func executeChannelWebSearch(c *gin.Context, info *relaycommon.RelayInfo, query string) (*websearch.SearchResponse, *types.NewAPIError)

func shouldHandleClaudeWebSearchEmulation(info *relaycommon.RelayInfo) bool
func handleClaudeWebSearchEmulation(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) *types.NewAPIError
func writeClaudeWebSearchStream(c *gin.Context, messageID string, toolUseID string, modelName string, query string, results []websearch.SearchResult, inputTokens int, outputTokens int) error

func shouldHandleChatWebSearchEmulation(info *relaycommon.RelayInfo) bool
func handleChatWebSearchEmulation(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) *types.NewAPIError
func writeChatWebSearchStream(c *gin.Context, responseID string, created int64, modelName string, query string, results []websearch.SearchResult, usage *dto.Usage, includeUsage bool) error
```

- Anthropic Reasoning Effort 日志同步：

```go
func syncAnthropicReasoningEffort(info *relaycommon.RelayInfo, outputConfig []byte)
func syncAnthropicReasoningEffortFromRequestBody(info *relaycommon.RelayInfo, requestBody []byte)
```

### 3. Contracts

- 文件所有权与薄层接入：
  - `dto/channel_websearch_settings.go` 独占 WebSearch provider 常量、`ChannelWebSearchSettings`、归一化、relay 校验和 provider 支持判断；`dto/channel_settings.go` 只在 `ChannelSettings` 中保留 `WebSearch ChannelWebSearchSettings` 字段。
  - `controller/channel_websearch_setting.go` 独占 setting JSON record 读写、WebSearch key 继承/显式清空、创建归一化和响应副本脱敏；`controller/channel.go` 只在列表/搜索/详情/更新响应、创建和更新原有位置调用 `sanitizeChannel(s)ForResponse`、`normalizeChannelWebSearchForCreate`、`mergeChannelWebSearchAPIKey`。
  - `relay/websearch_emulation.go` 独占 Claude 与 Chat 共用的配置校验、代理 Client、30 秒超时、provider 创建和错误映射；两个协议 handler 不复制 provider 调用。
  - `relay/claude_websearch_emulation.go` 独占 Claude 开关、JSON/SSE 响应和成功结算；`relay/claude_handler.go` 只在上游请求体转换前保留纯 WebSearch 条件分派。
  - `relay/chat_websearch_emulation.go` 独占 Chat 开关、JSON/SSE 响应和成功结算；`relay/compatible_handler.go` 只在 `adaptor.Init` 后、Chat-to-Responses 与上游 body 构造前保留窄条件分派。
  - `relay/claude_reasoning_effort.go` 独占 Anthropic effort 日志同步；`relay/claude_handler.go` 只在透传 body 路径从解析后的 `OutputConfig` 同步一次，并在普通路径从参数覆盖后的最终 JSON 同步一次。
  - 这些领域文件与原调用方保持同 package，避免为了结构治理新增导出 API、包装层或跨包依赖。
- 配置存储：
  - WebSearch 配置存放在 `model.Channel.Setting` 的 `web_search` 字段，不需要数据库迁移。
  - `provider` 只支持 `tavily` / `anysearch`，启用但未指定 provider 时默认 Tavily。
  - `max_results` 必须归一化到 `1..20`，默认 `5`。
  - `search_depth` 只允许 `basic` / `advanced`，非法值归一化为 `basic`。
  - AnySearch `freshness` 只允许空值、`day`、`week`、`month`、`year`。
  - AnySearch `content_types` 必须 trim、lowercase、去空、去重并按白名单过滤。
- 密钥处理：
  - 数据库存储真实 `api_key`；管理 API 响应必须移除 `api_key`，只返回 `api_key_configured`。
  - Tavily 启用时必须配置真实 `api_key`；AnySearch 的 `api_key` 可选，没有 key 时仍允许调用。
  - 更新渠道时，提交空 `web_search.api_key` 且 `clear_api_key != true` 必须沿用原渠道真实 key。
  - 显式清空 key 必须通过 `clear_api_key=true`，不能把前端空输入误认为清空。
  - 复制渠道必须使用原始渠道配置和已配置的真实 key；脱敏只能作用在响应副本上，不能污染数据库模型。
- Provider 调用：
  - Tavily POST `https://api.tavily.com/search`，认证只能使用 `Authorization: Bearer <api_key>`，请求体不能包含 key。
  - AnySearch POST `https://api.anysearch.com/mcp`，JSON-RPC `method=tools/call`，工具名 `search`，有 key 时使用 Bearer header。
  - provider JSON 编解码必须使用 `common.Marshal` / `common.Unmarshal` / `common.DecodeJson`。
  - provider 响应体读取必须设置大小上限，错误消息不得包含完整 key、请求体、响应体或用户对话内容。
- Relay 短路：
  - 仅当 Claude 请求 `tools` 恰好包含一个搜索工具时短路；工具 `type` 或 `name` 可为 `web_search`、`web_search_` 前缀或 `google_search`。
  - Chat 请求仅在 `RelayModeChatCompletions`、`web_search_options != nil`、`tools` 为空且旧式 `functions` 缺失、为 `null` 或空数组时短路；存在实际工具定义、非 Chat 模式或畸形 `functions` 时保持原转发路径。
  - `web_search.enabled=true` 时，纯 WebSearch 请求必须进入本地模拟短路。
  - `web_search.enabled=false` 时，Claude 和 Chat 纯 WebSearch 请求都必须跳过本地模拟并进入各自原有转发链路，由上游决定是否支持；不得因本地 provider 未配置而返回 400。
  - 混合普通工具、多个工具、无工具或非搜索工具时必须保持现有转发路径。
  - Claude 短路必须发生在 `adaptor.ConvertClaudeRequest`、`RemoveDisabledFields`、`ApplyParamOverride`、`NewOutboundJSONBody` 和 `adaptor.DoRequest` 之前；Chat 短路必须发生在 Chat-to-Responses、请求透传、`adaptor.ConvertOpenAIRequest`、参数覆盖和上游 body 构造之前。
  - Chat 渠道显式启用本地模拟后，即使全局或渠道开启请求透传，本地模拟仍优先；未启用时必须完全跳过本地 provider。
  - 搜索结果、响应 ID、时间戳和 provider 返回内容只能出现在响应侧，不能写回 `request`、`RelayInfo.RequestBody`、`ParamOverride` 或待转发上游 body。
- Reasoning Effort 日志同步：
  - 只对 `ChannelTypeAnthropic` 生效；非 Anthropic 渠道、空 `RelayInfo` 或尚未初始化 `ChannelMeta` 时不得修改日志字段。
  - 透传 body 路径必须读取解析后的 `ClaudeRequest.OutputConfig`，不能重新编码或修改原始 body。
  - 普通路径必须在 `RemoveDisabledFields` 和 `ApplyParamOverrideWithRelayInfo` 之后读取最终 JSON，确保日志记录的是实际发往上游的 `output_config.effort`。
  - `effort` 不是 JSON 字符串或字段不存在时必须将 `RelayInfo.ReasoningEffort` 清空，避免渠道重试复用旧值。
- 响应与计费：
  - Claude 非流式响应必须包含 `server_tool_use`、`web_search_tool_result` 和文本摘要；流式响应按 Claude SSE 事件序列发送 `message_start`、`content_block_start/stop`、`message_delta`、`message_stop`。
  - Chat 非流式响应必须返回 `object=chat.completion`、assistant 文本摘要、`finish_reason=stop` 和标准 token usage；流式响应依次发送 assistant 起始块、完整摘要块、stop 块、可选 usage 块和 `[DONE]`。
  - Chat 模型名优先使用映射后的 `UpstreamModelName`，为空时回退请求模型；输入 Token 优先使用预估值，缺失时按查询字符数兜底，输出 Token 按稳定摘要长度估算。
  - Chat 首版不返回 citation / annotations，不暴露 Claude 的 `server_tool_use` / `web_search_tool_result`，也不调用上游模型二次综合。
  - Claude 与 Chat 成功短路都必须设置 `c.Set("claude_web_search_requests", 1)` 并调用 `service.PostTextConsumeQuota`；Chat 额外设置 `ContextKeyChatWebSearchLocalEmulation=true`，禁止 `search-preview` 模型名兜底再叠加一次 `web_search_preview` 费用。

### 4. Validation & Error Matrix

| 条件 | 行为 |
|------|------|
| `web_search.enabled=false` 且收到纯 WebSearch 请求 | 跳过本地模拟，继续原有上游转发链路 |
| Chat 请求缺少 `web_search_options`、包含 `tools`、包含实际 `functions` 或不是 Chat Completions 模式 | 不触发本地模拟，继续原有转发链路 |
| Chat `functions` 缺失、为 `null` 或空数组 | 在其它条件满足时允许本地模拟 |
| 启用但 provider 非法 | 返回 relay 错误，`400`，带 `ErrOptionWithSkipRetry()` |
| 启用 Tavily 但没有真实 API Key | 返回 relay 错误，`400`，带 `ErrOptionWithSkipRetry()` |
| 最后一条消息不是 `role=user` 或无法提取文本查询 | 返回 relay 错误，`400`，带 `ErrOptionWithSkipRetry()` |
| 代理配置非法 | 返回 relay 错误，`400`，带 `ErrOptionWithSkipRetry()` |
| provider HTTP 非 2xx / JSON 解析失败 / 返回空响应 | 返回 relay 错误，`502`，带 `ErrOptionWithSkipRetry()` |
| Chat 流式请求 `ShouldIncludeUsage=false` | 发送 start/content/stop 和 `[DONE]`，不发送 usage 块 |
| Chat 本地模拟使用 `*-search-preview` 模型 | 只记录一次 `web_search`，不再推断 `web_search_preview` |
| 管理 API 创建/更新启用 Tavily WebSearch 但缺 key | HTTP 200 + `{success:false,message:"..."}` |
| 管理 API 列表/详情/更新响应包含 WebSearch key | 违反契约，必须修复为响应副本脱敏 |
| 非 Anthropic 渠道携带 `output_config.effort` | 不更新 `RelayInfo.ReasoningEffort` |
| Anthropic 最终上游 JSON 中 `effort` 缺失或不是字符串 | 清空 `RelayInfo.ReasoningEffort` |
| 参数覆盖修改 Anthropic `output_config.effort` | 日志记录覆盖后的最终值 |

### 5. Good/Base/Bad Cases

- Good: DeepSeek 渠道启用 WebSearch + Tavily key；Claude Code 发来 `tools=[{"type":"web_search_20250305","name":"web_search"}]`；relay 在上游 body 构造前调用 Tavily，返回 Claude `server_tool_use` + `web_search_tool_result`，并记录 Claude WebSearch 工具费。
- Good: DeepSeek 渠道启用 WebSearch + AnySearch 且未配置 key；relay 调用 AnySearch 时不发送 `Authorization`，仍返回 Claude WebSearch 模拟响应。
- Good: 渠道未启用本地 WebSearch 模拟；Claude Code 发来纯 WebSearch 请求；relay 不调用 Tavily / AnySearch，继续把原始 Claude Messages 请求转发给上游。
- Good: OpenAI Chat 请求携带 `web_search_options` 且无普通工具；渠道启用 WebSearch；relay 在 Chat-to-Responses 和透传判断前调用 provider，返回 `chat.completion` 文本摘要并记录一次 `web_search`。
- Good: Chat 流式请求设置 `stream_options.include_usage=false`；relay 发送 assistant、摘要、stop 和 `[DONE]`，但不发送 usage chunk。
- Good: 编辑已配置 key 的渠道时前端不填新 key；后端保留旧 key，并在响应中只返回 `api_key_configured=true`。
- Good: 复制已配置 WebSearch 的渠道；新渠道继承 provider、参数和真实 key。
- Good: Anthropic 请求的 Param Override 把 `effort` 改为 `high`；上游 JSON 和 `RelayInfo.ReasoningEffort` 都记录 `high`。
- Base: 请求包含 `web_search` 和普通函数工具；不做本地模拟，继续原转发路径。
- Base: Chat 请求同时包含 `web_search_options` 与 `tools` 或非空 `functions`；不做本地编排，继续现有上游路径。
- Base: 老渠道没有 `web_search` 字段；默认关闭，不影响普通请求。
- Base: 非 Anthropic 渠道请求中存在 `output_config.effort`；不写入 Anthropic 专属日志字段。
- Bad: Tavily 请求体包含 `api_key`。
- Bad: 把搜索结果追加到 `request.Messages` 或写入 `RelayInfo` 中会参与上游请求体构造的字段。
- Bad: Chat 本地模拟同时写入 Responses 工具计数，或未设置本地模拟标记导致 `search-preview` 模型重复收取两种搜索工具费。
- Bad: 在模型层或数据库层写入脱敏后的 `api_key=""`，导致复制渠道或后续编辑丢失真实 key。
- Bad: 在参数覆盖前同步 effort，导致日志记录值与实际发往 Anthropic 的最终请求不一致。
- Bad: 把 WebSearch、setting 或 effort 的完整实现重新放回 `claude_handler.go`、`channel.go` 或 `channel_settings.go`，扩大 build 分支与上游热点文件的冲突面。

### 6. Tests Required

- DTO 测试：
  - `ChannelWebSearchSettings.Normalize` 覆盖 provider 归一化、API Key trim、`max_results` 裁剪、Tavily depth、AnySearch freshness/content_types 白名单。
  - `ValidateForRelay` 覆盖启用 Tavily 但无 key、启用 AnySearch 但无 key、非法 provider、关闭状态不报错。
- Controller 测试：
  - 管理 API 响应脱敏不修改原始 `channel.Setting`。
  - 更新空 `api_key` 继承原 key。
  - `clear_api_key=true` 清空 key。
  - 复制渠道路径不得调用脱敏辅助或写回脱敏配置。
- Provider 测试：
  - Tavily / AnySearch 带 key 请求 Authorization header 正确，AnySearch 无 key 请求不发送 Authorization，JSON body 不包含 key。
  - Tavily `results[].url/title/content` 规范化为 `SearchResult`。
  - AnySearch MCP `content` 文本 JSON、嵌套 `results/items/data/list` 和错误响应都能稳定处理。
  - provider 错误消息不包含 key。
- Relay 测试：
  - 纯 WebSearch 只接受单个搜索工具；混合工具不触发模拟。
  - 本地模拟入口判断覆盖未启用时透传、启用时模拟，且不依赖渠道类型或 base URL。
  - 查询提取只取最后一条 `user` 消息文本。
  - 响应包含 `server_tool_use`、`web_search_tool_result`、`usage.server_tool_use.web_search_requests=1`。
  - 纯 WebSearch helper 不修改原始 Claude 请求对象，防止污染上游请求体。
  - Chat predicate 覆盖 `web_search_options`、空/非空 `tools`、缺失/`null`/空/非空/畸形 `functions`；薄层复核确认入口同时具备 `RelayModeChatCompletions` 门禁。
  - Chat 查询提取覆盖字符串、文本块、最后一条非 user 和请求对象不变。
  - Chat 非流式响应断言 `chat.completion`、assistant、stop、usage；流式响应断言 start、摘要、stop、usage 开关和 `[DONE]`。
  - provider 经代理失败时断言 `502` 且错误不包含 API Key；查询缺失断言 `400`。
  - 计费测试必须覆盖 `*-search-preview` 模型只产生一项 `web_search`、调用次数为 1，不产生 `web_search_preview`。
  - effort 同步覆盖 Anthropic 字符串值、缺失/非字符串清空、非 Anthropic 无操作，以及从参数覆盖后的最终请求体读取。
- 薄层边界复核：
  - `dto/channel_settings.go` 只保留 `WebSearch` 字段，WebSearch DTO 测试位于 `dto/channel_websearch_settings_test.go`。
  - `controller/channel.go`、`relay/claude_handler.go` 和 `relay/compatible_handler.go` 只保留本节约定的窄调用；迁移不得改变调用顺序、错误构造、状态码或计费入口。
- 回归命令：
  - `go test ./dto ./controller ./relay/websearch ./relay ./service`
  - 跨层改动完成前运行 `go test ./...`。
  - 前端表单改动运行 i18n sync、typecheck 和 build。

### 7. Wrong vs Correct

#### Wrong

```go
if websearch.IsPureClaudeWebSearchRequest(request) {
    request.Messages = append(request.Messages, dto.ClaudeMessage{
        Role:    "user",
        Content: providerResultText,
    })
}

convertedRequest, _ := adaptor.ConvertClaudeRequest(c, info, request)
jsonData, _ := common.Marshal(convertedRequest)
body, _, _, _ := relaycommon.NewOutboundJSONBody(jsonData)
```

问题：
- provider 结果进入了待转发上游请求体，破坏缓存键稳定性。
- 响应 ID、结果顺序和搜索内容会让同一语义请求生成不同 body。
- 混合工具请求可能被错误本地编排。

#### Correct

```go
// relay/claude_handler.go
if websearch.IsPureClaudeWebSearchRequest(request) && shouldHandleClaudeWebSearchEmulation(info) {
    return handleClaudeWebSearchEmulation(c, info, request)
}

convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, request)
```

```go
// relay/compatible_handler.go
if info.RelayMode == relayconstant.RelayModeChatCompletions &&
    websearch.IsPureChatWebSearchRequest(request) &&
    shouldHandleChatWebSearchEmulation(info) {
    return handleChatWebSearchEmulation(c, info, request)
}

passThroughGlobal := model_setting.GetGlobalSettings().PassThroughRequestEnabled
```

要求：
- 本地模拟短路在 `ConvertClaudeRequest` 和 `NewOutboundJSONBody` 之前完成。
- Chat 本地模拟短路在 Chat-to-Responses、透传和 `ConvertOpenAIRequest` 之前完成。
- 未启用本地模拟时必须跳过 `handleClaudeWebSearchEmulation`，进入原有转发链路。
- 两个协议 handler 都只读取查询并构造响应，不修改原始请求对象。
- 成功短路后设置 `claude_web_search_requests=1` 并进入现有文本计费结算；Chat 还必须抑制 `search-preview` 的模型名推断附加费。

#### Wrong

```go
// 错误：在热点主链路中重新实现参数解析，而且读取的是参数覆盖前的值。
effort := gjson.GetBytes(request.OutputConfig, "effort")
info.ReasoningEffort = effort.String()
jsonData, _ = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
```

#### Correct

```go
// 正确：主链路只保留窄调用，普通路径从最终上游 JSON 同步。
jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
if err != nil {
	return newAPIErrorFromParamOverride(err)
}
syncAnthropicReasoningEffortFromRequestBody(info, jsonData)
```

要求：
- effort 的解析和渠道门禁归属 `relay/claude_reasoning_effort.go`。
- 透传路径调用 `syncAnthropicReasoningEffort(info, request.OutputConfig)`；普通路径调用 `syncAnthropicReasoningEffortFromRequestBody(info, jsonData)`。
- 主链路不得复制 `gjson` 解析逻辑或新增另一套 effort 状态。
