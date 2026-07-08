# 优化 ai-fund 日志页统计与图表联动

## Goal

优化新 UI 公共 API Key 日志查看页（`/log`）的统计性能、筛选联动和图表展示。用户输入 API Key 后，上方统计卡片、模型调用分布、消耗趋势和下方日志表格应使用一致的查询条件，并且模型分布图可以作为快速筛选入口。

## Background

- 当前任务针对用户描述的 ai-fund 查日志页面；代码侧对应新 UI 公共 API Key 日志查看器 `web/default/src/features/token-logs/`，路由为 `/log`。
- 当前公共日志页使用 API Key 作为 Bearer token 调用：
  - `/api/log/token/stat`
  - `/api/log/token/data`
  - `/api/log/token`
- 当前前端表格查询参数包含 `type`、`model_name`、`request_id`、`start_timestamp`、`end_timestamp`，见 `web/default/src/features/token-logs/lib.ts:56`。
- 当前统计和图表查询只传时间参数：
  - `getTokenLogStat()` 类型只接收 `start_timestamp/end_timestamp`，见 `web/default/src/features/token-logs/api.ts:59`。
  - `getTokenLogChartData()` 类型只接收 `start_timestamp/end_timestamp`，见 `web/default/src/features/token-logs/api.ts:74`。
  - `TokenLogsWorkspace` 的 `timeParams` 由 `buildTokenLogTimeParams(appliedFilters)` 构造，见 `web/default/src/features/token-logs/index.tsx:726`。
- 当前后端表格接口已读取完整过滤参数，见 `controller/log.go:156`；统计接口和图表接口只读取时间参数，见 `controller/log.go:182` 和 `controller/log.go:202`。
- 当前统计性能风险明确：`GetTokenLogStat()` 先 SQL 汇总，再调用 `sumStatisticTokenUsedFromLogQuery(baseQuery)` 批量扫描日志计算 total_tokens，随后 RPM/TPM 又调用同一个 Go 侧扫描逻辑，见 `model/log.go:847`、`model/log.go:863`、`model/log.go:881`。在 MySQL 日志量较大时，该接口会导致数据库 CPU 和内存明显飙升，是本任务必须优化的核心问题。
- `sumStatisticTokenUsedFromLogQuery()` 通过 `iterateStatisticLogs()` 分批读取日志并解析 `Other` 计算 Anthropic cache token 口径，见 `model/token_statistics.go:277` 和 `model/token_statistics.go:123`。
- 线上 MySQL 只读排查确认 `logs` 约 162 万行，现有索引只有 `token_id` 单列和 `created_at,type` 组合；`token_id + type + created_at` 与 `token_id + created_at` 查询路径缺复合索引，统计接口会先扫描某个 token 的大量日志再过滤时间和类型。需要把索引优化纳入本任务。
- 当前前端 API Key 验证调用 `getTokenLogStat(nextClient)` 且不带时间范围，见 `web/default/src/features/token-logs/index.tsx:829`；这会触发全历史统计扫描，是首屏慢的高风险根因。
- 仓库已有轻量只读接口 `GET /api/usage/token/`，同样使用 `TokenAuthReadOnly()`，返回 token 名称、可用额度、已用额度、模型限制和过期时间，不扫描日志，见 `router/api-router.go:253` 和 `controller/token.go:118`。
- 当前模型调用分布只是静态条形列表，没有点击模型后回填筛选并查询的行为，见 `web/default/src/features/token-logs/index.tsx:284`。
- 当前消耗趋势是自绘柱状图，只取最后 12 条 `quota_data` 原始行，按 `quota` 归一化高度，并在固定高度容器里直接渲染柱子和 x 轴标签，见 `web/default/src/features/token-logs/index.tsx:253` 至 `web/default/src/features/token-logs/index.tsx:335`。后端 `GetTokenQuotaData()` 返回的是按时间和模型聚合的多行数据，见 `model/log.go:924`，因此前端直接取最后 12 行会混入模型维度，容易导致横轴重复、顺序不稳、纵轴含义不清；同时横纵坐标和 tooltip 可能超出绘图区或卡片可视范围。
- 后端需要兼容现有日志数据库能力，包含普通 GORM 数据库和 ClickHouse 日志库；模糊筛选需要沿用现有 LIKE 安全处理。

## Requirements

### R1：统计卡片查询性能优化

- API Key 验证不得再调用无时间范围的 `/api/log/token/stat`；应改用轻量只读接口，例如 `GET /api/usage/token/`，只验证 Key 有效性和读取基础 token 信息。
- `/api/log/token/stat` 在大日志量下必须降低 MySQL CPU 和内存压力，减少重复扫描，避免一次查询为了统计卡片多次全量遍历同一批日志。
- `logs` 表必须补充公共 API Key 日志统计需要的复合索引，覆盖 `token_id + created_at` 和 `token_id + type + created_at` 过滤路径；迁移方式必须兼容 SQLite、MySQL、PostgreSQL，并对独立 `LOG_SQL_DSN` 日志库生效。
- `/api/log/token/stat` 默认进入页面时只应按默认时间范围查询，不允许认证阶段或首屏无条件扫描该 API Key 的全历史日志。
- 优化后必须保持 Anthropic cache token 统计口径：`prompt_tokens + completion_tokens + cache_read + cache_creation`。
- 优化方案不得为了速度牺牲公共日志页的隐私边界，不得返回 channel、username、IP 等管理员敏感字段。
- 若必须保留 Go 侧解析 `Other` 才能准确计算 total_tokens，需要尽量复用同一过滤 query 的扫描结果或减少不必要字段/次数。

### R2：统计、图表、表格筛选条件一致

- 上方统计卡片必须跟随当前已应用筛选条件，而不只是时间范围。
- 模型调用分布和消耗趋势必须跟随当前已应用筛选条件，而不只是时间范围。
- 需要联动的筛选条件至少包括：
  - 时间范围
  - 日志类型 `type`
  - 模型名称 `model_name`
  - 请求 ID `request_id`
- 公共 API Key 日志接口不应新增 token/group 筛选，因为 token 已由 Bearer API Key 决定，group 对公共模式不是当前 UI 筛选项。
- 前端查询参数构造需要复用或扩展现有 `TokenLogQueryParams`，避免统计/图表和表格各维护一套不一致的字段。
- 日志类型口径按以下规则处理：
  - 请求数 `Requests` 按当前日志类型筛选联动。
  - 用量类指标 `Usage`、`Tokens`、`TPM`、消耗趋势只对消费日志有业务含义。
  - `RPM` 表示当前日志类型筛选下最近 60 秒请求/日志速率；`TPM` 表示当前消费日志子集的最近 60 秒 token 速率。
  - 当用户筛选错误、退款、管理等非消费日志类型时，用量类指标显示 0 或空态，并明确提示“用量统计仅适用于消费日志”。
  - 当日志类型为全部时，用量类指标只统计当前条件下的消费日志子集，避免把非消费日志混入额度或 token 口径。

### R3：模型调用分布点击筛选

- 用户点击“模型调用分布”中的具体模型后：
  - 将该模型名填入模型筛选输入框。
  - 自动应用筛选并刷新统计卡片、图表和日志表格。
  - 分页重置到第一页。
- 如果当前已经筛选同一个模型，重复点击应保持结果稳定，不产生额外错误状态。
- 空模型名或 Unknown 项不得把无意义文本写入筛选框。

### R4：消耗趋势图修复

- 消耗趋势应按时间 bucket 聚合展示，不应把同一个时间下多个模型行当作多个横坐标点直接展示。
- 横坐标应表达时间，避免重复、截断不清或顺序不稳定。
- 横轴标签、纵轴刻度、tooltip 和柱状绘图区必须被约束在图表卡片内部，不得出现坐标文本超出渲染区域、被裁切或覆盖卡片边界的问题。
- 纵坐标/高度继续使用消耗额度 `quota`，并在 tooltip 或标题中明确单位；纵轴刻度需要与绘图区留白协调，不能挤占或遮挡柱状图。
- Token 用量继续由上方 `Tokens` 卡片表达，避免“消耗趋势”同时混用额度和 token 两套口径。
- 消耗趋势默认展示按时间聚合后的总消耗，不按模型拆分堆叠或分组；点击模型调用分布筛选某个模型后，趋势自然变为该模型的总消耗趋势。
- 图表应显示完整时间范围内有意义的 bucket；不应无条件只截取最后 12 条原始行导致模型维度污染趋势。
- 图表需要保持新 UI 风格，不引入 classic/Semi Design，也不新增重型图表库。

### R5：错误处理与兼容

- `/api/log/token/data` 当前限制时间跨度不能超过 1 个月；如继续保留该限制，前端需要显示清晰错误，不应让图表静默空白。
- 统计/图表接口新增过滤参数时，需要兼容旧调用方只传时间参数。
- 模型名称模糊匹配应与日志表格一致，支持现有 LIKE 语义和安全转义。
- 请求 ID 匹配应使用精确匹配，避免误查。

## Acceptance Criteria

- [ ] 在 `/log` 输入有效 API Key 后，默认统计卡片、模型调用分布、消耗趋势和日志表格能正常加载。
- [ ] API Key 验证阶段不触发全历史日志统计；无效 Key、封禁用户、限频错误仍按现有错误提示展示。
- [ ] 输入模型名并点击查询后，统计卡片、模型调用分布、消耗趋势和日志表格均只反映该模型匹配范围。
- [ ] 修改日志类型后，统计卡片、模型调用分布、消耗趋势和日志表格均按该类型联动；用量类指标只统计消费日志，非消费日志类型显示 0 或空态说明。
- [ ] 输入 request ID 后，统计卡片、模型调用分布、消耗趋势和日志表格均反映该 request ID 范围。
- [ ] 点击模型调用分布中的模型项后，模型输入框自动填入该模型并立即查询，分页回到第一页。
- [ ] 消耗趋势横坐标按时间 bucket 展示，顺序稳定，不因多模型行导致同一时间重复出现多个不清晰柱。
- [ ] 消耗趋势横轴标签、纵轴刻度和 tooltip 均在卡片绘图区内正常显示，不超出渲染范围，不被裁切，不遮挡柱状图。
- [ ] 消耗趋势 tooltip 显示完整时间和对应消耗值；纵向比例与显示值一致。
- [ ] 统计接口性能有可验证改善：同等筛选条件下减少重复日志扫描，或通过测试/代码审查证明统计查询不再为同一时间范围重复遍历完整日志集，并且认证阶段不再触发 MySQL 全历史统计扫描；公共日志统计索引可由迁移创建。
- [ ] Anthropic cache token 总量口径保持正确，现有 `model/token_statistics_test.go` 相关测试继续通过。
- [ ] 公共日志页仍不展示管理员敏感字段。
- [ ] 新增或修改的 API 参数保持旧时间参数调用兼容。

## Out of Scope

- 不修改 API Key 创建、迁移或 Cloudflare D1 同步逻辑。
- 不新增数据库字段；本任务允许新增日志表复合索引迁移，因为线上执行计划已证明现有索引无法覆盖公共日志统计主路径。
- 不改变公共日志页的认证方式；仍使用 Bearer API Key。
- 不把公共日志页改成后台管理员日志页，不新增 channel、username、IP 等管理员列。
- 不迁移 classic/Semi Design 图表实现。

## Open Questions

- 无。
