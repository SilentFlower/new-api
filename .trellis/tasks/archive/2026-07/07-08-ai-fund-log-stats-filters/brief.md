# Brief — 优化 ai-fund 日志页统计与图表联动

## Goal

- 优化新 UI 公共 API Key 日志查看页 `/log` 的统计性能、筛选联动和消耗趋势展示，让统计卡片、模型调用分布、消耗趋势和日志表格使用一致查询条件。

## Scope

- 后端扩展 `/api/log/token/stat` 和 `/api/log/token/data`，支持 `type`、`model_name`、`request_id`、`start_timestamp`、`end_timestamp`，并兼容旧调用。
- 后端抽取或复用公共 token 日志过滤逻辑，保证统计、模型分布、消耗趋势和表格筛选语义一致。
- 优化统计接口，API Key 验证改用轻量 `GET /api/usage/token/`，统计接口减少同一请求内重复日志扫描，避免 MySQL 在大日志量下因 `/api/log/token/stat` 全历史聚合出现 CPU/内存飙升。
- 线上只读排查已证明现有 `logs` 索引无法覆盖 `token_id + type + created_at` 和 `token_id + created_at` 主查询路径，本任务新增对应复合索引迁移。
- 保持 Anthropic token 口径：`prompt_tokens + completion_tokens + cache_read + cache_creation`。
- 明确日志类型口径：`Requests/RPM` 跟随当前日志类型；`Usage/Tokens/TPM` 和消耗趋势只统计消费日志，非消费类型显示 0 或空态说明。
- 前端统一统计、图表、表格查询参数和 React Query key，筛选条件包括时间、类型、模型、请求 ID。
- 模型调用分布支持点击模型后写入模型筛选框、自动查询并重置分页。
- 消耗趋势按时间 bucket 聚合总 `quota`，修复横轴重复、顺序不稳、纵轴含义不清以及横纵坐标超出卡片渲染范围的问题。
- 新增文案走 default UI 的 i18n 流程，视觉和交互保持新 UI 风格，不引入 classic/Semi Design 或新重型图表库。

## Non-Goals

- 不修改 API Key 创建、迁移或 Cloudflare D1 同步逻辑。
- 不新增数据库字段；已证明性能需要，允许新增日志表复合索引迁移。
- 不改变公共日志页 Bearer API Key 认证方式。
- 不把公共日志页改成管理员日志页，不新增 channel、username、IP 等管理员敏感列。
- 不迁移 classic/Semi Design 图表实现，不新增重型图表库。

## Key Context

- 前端入口：`web/default/src/features/token-logs/index.tsx`、`api.ts`、`lib.ts`、`types.ts`。
- 后端入口：`controller/log.go` 的 token 日志接口；`model/log.go` 的 `GetTokenLogStat`、`GetTokenModelStats`、`GetTokenQuotaData`；`model/token_statistics.go` 的 Anthropic cache token 统计逻辑。
- 当前表格接口已读取完整筛选参数，但统计和图表接口只读取时间参数。
- 当前 API Key 验证会调用无时间范围的 `/api/log/token/stat`，容易触发全历史统计扫描并拉高 MySQL CPU/内存；应改为 `/api/usage/token/`，该接口返回 `code/message/data`。
- `quota_data` 后端按时间和模型聚合返回多行；前端必须再按 `created_at` 汇总为时间趋势，不能直接取最后 12 条原始行。
- 数据库查询必须兼容 SQLite、MySQL、PostgreSQL，并注意 LOG_DB/ClickHouse 日志库和 LIKE 安全处理。
- `logs` 普通数据库需要 `idx_logs_token_created_at(token_id, created_at)` 和 `idx_logs_token_type_created_at(token_id, type, created_at)`；ClickHouse 日志库保持现有 MergeTree 表结构。
- 公共日志页必须继续保护隐私边界，不暴露管理员敏感字段。

## Acceptance

- 有效 API Key 输入后，默认统计卡片、模型调用分布、消耗趋势和日志表格正常加载，认证阶段不触发全历史统计。
- 模型名、日志类型、request ID、时间范围筛选后，统计卡片、模型调用分布、消耗趋势和日志表格均按同一条件联动。
- 非消费日志类型下，请求数/RPM 正常联动，用量类指标为 0 或显示“用量统计仅适用于消费日志”的空态。
- `type=0` 全部日志下，请求数/RPM 覆盖全部类型，用量类指标只统计消费日志子集。
- 点击模型调用分布中的有效模型后，模型输入框自动填入并立即查询，分页回到第一页。
- 消耗趋势横坐标按时间 bucket 展示，顺序稳定；横轴标签、纵轴刻度和 tooltip 不超出卡片渲染范围，tooltip 显示完整时间、消耗额度、请求数和 token 用量。
- 统计接口减少重复日志扫描，Anthropic cache token 总量口径保持正确，相关后端测试继续通过。
- 新增或修改 API 参数保持旧时间参数调用兼容，公共日志页仍不展示管理员敏感字段。

## Next Step

- 等待用户确认 planning artifacts 和本 brief；确认后执行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/07-08-ai-fund-log-stats-filters`，进入 Phase 2.1，并按 route gate 选择实现方式。
