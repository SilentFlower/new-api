# Dashboard 与 Excel Build 薄层化

## Goal

将 Dashboard 分组/API Key 筛选、统计查询和 Excel 报表生成迁入独立领域文件与组件，使 `controller/usedata.go`、`model/usedata.go` 和页面入口只保留窄调用。

## Requirements

- 保持看板查询参数、用户/管理员权限、时间范围、分组和 Token 筛选行为不变。
- 保持 Excel 三张工作表、标题、样式、字段、缓存 Token 展示、汇总和明细上限不变。
- 保持大日志范围的流式扫描、请求取消和 ClickHouse 行为不变。
- Default/Classic 页面只拆分领域组件，不修改文案、交互或下载结果。
- 不引入新的通用报表框架。

## Acceptance Criteria

- [ ] Excel 生成不再位于 `controller/usedata.go` 主文件。
- [ ] Dashboard 专用查询不再大块位于 `model/log.go` 或 `model/usedata.go` 主文件。
- [ ] 前端筛选和导出组件独立挂载，查询参数 round-trip 不变。
- [ ] Excel 内容、样式、筛选、Token/cache 统计和取消行为保持不变。
- [ ] 后端相关测试、三数据库边界和两套前端质量门通过。

## Out Of Scope

- 新报表字段、样式调整、性能语义变化或 Dashboard 产品改版。
