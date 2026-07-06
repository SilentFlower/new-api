# 旧版数据看板统计筛选升级

## Goal

升级旧版 UI 数据看板的统计查询和导出筛选能力：管理员可以在数据看板搜索条件中多选分组，并让令牌下拉列表随分组联动；搜索和导出都支持令牌多选，查询出的统计数据和导出的 Excel 报表必须严格落在同一组筛选条件内。

## Background

- 旧版数据看板入口位于 `web/classic/src/components/dashboard`，核心状态在 `web/classic/src/hooks/dashboard/useDashboardData.js`。
- 现有搜索弹窗 `SearchModal.jsx` 的令牌名称是单选；导出弹窗 `ExportModal.jsx` 已有令牌多选，但只传 `token_names`，没有分组筛选，也没有令牌随分组联动。
- 数据看板主图数据来自 `/api/data/`、`/api/data/self`、`/api/data/users`，对应 `controller/usedata.go` 与 `model/usedata.go`。
- 导出报表来自 `/api/data/export`，当前从 `logs` 表聚合并支持 `token_names` 多值参数。
- 统计卡片里“统计额度 / RPM / TPM”来自 `/api/log/stat` 或 `/api/log/self/stat`，对应 `controller/log.go` 与 `model.SumUsedQuota`，目前只支持单个 `group` 和单个 `token_name`。
- 后端管理 API 多值查询参数必须优先使用重复 key，例如 `groups=a&groups=b`、`token_names=k1&token_names=k2`，并兼容旧单值参数。

## Requirements

1. 旧版 UI 数据看板搜索弹窗新增“分组”多选筛选项，仅管理员可见。
2. 管理员选择分组后，令牌下拉列表只展示所选分组内的令牌；未选择分组时展示全部令牌。
3. 搜索弹窗中的令牌名称改为多选；管理员仍需能区分同名令牌，避免只按名称误选。
4. 搜索确认后，数据看板主图、统计卡片和管理员用户排行/趋势必须使用同一组筛选条件。
5. 导出弹窗新增分组多选，并与令牌多选联动；导出请求必须带上所选分组和令牌过滤条件。
6. `/api/data/export` 导出的三个 Sheet 都必须应用同一筛选条件。
7. 后端新增或修改的多值参数必须兼容旧单值参数：
   - `token_names` / `token_names[]` 优先，旧 `token_name` 兜底；
   - `groups` / `groups[]` 优先，旧 `group` 兜底。
8. 分组筛选只按日志或统计记录的实际使用分组过滤，不改变令牌本身的配置。
9. 变更范围限定为旧版 UI 数据看板搜索/导出及其必要后端接口，不扩展到日志页、默认新版 UI 或其他搜索条件。

## Technical Notes

- 后端应将多值参数统一归一化：trim、去空、去重。
- `logs.group` 是 SQL 保留字列，后端查询必须使用现有跨库列名变量，保证 SQLite、MySQL、PostgreSQL 兼容。
- `quota_data` 当前没有 `group` 字段；当搜索条件包含分组或多令牌时，数据看板统计应从 `logs` 表聚合，避免分组过滤失真。
- 如果未选择分组和令牌，现有全量统计行为应保持不变。

## Acceptance Criteria

- [ ] 管理员打开旧版数据看板搜索弹窗，可以多选分组和令牌；选择分组后令牌选项立即收窄到对应分组。
- [ ] 管理员按多个分组查询后，数据看板主图、统计卡片、用户排行和用户趋势都只统计这些分组内的数据。
- [ ] 管理员同时选择多个令牌查询后，数据看板主图、统计卡片、用户排行和用户趋势都只统计这些令牌的数据。
- [ ] 管理员同时选择分组和令牌时，查询结果取两者交集。
- [ ] 导出报表支持同样的分组和令牌筛选，三个 Sheet 的数据范围一致。
- [ ] 旧调用方式 `token_name=x`、`group=y` 仍可用。
- [ ] 未选择分组和令牌时，现有数据看板和导出行为不回退。
- [ ] 普通用户旧版数据看板不出现分组筛选；令牌查询保持可用。

## Out Of Scope

- 不修改默认新版 UI。
- 不修改日志页、令牌页、渠道页或其他搜索表单。
- 不新增数据库字段或迁移。
- 不调整计费、日志写入或令牌配置语义。
