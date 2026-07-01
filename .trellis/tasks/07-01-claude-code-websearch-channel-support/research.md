# WebSearch 参考实现与供应商契约研究记录

## 本仓库现状

- `dto/claude.go` 已有 `ClaudeWebSearchTool`、`ClaudeWebSearchUserLocation`、`ClaudeUsage.ServerToolUse.WebSearchRequests`。
- `relay/channel/claude/relay-claude.go` 已能把 Claude 上游响应里的 `usage.server_tool_use.web_search_requests` 写入 `claude_web_search_requests`。
- `service/text_quota.go` 已根据 `claude_web_search_requests` 统计 Claude WebSearch 工具费。
- `relay/claude_handler.go` 非透传路径会先转换并 marshal 上游请求体，再执行禁用字段移除和参数覆盖；WebSearch 模拟必须在这之前短路。
- `controller/channel.go` 的 `CopyChannel` 通过 `model.GetChannelById(id, true)` 读取含密钥的原渠道并浅拷贝，适合保留 `Setting` 里的 WebSearch 密钥。

## sub2api 参考

- 参考路径：`/root/project/my/sub2api/backend/internal/pkg/websearch/*`。
- 可复用思路：`Provider` 抽象、`SearchRequest`、`SearchResponse`、`SearchResult`、provider 结果规范化、HTTP 超时和响应大小限制。
- 本次不采用：全局 provider 池、额度权重、代理故障切换、账号级三态覆盖。原因是用户已确认本次按渠道配置，且只做纯 WebSearch 请求。
- `gateway_websearch_emulation.go` 的关键判断是 `tools` 恰好只有一个搜索工具，本任务沿用这个触发边界。

## AnySearch

- 参考仓库：`https://github.com/AgIzT/astrbot_plugin_anysearch`。
- 本地参考：`/tmp/astrbot_plugin_anysearch/client.py`。
- endpoint：`https://api.anysearch.com/mcp`。
- 请求：JSON-RPC 2.0，`method` 为 `tools/call`，`params.name` 为 `search`。
- 参数：`query`、可选 `max_results`、`freshness`、`content_types`。
- API Key 可选；配置后通过 `Authorization: Bearer <api_key>` 请求头发送。
- 响应：插件从 `result.content[]` 中读取 `type=text` 的文本，本任务需要把文本或结构化结果规范化为 `SearchResult`。

## Tavily

- 官方参考：`https://docs.tavily.com/documentation/api-reference/endpoint/search`。
- endpoint：`https://api.tavily.com/search`。
- 认证：`Authorization: Bearer <api_key>` 请求头。
- 请求体核心字段：`query`、`max_results`、`search_depth`。
- 响应结果：`results[].url`、`results[].title`、`results[].content` 可规范化为内部 `URL`、`Title`、`Snippet`。
