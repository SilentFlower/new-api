# Dashboard 与 Excel Build 薄层化设计

## 1. 目标结构

### 新建文件

| 文件 | 职责 |
| --- | --- |
| `controller/dashboard_filters.go` | Dashboard 多值筛选参数解析，供数据看板和日志统计复用 |
| `controller/dashboard_export.go` | 数据看板 Excel 导出 Controller、工作簿样式、Sheet 写入和下载响应 |
| `model/dashboard_quota.go` | Dashboard quota 查询、用户维度聚合和 `quota_data`/`logs` 兼容读取 |
| `model/dashboard_export.go` | Excel 导出所需日志单次遍历、汇总聚合、模型明细聚合和明细行限制 |

### 保持现状的前端薄层

| 文件 | 现状 |
| --- | --- |
| `web/default/src/features/dashboard/components/models/dashboard-export-dialog.tsx` | Default 导出弹窗已独立 |
| `web/default/src/features/dashboard/components/models/models-filter-dialog.tsx` | Default 筛选弹窗已独立 |
| `web/default/src/features/dashboard/api.ts` / `lib/filters.ts` | 查询参数构造已集中 |
| `web/classic/src/components/dashboard/modals/ExportModal.jsx` | Classic 导出弹窗已独立 |
| `web/classic/src/components/dashboard/modals/SearchModal.jsx` | Classic 搜索弹窗已独立 |
| `web/classic/src/hooks/dashboard/useDashboardData.js` / `helpers/dashboardFilters.js` | Classic 数据加载和参数构造已从页面组件拆出 |

本任务不为了制造 diff 重拆已独立的前端组件。前端只做链路复核；若检查发现页面入口仍承载大块导出/筛选实现，再做最小迁移。

### 修改的热点文件

| 文件 | 治理后保留内容 |
| --- | --- |
| `controller/usedata.go` | Flow 看板 Controller 和少量稳定入口；Excel/筛选实现迁出 |
| `model/usedata.go` | `QuotaData` 实体、缓存写入和 `SaveQuotaDataCache` 等稳定数据采集入口 |
| `model/log.go` | 日志实体、通用日志查询、日志清理和非 Dashboard 专属逻辑 |

## 2. 行为数据流

### Dashboard 查询

Router 继续注册原有 `GetAllQuotaDates`、`GetQuotaDatesByUser`、`GetUserQuotaDates`、`GetAllTokenNames` 和 `GetSystemStats`，不改路由、权限或响应结构。Controller 继续解析 `start_timestamp`、`end_timestamp`、`username`、`token_names/token_name`、`groups/group`，多值参数继续 trim、去空、去重，并兼容括号数组格式。

Model 层继续保持无筛选时读取 `quota_data`，存在 token/group 筛选时回落到 `logs` 表聚合，以保留历史数据和分组过滤语义。Anthropic 缓存 Token 口径继续复用 `model/token_statistics.go` 中的统一聚合函数。

### Excel 导出

`ExportQuotaDataExcel` 迁入独立 Controller 文件，但函数名、路由、权限、参数和响应头不变。工作簿仍包含“汇总统计”“模型明细”“请求日志”三张 Sheet，保留标题、元信息、表头、筛选 Table、冻结窗格、颜色、数字格式、缓存 Token 文本展示和文件名规则。

导出聚合继续由 `ProcessLogsForExport` 单次遍历 `LOG_DB` 完成。查询必须使用请求上下文、最小字段选择、`created_at asc` 稳定排序和 `Rows()`，明细回调只写前 500000 条，请求汇总和模型明细仍覆盖完整匹配范围。

### 前端

Default 和 Classic 的导出/筛选入口继续调用同一组 API：

- `GET /api/data`
- `GET /api/data/users`
- `GET /api/data/self`
- `GET /api/data/export`
- `GET /api/data/token-names`

本轮不改显示文案、交互、i18n key、下载文件处理或 React/Semi 组件结构；只在 Check-All 中确认参数 round-trip 没有漂移。

## 3. 兼容性与安全

- 不改 Router 注册、Gin handler 名、HTTP 方法、鉴权中间件或错误文案。
- 不改 Excel Sheet 名、列名、行布局、样式、合计公式、静默截断上限或响应头。
- 不改 Dashboard 查询参数、多值参数兼容规则、用户/管理员权限和时间跨度限制。
- 不改 `QuotaData`、`LogSummaryByKey`、`LogDetailByKeyModel` 字段或 JSON 标签。
- 不改 `LOG_DB`、`DB`、ClickHouse 遍历、三数据库兼容查询和任何迁移/索引。
- 不改 Default/Classic 前端文案和 UI 行为。

## 4. 回滚

- 将 `controller/dashboard_export.go` 与 `controller/dashboard_filters.go` 的内容原样移回 `controller/usedata.go`。
- 将 `model/dashboard_quota.go` 的内容原样移回 `model/usedata.go`。
- 将 `model/dashboard_export.go` 的内容原样移回 `model/log.go`。
- 删除新增领域文件；不涉及数据、配置、索引或迁移回滚。

## 5. 上游同步复核点

- 上游是否修改 `controller/usedata.go` 的数据看板路由、参数解析或 Excel 响应。
- 上游是否修改 `model/usedata.go` 的 `quota_data` 聚合、缓存写入或 `QuotaData` 字段。
- 上游是否修改 `model/log.go` 的日志实体、`LOG_DB` 遍历、ClickHouse 排序或 Token 统计口径。
- Default/Classic 前端是否调整 Dashboard 筛选参数、导出入口或下载处理。
