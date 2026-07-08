# Brief — 统计总 Token 包含 Anthropic 缓存

## Goal

- 将 Anthropic/Claude 请求的总 Token 统计口径调整为 `普通输入 + 缓存读取 + 缓存写入 + 输出`，避免数据看板、排行、导出低估缓存请求规模。

## Scope

- 新增后端统一统计 helper，从日志字段和 `other` 中计算统计 Token。
- 修正新消费日志写入 `quota_data.token_used` 的口径。
- 修正从 `logs` 聚合的统计入口，包括数据看板筛选图表、导出 Sheet 1/2、单 Token 趋势图。
- 实现 `quota_data` 历史重建/回算，让旧 UI 无筛选数据看板和默认 UI 排行使用新口径。
- 保持导出 Sheet 3 能看清普通输入、缓存读、缓存写、输出。
- 增加或更新模型层测试覆盖缓存读、缓存写、5m/1h 拆分缓存写、历史回算幂等和过滤语义。

## Non-Goals

- 不调整实际扣费金额、倍率或结算逻辑。
- 不改 Anthropic/OpenAI 对外响应体 usage 格式。
- 不删除日志明细里的缓存读写拆分展示。
- 不使用数据库专属 JSON 函数。

## Key Context

- 缓存字段在 `logs.other` JSON 字符串中：`cache_tokens`、`cache_creation_tokens`、`cache_creation_tokens_5m`、`cache_creation_tokens_1h`。
- 为兼容 SQLite、MySQL、PostgreSQL，历史聚合和回算需要在 Go 侧解析 `other`，不能依赖 SQL JSON 函数。
- `quota_data` 历史值已按旧口径写死；无筛选数据看板和模型排行读取该表，因此必须回算。
- 关键文件：`model/log.go`、`model/usedata.go`、`model/usedata_rankings.go`、`controller/usedata.go`、`model/dashboard_filters_test.go`。
- 回滚代码不会自动恢复已回算的 `quota_data` 旧口径；如需回退统计口径，需要再次重建。

## Acceptance

- Anthropic 原生格式消费日志的总 Token 统计包含普通输入、缓存读、缓存写和输出。
- OpenAI 格式转 Claude 的响应体 usage 行为不被破坏。
- 旧 UI 数据看板带筛选图表、单 Token 趋势图、导出 Sheet 1/2 的历史统计按新口径展示。
- 无筛选数据看板和模型排行使用回算后的 `quota_data` 新口径。
- 导出 Sheet 3 不产生双算误导。
- 相关 Go 测试通过，至少覆盖缓存读、缓存写、5m/1h 拆分缓存写和回算幂等。

## Next Step

- 用户确认 planning artifacts 和 brief 后，运行 `task.py start`，随后进入 `trellis-route(implement)`。
