# 数据看板 Excel 分组列与导出性能优化实施计划

## 实施步骤

1. 调整导出聚合契约
   - 为 `LogSummaryByKey`、`LogDetailByKeyModel` 增加 `Group`。
   - 将导出聚合键和排序规则补充为分组优先。
   - 保持空分组原值，不做历史数据推断。

2. 增加单次日志遍历能力
   - 新增带 `context.Context` 的导出处理方法。
   - 复用时间、用户名、API Key、分组过滤规则。
   - 使用 `Rows()` 和最小字段集按 `created_at asc` 遍历。
   - 一次计算汇总、模型明细和请求日志回调数据。
   - 仅回调前 500000 条明细，超过后继续完成全量聚合。
   - 将查询、扫描、Context 取消和回调错误原样返回。

3. 将 Excel 生成切换为流式写入
   - 为三个 Sheet 创建 StreamWriter。
   - 在写行前设置列宽和加粗样式。
   - Sheet 3 在 Model 遍历回调中直接写行，并增加“分组”列。
   - 遍历完成后写 Sheet 1 和 Sheet 2，增加分组列及分组标题。
   - Flush 所有 StreamWriter 后再设置下载响应头并输出文件。

4. 清理旧导出调用路径
   - Controller 不再依次调用三条日志查询。
   - 保留仍有独立调用价值的 Model API；删除仅为旧 Controller 链路服务且已无调用的冗余代码前，先用 `rg` 确认引用。
   - 不修改其他数据看板统计路径。

5. 增加回归测试
   - Model：同名 API Key 在不同分组下分别聚合。
   - Model：分组/API Key 筛选同时作用于汇总、模型明细和明细回调。
   - Model：取消 Context 能终止处理并返回错误。
   - Controller：导出的三个 Sheet 包含预期分组表头和值，文件可被 excelize 重新打开。
   - Controller：原有缓存 Token 展示和列顺序不回退。

## 验证命令

```bash
gofmt -w controller/usedata.go controller/usedata_test.go model/log.go model/token_statistics.go model/dashboard_filters_test.go
go test ./model -run 'Test.*LogExport|Test.*DashboardFilter'
go test ./controller -run 'Test.*ExportQuotaDataExcel|TestParseDashboard'
go test ./model ./controller
go test ./...
```

若实际新增独立测试文件，`gofmt` 命令同步包含该文件。

## 风险检查

- StreamWriter 的列宽必须先于首行写入，且同一 Sheet 不得混用普通写入模式。
- Sheet 2 分组区块的行号必须严格递增，空数据时仍需生成有效空 Sheet。
- 500000 行之后必须停止 Sheet 3 写入但继续聚合，不能提前终止数据库遍历。
- `group` 是保留字，查询字段和过滤条件必须经过 GORM 或现有跨库列名变量处理。
- ClickHouse `id` 不适合作为 GORM 批次游标，不能把实现退回 `FindInBatches`。
- 必须在写出 Excel 响应头前完成查询和 StreamWriter Flush，避免错误响应被二进制响应头污染。

## 回滚点

- Model 单次遍历方法可独立回滚，不涉及数据库数据。
- Controller 可恢复原三个查询和普通 excelize 写入模式。
- 分组列只影响导出文件结构，不影响 API JSON 契约和数据库记录。
