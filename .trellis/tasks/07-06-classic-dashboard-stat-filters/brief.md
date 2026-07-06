# Brief — 旧版数据看板统计筛选升级

## Goal

- 升级旧版 UI 数据看板的统计查询和导出筛选能力：管理员可多选分组、令牌随分组联动，搜索结果和导出报表都严格按同一筛选条件统计。

## Scope

- 旧版数据看板搜索弹窗新增管理员分组多选，令牌名称改为多选。
- 导出弹窗新增分组多选，并让令牌多选随分组筛选联动。
- 后端 `/api/data/`、`/api/data/users`、`/api/data/self`、`/api/log/stat`、`/api/log/self/stat`、`/api/data/export` 支持必要的 `groups` / `token_names` 多值筛选。
- `/api/data/token-names` 返回令牌所属分组，供旧版 UI 进行分组联动。
- 后端多值参数兼容重复 key、括号数组和旧单值参数。

## Non-Goals

- 不修改默认新版 UI。
- 不修改日志页、令牌页、渠道页或其他搜索表单。
- 不新增数据库字段或迁移。
- 不调整计费、日志写入或令牌配置语义。

## Key Context

- 前端核心文件：`web/classic/src/hooks/dashboard/useDashboardData.js`、`SearchModal.jsx`、`ExportModal.jsx`。
- 后端 Controller：`controller/usedata.go`、`controller/log.go`。
- 后端 Model：`model/usedata.go`、`model/usedata_rankings.go`、`model/log.go`、`model/token.go`。
- 多值查询参数按项目规范使用重复 key：`groups=a&groups=b`、`token_names=k1&token_names=k2`，并兼容 `groups[]`、`token_names[]`、旧 `group`、旧 `token_name`。
- `logs.group` 是保留字列，查询必须使用现有跨库列名变量，保证 SQLite、MySQL、PostgreSQL 兼容。
- `quota_data` 没有分组字段；带分组或令牌筛选时应从 `logs` 表聚合，未筛选路径保持现有聚合表行为。
- 管理员同名令牌仍以 token name 过滤，前端通过用户名和分组标签降低误选风险；本任务不新增 token_id 筛选。

## Acceptance

- 管理员在旧版数据看板搜索弹窗中可多选分组和令牌；分组变化后令牌选项立即收窄。
- 多分组、多令牌、分组加令牌交集查询时，主图、统计卡片、用户排行和用户趋势都按同一条件统计。
- 导出报表支持同样的分组和令牌筛选，三个 Sheet 条件一致。
- 旧调用方式 `token_name=x`、`group=y` 仍可用。
- 未选择分组和令牌时，现有数据看板和导出行为不回退。
- 普通用户旧版数据看板不出现分组筛选，令牌查询保持可用。

## Next Step

- 用户确认 planning artifacts 和 brief 后，运行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/07-06-classic-dashboard-stat-filters`，再进入实现路由。
