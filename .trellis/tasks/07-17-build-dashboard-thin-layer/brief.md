# Brief — Dashboard 与 Excel Build 薄层化

## Goal

- 将 Dashboard 分组/API Key 筛选、统计查询和 Excel 报表生成迁入独立领域文件与组件，使 `controller/usedata.go`、`model/usedata.go` 和页面入口只保留窄调用。

## Scope

- 新建 `controller/dashboard_filters.go` 承载 Dashboard 多值筛选参数解析。
- 新建 `controller/dashboard_export.go` 承载 Excel 导出 Controller、样式、Sheet 写入和响应。
- 新建 `model/dashboard_quota.go` 承载 Dashboard quota 查询和 `quota_data`/`logs` 兼容读取。
- 新建 `model/dashboard_export.go` 承载 Excel 导出日志单次遍历、汇总和模型明细聚合。
- 复核 Default/Classic 前端导出和筛选组件；已薄层时不制造无意义前端 diff。

## Non-Goals

- 不新增报表字段。
- 不调整 Excel 样式或 Dashboard 产品交互。
- 不改变性能语义、日志扫描语义或取消行为。
- 不引入新的通用报表框架。

## Key Context

- 看板查询参数、用户/管理员权限、时间范围、分组和 Token 筛选行为必须不变。
- Excel 三张工作表、标题、样式、字段、缓存 Token 展示、汇总和明细上限必须不变。
- 大日志范围继续保持流式扫描、请求取消和 ClickHouse 兼容行为。
- Default/Classic 页面只做薄层拆分，不修改文案、交互或下载结果。
- Default 已有独立 `DashboardExportDialog`、`ModelsFilter`、`api.ts` 和 `lib/filters.ts`。
- Classic 已有独立 `ExportModal`、`SearchModal`、`useDashboardData` 和 `dashboardFilters.js`。

## Acceptance

- Excel 生成不再位于 `controller/usedata.go` 主文件。
- Dashboard 专用查询不再大块位于 `model/log.go` 或 `model/usedata.go` 主文件。
- 前端筛选和导出组件独立挂载，查询参数 round-trip 不变。
- Excel 内容、样式、筛选、Token/cache 统计和取消行为保持不变。
- 后端相关测试、三数据库边界和两套前端质量门通过。

## Next Step

- 先跑后端导出和 Dashboard 查询基线，再按 Controller、Model、前端链路复核顺序做薄层化。
