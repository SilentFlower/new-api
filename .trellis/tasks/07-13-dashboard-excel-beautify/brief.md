# Brief — 数据看板 Excel 导出完整美化

## Goal

- 在现有分组列 + 单次遍历 + StreamWriter 导出上，完成三 Sheet 完整美化（青绿专业色板、数字格式、冻结/筛选、元信息、Sheet2 分段配色、Sheet1 `SUBTOTAL` 合计），不改筛选语义、聚合口径与性能主路径。

## Scope

- 改造 `controller/usedata.go` 导出展示层：样式体系、版式、冻结窗格、Sheet1/Sheet3 数据区筛选、Sheet1 筛选感知合计、Sheet2 分段美化、顶部元信息。
- 更新 `controller/usedata_test.go` 适配新行布局，并断言 `SUBTOTAL`、分组与缓存 Token 不回退。
- 若列标题/版式变化，同步 `.trellis/spec/backend/api-contracts.md` 导出场景说明。
- 固定色板：表头 `#0F766E`、分组 `#E6F4F1`、小计 `#F0FDFA`、合计 `#ECFDF5`、边框 `#CCE3DE`。
- Sheet2 保留分段表；空分组原样空白；无斑马纹。

## Non-Goals

- 不改 Model 聚合/遍历语义、路由权限、前端下载交互、数据库结构。
- 不做 Sheet2 扁平表或 Excel 内分组下拉筛选。
- 不做斑马纹、条件格式、图表/透视、CSV/异步导出。
- 不改变 500000 明细上限与统计口径。

## Key Context

- 入口：`GET /api/data/export` → `ExportQuotaDataExcel`（`controller/usedata.go`）。
- 数据仍来自 `model.ProcessLogsForExport`；本任务原则上只改 Controller 展示。
- 必须继续 StreamWriter；`SetColWidth`/`SetPanes` 在首行前；`AddTable` 在写行后、Flush 前；合计行不得进入筛选范围。
- Sheet1 合计用 `excelize.Cell{Formula: "SUBTOTAL(109,...)" }`；测试断言公式字符串。
- 建议行布局：R1 标题 / R2 元信息 / R3 空白 / R4 起业务内容。
- 契约与性能约束见 `.trellis/spec/backend/api-contracts.md`「数据看板 Excel 分组导出与大数据量生成」。
- 预览 xlsx 仅评审用，不提交。

## Acceptance

- AC1：三 Sheet 统一青绿表头样式，文件可打开。
- AC2：Sheet1/Sheet3 冻结与数据区筛选可用，合计不在筛选范围。
- AC3：Sheet2 分段表，分组/小计可区分，无整表分组下拉。
- AC4：次数/Token 千分位、额度 USD、耗时小数格式生效。
- AC5：Sheet1 合计为 `SUBTOTAL`，筛选后随可见行变化。
- AC6：顶部元信息含时间范围与筛选摘要。
- AC7：无斑马纹；空分组原样空白。
- AC8：一次遍历 + StreamWriter；上限与聚合口径不回退。
- AC9：相关 Go 测试通过。

## Next Step

- 用户确认 planning artifacts 与本 brief 后：`task.py start`，再 `trellis-route(implement)`，按 `implement.md` 从样式辅助与 Sheet3/1/2 改造开始。
