# Relay WebSearch 模拟契约

> 记录 Claude Code 纯 WebSearch 请求在渠道级配置、管理 API、provider 调用、relay 短路、响应构造和计费之间的可执行契约。

## 场景：Claude Code 纯 WebSearch 渠道级模拟

### 1. Scope / Trigger

- Trigger: 修改 Claude Messages `/v1/messages` 请求解析、渠道 `setting.web_search` 配置、WebSearch provider、管理 API 渠道脱敏/复制、Claude 本地响应构造或 Claude WebSearch 计费逻辑。
- 适用范围: 渠道本身不支持 Claude `web_search` 工具，但本系统需要按渠道调用 Tavily / AnySearch 并返回 Claude Messages 兼容响应。
- 透传规则: 本地 WebSearch 模拟只由渠道 `web_search.enabled` 开关控制；未启用时纯 WebSearch 请求必须走原有上游转发路径，不能被本地配置拦截为 400。
- 风险背景: Claude prompt caching 对请求前缀敏感。WebSearch 模拟只能在本地短路并构造响应，不能把搜索结果、时间戳、随机 ID 或 provider 返回内容写回待转发的上游请求体。

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

- Relay 插入点：

```go
func shouldHandleClaudeWebSearchEmulation(info *relaycommon.RelayInfo) bool
func handleClaudeWebSearchEmulation(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) *types.NewAPIError
```

### 3. Contracts

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
  - `web_search.enabled=true` 时，纯 WebSearch 请求必须进入本地模拟短路。
  - `web_search.enabled=false` 时，纯 WebSearch 请求必须跳过本地模拟并进入原有 `adaptor.ConvertClaudeRequest` / `adaptor.DoRequest` 转发链路，由上游决定是否支持。
  - 混合普通工具、多个工具、无工具或非搜索工具时必须保持现有转发路径。
  - 本地模拟短路必须发生在 `adaptor.ConvertClaudeRequest`、`RemoveDisabledFields`、`ApplyParamOverride`、`NewOutboundJSONBody` 和 `adaptor.DoRequest` 之前；未启用本地模拟时必须完全跳过本地模拟 provider。
  - 搜索结果、响应 ID、时间戳和 provider 返回内容只能出现在响应侧，不能写回 `request`、`RelayInfo.RequestBody`、`ParamOverride` 或待转发上游 body。
- 响应与计费：
  - 非流式响应必须包含 `server_tool_use`、`web_search_tool_result` 和文本摘要。
  - 流式响应按 Claude SSE 事件序列发送 `message_start`、`content_block_start/stop`、`message_delta`、`message_stop`。
  - 成功短路时必须设置 `c.Set("claude_web_search_requests", 1)`，并调用 `service.PostTextConsumeQuota`，让现有 Claude WebSearch 工具费进入消费日志。

### 4. Validation & Error Matrix

| 条件 | 行为 |
|------|------|
| `web_search.enabled=false` 且收到纯 WebSearch 请求 | 跳过本地模拟，继续原有上游转发链路 |
| 启用但 provider 非法 | 返回 relay 错误，`400`，带 `ErrOptionWithSkipRetry()` |
| 启用 Tavily 但没有真实 API Key | 返回 relay 错误，`400`，带 `ErrOptionWithSkipRetry()` |
| 最后一条消息不是 `role=user` 或无法提取文本查询 | 返回 relay 错误，`400`，带 `ErrOptionWithSkipRetry()` |
| 代理配置非法 | 返回 relay 错误，`400`，带 `ErrOptionWithSkipRetry()` |
| provider HTTP 非 2xx / JSON 解析失败 / 返回空响应 | 返回 relay 错误，`502`，带 `ErrOptionWithSkipRetry()` |
| 管理 API 创建/更新启用 Tavily WebSearch 但缺 key | HTTP 200 + `{success:false,message:"..."}` |
| 管理 API 列表/详情/更新响应包含 WebSearch key | 违反契约，必须修复为响应副本脱敏 |

### 5. Good/Base/Bad Cases

- Good: DeepSeek 渠道启用 WebSearch + Tavily key；Claude Code 发来 `tools=[{"type":"web_search_20250305","name":"web_search"}]`；relay 在上游 body 构造前调用 Tavily，返回 Claude `server_tool_use` + `web_search_tool_result`，并记录 Claude WebSearch 工具费。
- Good: DeepSeek 渠道启用 WebSearch + AnySearch 且未配置 key；relay 调用 AnySearch 时不发送 `Authorization`，仍返回 Claude WebSearch 模拟响应。
- Good: 渠道未启用本地 WebSearch 模拟；Claude Code 发来纯 WebSearch 请求；relay 不调用 Tavily / AnySearch，继续把原始 Claude Messages 请求转发给上游。
- Good: 编辑已配置 key 的渠道时前端不填新 key；后端保留旧 key，并在响应中只返回 `api_key_configured=true`。
- Good: 复制已配置 WebSearch 的渠道；新渠道继承 provider、参数和真实 key。
- Base: 请求包含 `web_search` 和普通函数工具；不做本地模拟，继续原转发路径。
- Base: 老渠道没有 `web_search` 字段；默认关闭，不影响普通请求。
- Bad: Tavily 请求体包含 `api_key`。
- Bad: 把搜索结果追加到 `request.Messages` 或写入 `RelayInfo` 中会参与上游请求体构造的字段。
- Bad: 在模型层或数据库层写入脱敏后的 `api_key=""`，导致复制渠道或后续编辑丢失真实 key。

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
if websearch.IsPureClaudeWebSearchRequest(request) && shouldHandleClaudeWebSearchEmulation(info) {
    return handleClaudeWebSearchEmulation(c, info, request)
}

convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, request)
```

要求：
- 本地模拟短路在 `ConvertClaudeRequest` 和 `NewOutboundJSONBody` 之前完成。
- 未启用本地模拟时必须跳过 `handleClaudeWebSearchEmulation`，进入原有转发链路。
- `handleClaudeWebSearchEmulation` 只读取查询并构造响应，不修改原始请求对象。
- 成功短路后设置 `claude_web_search_requests=1` 并进入现有文本计费结算。
