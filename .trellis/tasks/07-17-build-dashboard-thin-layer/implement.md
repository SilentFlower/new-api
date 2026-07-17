# Dashboard 与 Excel Build 薄层化实施计划

## 1. 固化治理前基线

- [ ] 运行 Dashboard 查询、Excel 导出、Token/group 筛选、导出聚合和前端参数构造定向测试。
- [ ] 记录 `controller/usedata.go`、`model/usedata.go`、`model/log.go` 和前端 Dashboard 入口的现有职责边界。

验证：

```bash
go test ./controller ./model -run 'Dashboard|ExportQuotaDataExcel|QuotaDates|ProcessLogsForExport|LogExport|GetAllTokenNames|TokenQuota|StatisticToken' -count=1
cd web/default && bun test src/features/dashboard/lib/flow.test.ts src/features/dashboard/lib/flow-selection.test.ts
```

## 2. 迁移 Controller 职责

- [ ] 新建 `controller/dashboard_filters.go`，原样迁移 `parseDashboardTokenNames`、`parseDashboardGroups` 和 `parseDashboardQueryValues`。
- [ ] 新建 `controller/dashboard_export.go`，原样迁移 `ExportQuotaDataExcel`、Excel 样式常量、样式结构、Sheet helper、Token/cache 展示和 quota 格式化。
- [ ] `controller/usedata.go` 只删除迁出块，保留 Flow 看板、系统统计和 Dashboard 查询入口。
- [ ] 不改路由、鉴权、参数名、错误文案、文件名、响应头或工作簿内容。

## 3. 迁移 Model 职责

- [ ] 新建 `model/dashboard_quota.go`，原样迁移 Dashboard quota 查询、`getQuotaDataFromLogs`、用户/管理员聚合、`singleValueSlice` 和 `usernameFilterCondition`。
- [ ] 新建 `model/dashboard_export.go`，原样迁移 `LogSummaryByKey`、`LogDetailByKeyModel`、`ProcessLogsForExport`、导出日志上限、聚合 key、排序和导出查询 helper。
- [ ] `model/usedata.go` 保留 `QuotaData`、缓存聚合和写入逻辑。
- [ ] `model/log.go` 保留日志实体、通用日志查询、日志清理和前一任务迁出的公共日志薄层入口。
- [ ] 不改 `LOG_DB`/`DB` 选择、ClickHouse 遍历、Anthropic 缓存 Token 口径或聚合结果排序。

## 4. 前端复核

- [ ] 确认 Default 的 `DashboardExportDialog`、`ModelsFilter`、`api.ts` 和 `lib/filters.ts` 已是独立领域文件，参数仍使用重复 key。
- [ ] 确认 Classic 的 `ExportModal`、`SearchModal`、`useDashboardData` 和 `dashboardFilters.js` 已是独立领域文件，参数仍使用重复 key。
- [ ] 若无需前端改动，记录为“已薄层，无代码变更”；若发现页面入口仍含大块导出/筛选实现，只做最小迁移。

## 5. 完整回归与安全检查

- [ ] 运行后端定向测试和 `./controller ./model` 完整测试。
- [ ] 如修改前端，运行对应 `bun test`、`bun run typecheck` 和相关 lint/build；若前端无代码变更，只运行既有 Dashboard lib 定向测试。
- [ ] 运行 `go vet ./controller ./model`、`git diff --check` 和符号唯一性扫描。
- [ ] Check-All 复核 Excel 内容、样式、筛选、Token/cache 统计、取消行为、ClickHouse 遍历和前端参数 round-trip。

最终验证：

```bash
go test ./controller ./model -count=1
go test ./controller ./model -run 'Dashboard|ExportQuotaDataExcel|QuotaDates|ProcessLogsForExport|LogExport|GetAllTokenNames|TokenQuota|StatisticToken|ClickHouse' -count=1
go vet ./controller ./model
git diff --check
```

## 6. Review Gates

- [ ] Gate A：治理前基线通过。
- [ ] Gate B：Excel 函数名、路由、参数、响应头和 workbook 行为不变。
- [ ] Gate C：Dashboard 查询参数、权限、token/group 筛选和历史数据兼容不变。
- [ ] Gate D：前端参数构造和下载处理不变。
- [ ] Gate E：原热点文件只剩薄层入口或稳定基础能力，无无关格式化。
