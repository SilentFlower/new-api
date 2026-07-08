# 优化 ai-fund 日志页统计与图表联动实现计划

## 前置检查

1. 读取本任务 `prd.md`、`design.md`。
2. 使用 `trellis-before-dev` 读取后端 API/日志/数据库规范和 `web/default` 前端规范。
3. 确认工作区状态，避免覆盖其他窗口改动。
4. 实现前按 Trellis route gate 选择 implement 路由。

## 实现顺序

### 1. 后端参数结构与过滤 helper

- 在 `controller/log.go` 或 `model/log.go` 增加公共 token 日志过滤参数结构。
- Controller 的 `/api/log/token/stat`、`/api/log/token/data` 解析 `type/model_name/request_id/start_timestamp/end_timestamp`。
- Model 层抽取 token 日志过滤 helper：
  - `token_id`
  - `type`
  - `created_at`
  - `model_name` 安全 LIKE
  - `request_id` 精确匹配
- 确认 ClickHouse 日志库分支仍使用现有 LIKE helper。

### 2. 后端统计性能和语义

- 改造 `GetTokenLogStat`：
  - `count` 按当前日志类型筛选。
  - `rpm` 按当前日志类型筛选最近 60 秒。
  - `quota/prompt_tokens/completion_tokens/total_tokens/tpm` 只统计消费日志。
  - 非消费 type 时用量字段直接为 0，不扫描消费日志。
  - `type=0` 时用量字段统计消费日志子集。
- 减少同一请求内重复调用 `sumStatisticTokenUsedFromLogQuery()`。
- 避免认证阶段或默认首屏触发无时间范围的全历史统计，降低 MySQL CPU/内存压力。
- Go 侧批量扫描只选择 token 统计必要字段，避免读取和保留无关日志列。
- 保持 Anthropic cache token 口径。

### 3. 后端图表接口

- 改造 `GetTokenModelStats` 支持完整筛选条件。
- 改造 `GetTokenQuotaData` 支持完整筛选条件。
- 非消费 type 下 `quota_data` 直接返回空数组。
- 保留 `/api/log/token/data` 的 1 个月时间跨度限制，并确保错误返回前端可展示。

### 4. 日志统计复合索引

- 在 `Log` 模型上增加 `idx_logs_token_created_at(token_id, created_at)`，覆盖默认全部类型的公共日志时间范围统计。
- 在 `Log` 模型上增加 `idx_logs_token_type_created_at(token_id, type, created_at)`，覆盖消费用量、TPM、模型分布和趋势查询。
- 复用现有 `migrateDB()` / `migrateLOGDB()` 的 `AutoMigrate(&Log{})` 路径，不写数据库专用 DDL。
- 增加迁移测试，验证 AutoMigrate 能创建两个索引。
- 生产环境只读排查确认缺索引，但本任务不直接在线执行 DDL；上线时由新版本启动迁移或按发布操作单在低峰执行。

### 5. 前端轻量 API Key 验证

- 在 `web/default/src/features/token-logs/api.ts` 增加 `getTokenUsage()` 或等价封装，调用 `/api/usage/token/`。
- 增加响应类型，注意该接口返回 `code` 而不是 `success`。
- `AuthPanel` 提交验证改用轻量 usage 接口，不再调用无时间范围的 `getTokenLogStat()`。
- 保留 401、403、429 的错误提示。

### 6. 前端筛选联动

- 统一统计、图表和表格的查询参数构造。
- `getTokenLogStat()` 和 `getTokenLogChartData()` 接收完整 `TokenLogQueryParams` 子集。
- React Query key 包含完整 `appliedFilters`。
- 查询按钮应用筛选时，三块区域全部刷新。
- 重置按钮清空筛选时，三块区域全部回到默认当天范围。

### 7. 模型分布点击筛选

- `TokenLogCharts` 增加 `onSelectModel` 回调。
- 模型分布项改为可点击按钮或语义化可交互元素，保留 tooltip/aria-label。
- 点击模型后更新模型输入框、应用筛选、刷新统计/图表/表格。
- 表格分页在筛选变化时重置到第一页。

### 8. 消耗趋势修复

- 前端按 `created_at` 聚合 `quota_data`，得到总消耗时间序列。
- 横轴显示时间 bucket，顺序稳定。
- 纵向高度使用聚合后的 `quota`。
- tooltip 显示完整时间、消耗额度、请求数和 token 用量。
- 重做图表内部布局边界，给 y 轴刻度、柱状绘图区和 x 轴标签预留固定空间。
- 控制横轴标签密度，避免标签超出卡片、被裁切或互相覆盖。
- 确保 tooltip 在卡片内或视口内可见，不遮挡主要坐标内容。
- 非消费类型筛选时显示“用量统计仅适用于消费日志”的空态。
- 保持 default UI 风格，不引入新 UI 库。

### 9. i18n

- 新增文案使用英文 key。
- 按项目流程同步 locale 文件。

## 验证计划

后端：

```bash
go test ./controller ./model
```

前端：

```bash
cd web/default && bun run i18n:sync
cd web/default && bun run build
```

若当前环境缺少 Bun 或依赖，记录失败原因，并至少运行可用的 TypeScript/目标文件检查。

## 回归场景

- 输入有效 API Key，认证阶段不触发全历史统计。
- 默认当天范围加载统计、图表、表格。
- 输入模型名后查询，统计、模型分布、消耗趋势、表格全部联动。
- 点击模型分布项后，模型输入框自动填入并刷新。
- 输入 request ID 后，统计、图表、表格全部收窄到该请求。
- 选择错误日志类型后，请求数/RPM 联动，用量类指标为 0 或空态说明。
- 选择全部日志类型时，请求数/RPM 覆盖全部类型，用量类指标只统计消费日志。
- 消耗趋势横轴不重复混乱，tooltip 数值与柱高一致。
- 消耗趋势横轴、纵轴和 tooltip 不超出卡片渲染范围，在截图所示密集时间点场景下仍可读。
- 无效 API Key、封禁用户、限频错误提示仍正常。

## 风险文件

- `controller/log.go`
- `model/log.go`
- `model/token_statistics.go`
- `web/default/src/features/token-logs/api.ts`
- `web/default/src/features/token-logs/lib.ts`
- `web/default/src/features/token-logs/types.ts`
- `web/default/src/features/token-logs/index.tsx`
- `web/default/src/i18n/locales/*.json`
