# 报表导出支持令牌筛选和快捷时间

## 目标

增强数据看板的报表导出和搜索条件能力，让管理员导出 Excel 报表时可以按令牌名称筛选，并让搜索条件支持常用自然时间范围的一键填充。

## 已确认事实

- 经典前端已有数据看板搜索弹窗：`web/classic/src/components/dashboard/modals/SearchModal.jsx`。
- 经典前端已有导出报表弹窗：`web/classic/src/components/dashboard/modals/ExportModal.jsx`。
- 经典前端数据看板逻辑在 `web/classic/src/hooks/dashboard/useDashboardData.js`：
  - 搜索条件已有 `token_name` 单选下拉。
  - 管理员令牌下拉数据来自 `/api/data/token-names`，普通用户令牌下拉数据来自 `/api/token/?p=1&size=100`。
  - 导出报表当前只向 `/api/data/export` 传 `start_timestamp` 和 `end_timestamp`。
- 后端导出接口在 `controller/usedata.go` 的 `ExportQuotaDataExcel`。
- 后端导出数据查询在 `model/log.go`：
  - `GetLogSummaryByKey(startTimestamp, endTimestamp, username, tokenName)`
  - `GetLogDetailByKeyModel(startTimestamp, endTimestamp, username, tokenName)`
  - `GetLogsForExport(startTimestamp, endTimestamp, username, tokenName)`
  - 当前只支持单个 `tokenName`，且导出接口调用时传空字符串。
- 后端已有 `/api/data/token-names`，由 `controller.GetAllTokenNames` 调用 `model.GetAllTokenNames` 返回去重令牌名称及用户名。
- 项目要求数据库查询兼容 SQLite、MySQL、PostgreSQL。

## 需求

1. 报表导出弹窗新增“令牌名称”下拉多选。
2. 选择多个令牌名称后导出的 Excel 三个 Sheet 都只包含这些令牌对应的数据。
3. 不选择令牌时保持原行为：导出所选时间范围内全部令牌数据。
4. 导出弹窗的令牌多选不默认带入搜索条件弹窗当前已选令牌；导出筛选与页面搜索筛选相互独立。
5. 后端导出接口支持多令牌筛选，并保留原 `token_name` 单值参数兼容旧调用。
6. 搜索条件弹窗新增快速查询标签：
   - 查询 1 天内的
   - 查询本周的
   - 查询上周的
   - 查询本月的
   - 查询上月的
7. 点击快速查询标签后自动填充搜索表单的起始时间和结束时间。
8. 快捷时间范围统一使用本地时间：
   - 起始时间为当天或周期第一天 `00:00:00`
   - 截止时间为当天或周期最后一天 `23:59:59`
   - 周范围固定按周一到周日计算。
9. 搜索条件原有自定义起止时间、时间粒度、用户名、令牌筛选能力保持可用。

## 验收标准

- [ ] 管理员打开经典数据看板导出报表弹窗时，可以看到令牌名称多选下拉。
- [ ] 管理员可在导出报表弹窗选择多个令牌并成功下载 Excel。
- [ ] 导出的“汇总统计”“模型明细”“请求日志”三个 Sheet 都按所选令牌过滤。
- [ ] 导出报表未选择令牌时，仍导出所选时间范围内全部令牌数据。
- [ ] 导出弹窗打开时不默认带入搜索弹窗当前已选令牌。
- [ ] `/api/data/export` 支持多值 `token_names` 查询参数，并兼容旧的 `token_name` 参数。
- [ ] 搜索条件弹窗展示 5 个快速查询标签。
- [ ] 点击“查询 1 天内的”后，起始时间为当天 `00:00:00`，结束时间为当天 `23:59:59`。
- [ ] 点击“查询本周的”后，起始时间为本周周一 `00:00:00`，结束时间为本周周日 `23:59:59`。
- [ ] 点击“查询上周的”后，起始时间为上周周一 `00:00:00`，结束时间为上周周日 `23:59:59`。
- [ ] 点击“查询本月的”后，起始时间为本月第一天 `00:00:00`，结束时间为本月最后一天 `23:59:59`。
- [ ] 点击“查询上月的”后，起始时间为上月第一天 `00:00:00`，结束时间为上月最后一天 `23:59:59`。
- [ ] 前端构建和后端相关测试通过，或明确记录无法运行的原因。

## 暂定不做

- 不改变 Excel Sheet 结构和字段。
- 不改变普通数据看板图表的数据聚合逻辑，除非实现导出筛选必须复用相关工具函数。
- 不新增数据库字段或迁移。
