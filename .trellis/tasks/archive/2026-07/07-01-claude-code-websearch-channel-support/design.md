# Claude Code WebSearch 渠道支持技术设计

## 目标边界

本任务为 Claude Code 的纯 WebSearch 请求增加渠道级模拟能力。目标是在目标渠道本身不支持 Claude `web_search` 时，由本系统按渠道配置调用 Tavily 或 AnySearch，并返回 Claude Messages 兼容响应。

本次只处理“纯 web_search 请求”：`tools` 恰好只有一个搜索工具，且该工具类型或名称是 `web_search`、`web_search_20250305`、`google_search` 等搜索工具。只要请求同时包含普通工具、多个工具，或需要工具循环编排，就继续走现有转发路径。

## 配置模型

WebSearch 配置放在 `model.Channel.Setting` 对应的 `dto.ChannelSettings` 中，原因是它属于渠道通用行为增强，不是某个渠道类型的版本参数。新增结构建议为：

```go
type ChannelWebSearchSettings struct {
	Enabled          bool   `json:"enabled,omitempty"`
	Provider         string `json:"provider,omitempty"`
	APIKey           string `json:"api_key,omitempty"`
	APIKeyConfigured bool   `json:"api_key_configured,omitempty"`
	ClearAPIKey      bool   `json:"clear_api_key,omitempty"`
	MaxResults       int    `json:"max_results,omitempty"`
	SearchDepth      string `json:"search_depth,omitempty"`
	Freshness        string `json:"freshness,omitempty"`
	ContentTypes     []string `json:"content_types,omitempty"`
}
```

`APIKey` 是存储字段，只能在数据库和 relay 内部使用。`APIKeyConfigured` 与 `ClearAPIKey` 是管理 API / 前端交互字段：前者用于响应展示是否已配置，后者用于显式清空密钥。管理 API 返回渠道列表和详情时必须移除 `api_key`，只保留 `api_key_configured`。

更新渠道时，如果提交的 `web_search.api_key` 为空且没有 `clear_api_key=true`，后端从原渠道 `Setting` 中继承原始 API Key。这样编辑其他字段不会误清空密钥。创建渠道时，启用 Tavily WebSearch 且没有密钥应在校验阶段报错；启用 AnySearch 时 API Key 可为空。复制渠道使用 `model.GetChannelById(id, true)` 读取原始渠道并浅拷贝，因此不能在模型层或数据库层写入脱敏值，否则复制后的渠道会失效。

## Provider 抽象

新增后端包建议放在 `relay/websearch`，避免和通用业务 service 混在一起，同时让 relay 拦截路径可直接依赖。包内定义稳定契约：

```go
type SearchRequest struct {
	Query       string
	MaxResults int
}

type SearchResult struct {
	URL     string
	Title   string
	Snippet string
	PageAge string
}

type Provider interface {
	Name() string
	Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)
}
```

Tavily provider 复用 sub2api 的简单路径，但认证按当前官方契约使用 `Authorization: Bearer <api_key>` 请求头，避免把密钥放入 JSON body。POST `https://api.tavily.com/search`，请求体包含 `query`、`max_results`、`search_depth`，响应的 `results[].url/title/content` 规范化为 `URL/Title/Snippet`。

AnySearch provider 不引入 AstrBot 插件代码，只实现其 HTTP/MCP 调用契约：POST `https://api.anysearch.com/mcp`，JSON-RPC method 为 `tools/call`，工具名为 `search`，参数包含 `query`、`max_results`、`freshness`、`content_types`，有密钥时设置 `Authorization: Bearer <api_key>`。响应结果统一解析成同一组 `SearchResult` 字段。

Provider 调用必须设置请求超时和响应体大小上限，错误消息不能包含完整 API Key。JSON 编解码在本仓库内使用 `common.Marshal` / `common.Unmarshal` / `common.DecodeJson`。

## Relay 插入点

插入点在 `relay/claude_handler.go` 中 Claude 请求完成解析、渠道配置已经写入 `RelayInfo` 后，正式构造上游请求体之前。

处理顺序：

1. 使用 `request.GetTools()` 或等价解析判断是否纯 WebSearch。
2. 如果不是纯 WebSearch，保持现有路径不变。
3. 如果是纯 WebSearch 但渠道未启用 WebSearch，返回 Claude relay 错误，标记为不可重试。
4. 如果渠道启用但 provider 或当前 provider 所需的 API Key 配置无效，返回 Claude relay 错误，标记为不可重试。
5. 从最后一条 user 消息提取文本查询。
6. 调用配置的 provider。
7. 本地构造 Claude Messages 响应并写回客户端。
8. 设置 `c.Set("claude_web_search_requests", 1)`，再调用 `service.PostTextConsumeQuota` 完成普通文本计费和 Claude WebSearch 工具费记录。

该短路不能调用 `adaptor.ConvertClaudeRequest`、`RemoveDisabledFields`、`ApplyParamOverride` 或 `NewOutboundJSONBody`，因为本次没有真实上游请求。搜索结果、响应 ID、时间戳只能出现在响应侧。

## 响应构造

非流式响应采用 Claude Messages 格式，内容块包含：

- `server_tool_use`：`name=web_search`，`input.query` 为提取出的查询。
- `web_search_tool_result`：引用上面的 tool use id，内容为规范化搜索结果列表。

响应 `usage.server_tool_use.web_search_requests` 设置为 `1`，以便复用现有 `service/text_quota.go` 中的 `claude_web_search_requests` 工具费逻辑。

流式请求本次可以按 Claude SSE 事件返回等价内容；如果实现成本过高，允许先返回非流式同构响应，但必须保证 Claude Code 可消费。实现阶段需要用单测固定事件顺序或响应结构，避免后续漂移。

## 请求体稳定性

稳定性约束如下：

- 纯 WebSearch 短路不产生上游请求体，因此不会影响上游缓存键。
- 混合工具请求不触发模拟，继续原始转发路径。
- 不把搜索结果、时间戳、随机 ID 写入 `request`、`RelayInfo.RequestBody`、`ParamOverride` 或任何待转发上游 body。
- Provider 参数由渠道配置和用户查询确定，结果排序以供应商返回顺序为准，不在本地做随机重排。
- 测试需要断言纯 WebSearch 短路不会调用 outbound body 构造路径，混合工具请求的转换 body 与现有路径保持一致。

## 错误处理

管理 API 按项目规范返回 HTTP 200 + `{success:false,message:"..."}`。Relay API 返回 Claude/OpenAI 兼容错误格式，使用 `types.NewErrorWithStatusCode` 或相邻 relay 工具构造，并设置 `ErrOptionWithSkipRetry()`，避免“渠道配置错误”触发无意义重试。

Provider 错误对客户端展示时只给出供应商、状态码和简短原因，不返回请求体中的 API Key。内部日志也不得记录明文密钥。

## 前端设计

默认前端在渠道新增/编辑抽屉中增加 WebSearch 设置区：

- 开关：启用 Claude Code WebSearch 增强。
- 供应商：Tavily / AnySearch。
- API Key 输入：Tavily 必填，AnySearch 可选；空值在编辑时表示沿用旧 key；另设清空按钮或复选项表达显式清空。
- 最大结果数：默认 5，限制合理范围。
- Tavily 参数：`search_depth`，默认 `basic`。
- AnySearch 参数：`freshness`、`content_types`。

前端类型同步扩展 `ChannelSettings`，表单组装继续走 `buildSettingJSON`，避免直接编辑原始 JSON 时丢弃未知字段。翻译补齐 `en`、`zh`、`fr`、`ja`、`ru`、`vi`。

## 兼容与迁移

不需要数据库迁移，配置复用 `channels.setting` 文本 JSON。老渠道没有 `web_search` 字段时默认关闭。老前端保存渠道时需要保留 `existingSettings.web_search` 中未知字段，避免编辑其他设置时覆盖掉密钥或供应商参数。

复制渠道不新增特殊复制逻辑，但需要增加回归测试或后端测试固定行为：脱敏响应不能污染数据库中的 `Setting`，复制后新渠道的 `web_search.api_key` 如原渠道已配置则仍是真实值。

## 测试策略

后端单元测试：

- `dto.ChannelSettings` 能解析缺省与完整 `web_search` 配置。
- 管理 API 列表/详情返回会脱敏 `web_search.api_key` 并返回 `api_key_configured`。
- 更新渠道空 `api_key` 会继承旧 key，`clear_api_key=true` 会清空。
- 复制渠道保留真实 WebSearch 配置和已配置的 key。
- 纯 WebSearch 判断只接受单个搜索工具。
- Tavily 与 AnySearch provider 响应规范化。
- Provider 错误不会泄露 key。
- 纯 WebSearch 短路响应包含 `server_tool_use`、`web_search_tool_result`、`usage.server_tool_use.web_search_requests=1`。
- 混合工具请求不触发模拟。

前端验证：

- 渠道表单能显示、编辑、保存 WebSearch 设置。
- 编辑已配置 key 的渠道时，不输入新 key 不会提交清空动作。
- i18n 同步脚本通过。

回归命令建议：

- `go test ./dto ./relay/... ./controller ./service`
- `cd web/default && bun run i18n:sync`
- `cd web/default && bun run build`
