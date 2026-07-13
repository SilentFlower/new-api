# 数据看板 Excel 导出完整美化实施计划

## 实施步骤

1. 收敛样式与版式辅助逻辑（仅 `controller/usedata.go` 内或同文件私有函数）
   - 定义青绿专业色板常量
   - 预创建 title/meta/header/text/number/money/duration/center/group/subtotal/total 等 StyleID
   - 实现元信息文案格式化（时间范围 + groups/tokenNames 摘要）
   - 实现安全的 StreamWriter 写行封装（带 StyleID / Formula）

2. 改造 Sheet3（请求日志）
   - 写标题、元信息、空白行、表头
   - `SetPanes` 冻结表头
   - 回调写明细时应用对齐与数字格式
   - 有数据时对数据区 `AddTable` 或 AutoFilter
   - Flush

3. 改造 Sheet1（汇总统计）
   - 写标题、元信息、空白行、表头
   - 写数据行（数值类型 + 格式）
   - 有数据时写 `SUBTOTAL` 合计行（范围不含合计自身）
   - 数据区筛选/Table（不含合计）
   - 冻结表头
   - Flush

4. 改造 Sheet2（模型明细分段表）
   - 写标题、元信息、空白行
   - 保留分组标题 + 段内表头 + 数据 + 静态小计 + 空行
   - 应用分组/表头/小计样式与数字格式
   - 不做整表筛选
   - Flush

5. 更新测试与契约
   - 调整 `controller/usedata_test.go` 行索引：元信息后的表头/数据/合计
   - 断言 Sheet1 合计公式包含 `SUBTOTAL`
   - 断言分组、缓存 Token、筛选结果不回退
   - 如列标题变化，同步 `.trellis/spec/backend/api-contracts.md` 导出场景描述

6. 清理
   - 不提交预览 xlsx
   - `gofmt` 相关 Go 文件

## 验证命令

```bash
gofmt -w controller/usedata.go controller/usedata_test.go
go test ./controller -run 'Test.*ExportQuotaDataExcel|TestParseDashboard'
go test ./controller
go test ./model -run 'Test.*LogExport|Test.*DashboardFilter'
go test ./model ./controller
```

## 风险检查

- `SetColWidth`/`SetPanes` 必须在该 Sheet 首次 `SetRow` 前
- `AddTable` 必须在写完目标行后、`Flush` 前；header 唯一字符串
- Sheet1 合计范围在无数据/单行时不能生成非法引用
- 空分组保持 `""`
- 不得把 Model 遍历改回三次查询或普通 excelize 全量写单元格
- 失败路径仍在写响应头前返回 API 错误

## 回滚点

- 仅回退 `controller/usedata.go` 与对应测试/契约文案即可恢复旧版式
