# Brief — 数据看板 Excel 导出增加分组列并优化性能

## Goal

- 为管理员数据看板的三个 Excel Sheet 补充分组维度，并通过单次日志遍历和 Excel 流式写入降低大导出的数据库与内存开销。

## Scope

- Sheet 1“汇总统计”增加分组列，按“分组 + API Key + 用户”聚合。
- Sheet 2“模型明细”按“分组 + API Key + 用户 + 模型”聚合，标题与小计明确归属分组。
- Sheet 3“请求日志”增加分组列，继续按时间升序输出。
- 将三条日志数据链路合并为一次顺序遍历，同时生成两类聚合并流式写入请求日志。
- 三个 Sheet 全部改用 excelize StreamWriter，避免大规模单元格常驻内存。
- 保持请求日志最多 500000 行；超过部分不输出、不拒绝、不提示，但 Sheet 1、Sheet 2 仍统计完整范围。
- 增加 Model 分组聚合、筛选一致性、Context 取消和 Controller Excel 文件契约测试。

## Non-Goals

- 不修改前端导出弹窗、API 路由、管理员权限、文件名或下载交互。
- 不增加普通用户导出、CSV、异步导出、对象存储或导出历史。
- 不增加时间范围限制、截断提示、数据库字段、迁移或索引变更。
- 不修改其他数据看板统计接口和日志写入逻辑。

## Key Context

- 入口为 `GET /api/data/export`，主要实现位于 `controller/usedata.go`、`model/log.go`、`model/token_statistics.go`。
- `model.Log.Group` 已保存请求发生时的分组；历史空分组保持空字符串，不推断为 `default`。
- 时间、分组多选和 API Key 多选必须继续同时作用于三个 Sheet。
- Sheet 1、Sheet 2 的 Token 统计必须保留 Anthropic 缓存读取和缓存写入口径。
- `group` 是 SQL 保留字，查询必须兼容 SQLite、MySQL、PostgreSQL 和独立 ClickHouse 日志库。
- 单次遍历采用带请求 Context 的 Rows 顺序读取；ClickHouse `id` 可能恒为 `0`，不能依赖 GORM `FindInBatches` 的主键游标。
- StreamWriter 必须先设置列宽、按递增行号写入、逐 Sheet Flush，且同一 Sheet 不混用普通写入 API。

## Acceptance

- 不同分组下的同名 API Key 在 Sheet 1、Sheet 2 中分别统计，Sheet 3 每行包含真实日志分组。
- 三个 Sheet 的筛选范围一致，Anthropic 缓存 Token 统计不回退。
- 匹配消费日志只遍历一次，不再构造最多 500000 条 `*Log` 的内存切片。
- 请求日志仍最多输出按时间升序的前 500000 条，汇总与模型明细覆盖完整匹配范围。
- 客户端取消后查询和生成能够停止，生成文件可被 Excel/WPS 正常打开。
- 相关 Model、Controller 测试及全量 Go 测试通过。

## Next Step

- 用户确认 planning artifacts 与本 brief 后，运行 `task.py start` 激活任务，再进入 `trellis-route(implement)` 执行实现。
