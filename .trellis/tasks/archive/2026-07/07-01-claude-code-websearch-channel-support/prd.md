# Claude Code WebSearch 渠道支持

## 目标

让不原生支持 Claude Code `web_search` 能力的三方渠道，可以在本系统中按渠道开启 WebSearch 增强，并可选择不同搜索供应商（至少包含 Tavily 与 AnySearch），同时保证转发请求体稳定，不破坏现有缓存命中逻辑。

## 背景

- 用户希望参考 `/root/project/my/sub2api` 中已有的 websearch 支持设计。
- 用户希望接入 `https://github.com/AgIzT/astrbot_plugin_anysearch` 的能力，并额外接入 Tavily。
- 能力开关需要落在渠道维度：不同渠道可以独立开启/关闭 websearch，并选择不同供应商。
- 请求体稳定是硬约束：实现不能因为注入搜索能力导致同一语义请求生成不稳定 body，从而影响缓存。

## 已确认事实

- 本仓库已经支持 Claude 原生 web_search 工具结构：
  - `dto.ClaudeWebSearchTool` / `dto.ClaudeWebSearchUserLocation` 定义在 `dto/claude.go`。
  - OpenAI `web_search_options` 转 Claude `web_search_20250305` 的逻辑在 `relay/channel/claude/relay-claude.go`。
  - Claude 响应中的 `server_tool_use.web_search_requests` 会进入 `claude_web_search_requests` 上下文并参与工具计费。
- 本仓库的渠道配置已有两个 JSON 槽：
  - `model.Channel.Setting` 解析为 `dto.ChannelSettings`，用于渠道通用行为配置。
  - `model.Channel.OtherSettings` 解析为 `dto.ChannelOtherSettings`，用于渠道类型相关透传/版本配置。
- `middleware/distributor.go` 选定渠道后会把 `channel.GetSetting()` 写入请求上下文，后续 `RelayInfo.ChannelSetting` 可直接读取渠道配置，不需要每次额外查库。
- `relay/claude_handler.go` 的非透传路径是：`ConvertClaudeRequest` → `common.Marshal` → `RemoveDisabledFields` → `ApplyParamOverride` → `NewOutboundJSONBody`。如果在该路径注入搜索结果、时间戳或随机 ID，会影响上游请求体稳定性。
- `common.Marshal` 当前是 `encoding/json.Marshal` 的项目包装；实现必须继续使用 `common.*` JSON 包装，不直接调用 `encoding/json`。
- sub2api 的参考实现包含：
  - `backend/internal/pkg/websearch/*`：`Provider` 抽象、Tavily provider、Brave provider、Manager、Redis 额度、代理、超时与故障切换。
  - `backend/internal/service/gateway_websearch_emulation.go`：仅当请求 `tools` 中只有 `web_search` 时拦截，调用搜索供应商，然后本地构造 Anthropic `server_tool_use` + `web_search_tool_result` 响应。
  - 参考实现会用 UUID 构造响应 ID，这适合响应侧，但不能进入上游请求体稳定性相关路径。
- `astrbot_plugin_anysearch` 是 AstrBot 插件，不是 Go 包；可复用的是 AnySearch HTTP/MCP 调用契约：
  - endpoint：`https://api.anysearch.com/mcp`
  - JSON-RPC method：`tools/call`
  - search 工具名：`search`
  - Authorization：可选 `Bearer <api_key>`
  - 支持参数：`query`、`max_results`、`freshness`、`content_types`
- sub2api 已有 Tavily provider：POST `https://api.tavily.com/search`，其旧实现把 `api_key` 放在 payload 中；本任务按 Tavily 当前官方契约改用 `Authorization: Bearer <api_key>` 请求头，响应 `results[].url/title/content` 可规范化为 `SearchResult`。
- Claude prompt caching 对请求前缀敏感；websearch 设计必须保持 `tools`、`system`、`messages` 在相同输入和配置下稳定，不能把搜索结果或随机响应 ID 注入上游请求体。
- 本仓库复制渠道接口 `controller/channel.go` 的 `CopyChannel` 会用 `model.GetChannelById(id, true)` 读取原渠道完整字段，然后浅拷贝 `model.Channel` 并重置 ID、创建时间、测试状态和可选余额字段；因此只要 WebSearch 密钥仍保存在原始 `Setting` 中，复制渠道可以天然继承 WebSearch 配置。脱敏必须只发生在管理 API 响应层，不能把脱敏后的 `api_key` 写回数据库。

## 已确认决策

- WebSearch 采用渠道级配置，不做本次全局供应商池。
- 渠道配置需要支持密钥脱敏与空 key 沿用旧 key，避免编辑时误清空密钥。
- 复制渠道必须复制 WebSearch 供应商、参数和实际 API Key；复制后的渠道应能立即使用 WebSearch。
- 本次只处理 Claude Code 的“纯 web_search 请求”：请求 `tools` 只包含一个 `web_search` / `web_search_20250305` / `google_search` 等搜索工具时，本系统本地拦截并构造 Claude 响应；web_search 与普通函数工具混合在同一轮中的编排不纳入本次。
- Tavily 启用时必须配置供应商密钥；AnySearch 供应商密钥可选。配置密钥时均通过 `Authorization: Bearer <api_key>` 请求头发送，不放入供应商 JSON 请求体。

## 需求

- R1：系统需要支持渠道级 WebSearch 配置，包括是否启用和供应商选择。
- R2：系统需要支持至少两个搜索供应商：Tavily 与 AnySearch。
- R3：Claude Code 请求触发 websearch 时，应由本系统为不支持该能力的上游渠道提供兼容能力。
- R4：实现必须保持请求体稳定，避免影响已有缓存键、缓存命中或请求去重行为。
- R5：如果渠道未开启 websearch 或供应商配置不可用，系统应返回符合当前 relay 错误模型的明确错误，而不是静默失败。
- R6：规划阶段必须对照本仓库现有 relay、渠道配置、缓存相关代码，以及 sub2api 的 websearch 实现后再确定技术方案。
- R7：前端需要在 default 渠道编辑表单中提供结构化配置控件，并补全 `en`、`zh`、`fr`、`ja`、`ru`、`vi` 翻译。
- R8：AnySearch 接入仅要求实现通用 search；extract、batch_search、vertical_search 可作为后续扩展，除非用户明确要求纳入本次。
- R9：搜索供应商返回结果需要规范化为稳定字段集合：`url`、`title`、`snippet`、可选 `page_age`。
- R10：本地构造给 Claude Code 的响应可以包含响应 ID，但这些 ID 必须只存在于响应侧，不得写入待转发上游的请求体。
- R11：渠道 WebSearch 配置需要存储在渠道级设置中，并包含启用状态、供应商、供应商参数与密钥配置状态；AnySearch 允许没有密钥。
- R12：管理 API 返回渠道详情/列表时不得泄露 WebSearch API Key；需要返回 `api_key_configured` 一类只读状态用于前端显示。
- R13：创建或更新渠道时，如果 WebSearch API Key 为空且原渠道已有密钥，必须沿用旧密钥；如果显式清空密钥，需要有明确字段或动作表达，避免空字符串误删。
- R14：复制渠道必须保留 WebSearch 配置和密钥，复制后的渠道在未手动修改配置时与原渠道能力一致。
- R15：本次只支持纯 web_search 请求拦截；混合工具调用必须保持原有转发路径，不应尝试本地编排。

## 验收标准

- [ ] 可以按渠道开启/关闭 Claude Code WebSearch 增强。
- [ ] 可以按渠道选择 Tavily 或 AnySearch 作为搜索供应商。
- [ ] 对不支持原生 websearch 的三方渠道，开启后可以完成 Claude Code websearch 场景。
- [ ] 对未开启或配置错误的渠道，返回清晰且符合项目错误处理规范的错误。
- [ ] 相同语义请求在未改变用户输入与渠道配置时，转发请求体保持稳定，不引入随机顺序、时间戳、非确定性字段等缓存破坏因素。
- [ ] 方案说明明确覆盖 `/root/project/my/sub2api` 参考实现的可复用点与不采用点。
- [ ] 默认前端可以配置渠道级 WebSearch 开关与供应商选择。
- [ ] AnySearch provider 使用 JSON-RPC `tools/call` 的 `search` 工具完成查询，并将文本结果规范化为系统内部结果结构。
- [ ] Tavily provider 查询结果可规范化为系统内部结果结构。
- [ ] 单元测试覆盖：渠道配置解析、请求体稳定、纯 web_search 请求识别、AnySearch/Tavily 响应规范化、错误路径。
- [ ] 管理 API 的渠道列表和详情不会返回明文 WebSearch API Key。
- [ ] 编辑已配置 WebSearch 的渠道时，不重新输入 API Key 也不会丢失原密钥。
- [ ] 复制已配置 WebSearch 的渠道后，新渠道保留供应商、参数和已配置的密钥配置状态，并可正常执行 WebSearch。
- [ ] 只有请求 `tools` 恰好包含一个搜索工具时才触发本地 WebSearch 模拟；包含普通工具或多个工具的请求不走本地模拟。

## 非本次范围

- 不要求所有非 Claude Code 格式一次性支持 WebSearch。
- 不在规划阶段直接实现代码。
- 不要求本次支持 AnySearch 的网页正文提取、批量搜索或垂直搜索。
- 不要求复刻 sub2api 的账号级三态覆盖，除非后续明确需要。
- 不支持 web_search 与普通工具混合在同一轮请求中的本地工具循环编排。
