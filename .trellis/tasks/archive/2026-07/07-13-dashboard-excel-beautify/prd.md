# 数据看板 Excel 导出完整美化

## Goal

在已完成的「分组列 + 单次遍历 + StreamWriter」导出基础上，提升三 Sheet 的可读性与版式：统一表头样式、数字/货币格式、冻结窗格、筛选、Sheet2 分组/小计视觉层级、Sheet1 筛选感知合计，以及顶部导出元信息；不改变筛选语义、聚合口径、性能主路径与 500000 行上限。

## Background

- 导出入口：`GET /api/data/export`，实现于 `controller/usedata.go` 的 `ExportQuotaDataExcel`。
- 上一任务已落地：分组维度、一次日志遍历、`excelize` StreamWriter、三 Sheet 契约（见 `.trellis/spec/backend/api-contracts.md`「数据看板 Excel 分组导出与大数据量生成」）。
- 当前样式仅有加粗，缺少表头底色、数字格式、冻结/筛选、合计与分组层级配色。
- 视觉预览文件（非正式导出，勿提交）：`数据报表_完整美化预览_20260701_20260713.xlsx`、`数据报表_配色三选一预览.xlsx`。

## Requirements

### R1：统一视觉样式（无斑马纹）

- 三 Sheet 使用统一「青绿专业」色板：
  - 表头：`#0F766E` 底 + 白字加粗居中 + 细边框（`#CCE3DE`）
  - 分组标题：`#E6F4F1`
  - 小计：`#F0FDFA` + 加粗
  - 合计：`#ECFDF5` + 加粗
  - 标题/元信息文字：`#0F766E` / `#64748B`
- 文本列左对齐，数值列右对齐，「是否流式」居中。
- 不实现数据行斑马纹。

### R2：数字与列表现

- 请求次数、纯数字 Token 列：千分位整数格式（`#,##0`）。
- 额度列：USD 货币格式（`$#,##0.00` 或等价），数值仍来自现有 `formatQuotaValue` 美元口径。
- 请求日志耗时：保留合适小数格式（如 `0.00`）。
- 输入 Tokens 含缓存说明时保持文本；无缓存说明时尽量写数值类型。
- 列标题可微调为更清晰文案（如额度旁标注 USD），但列语义与顺序不得改变。
- 列宽按新表头与格式微调。

### R3：冻结、筛选与元信息

- 每个 Sheet 顶部增加元信息区（建议 2 行）：
  - 标题行：Sheet 名称语义（如「数据看板导出 · 汇总统计」）
  - 摘要行：时间范围；若存在分组/API Key 筛选则展示摘要，否则展示「全部」
- Sheet1 / Sheet3：
  - 数据表头行使用统一表头样式
  - 冻结窗格冻结在数据表头下一行
  - 对**数据区**开启 AutoFilter 或 StreamWriter `AddTable`（二选一，优先实现简单且与合计行兼容的方案）
  - 筛选范围**不得包含**合计行；合计行位于筛选范围下方
- Sheet2：
  - **保留分段表**（分组标题 / 段内表头 / 模型数据 / 小计 / 段间空行）
  - 不做整表分组下拉筛选，不改为扁平表
  - 按分组查看继续依赖导出接口的 `groups` 筛选参数

### R4：合计与小计

- Sheet1 底部保留合计行：
  - 标签可用「合计」（打开未筛选时等于全表；筛选后随可见行变化）
  - 请求次数、Token、额度列写入 `SUBTOTAL` 公式（筛选感知，优先 `109` 语义：忽略隐藏行）
  - 不得再写静态全表求和数字作为合计展示值
- Sheet2 每段保留静态「小计」（加粗 + 小计底色）；不做整表动态合计。
- 空数据时：Sheet1 仍输出表头；合计行在无数据行时的公式/展示以实现时不报错、文件可打开为准（可写 0 或指向空范围的合法公式）。

### R5：空分组与数据保真

- 空分组继续原样输出空字符串，不改写为「（空）」或 `default`。
- 不改变聚合键、统计口径、一次遍历、500000 明细上限、路由/权限/文件名/前端下载交互。

### R6：兼容、错误与测试

- 查询/流式写入/Flush/响应输出失败时不得返回伪成功文件。
- 必须继续 StreamWriter；禁止回退普通单元格全量写入。
- 更新 Controller 导出测试以适配新行布局（元信息行、合计行、可能的列标题微调），并断言：
  - 三 Sheet 存在且可打开
  - 分组与缓存 Token 展示不回退
  - Sheet1 合计单元格为 `SUBTOTAL` 公式
  - 关键筛选语义与聚合结果不回退
- 若列标题或行布局变化影响 `.trellis/spec/backend/api-contracts.md` 中导出契约描述，实现时同步更新该契约中的列/版式说明，并明确性能与聚合口径不变。

## Acceptance Criteria

- [ ] AC1：三 Sheet 表头使用青绿专业色板（底色/白字/加粗/边框），文件可被 Excel/WPS 正常打开。
- [ ] AC2：Sheet1/Sheet3 冻结数据表头可用，数据区筛选可用；合计行不在筛选范围内。
- [ ] AC3：Sheet2 保持分段表，分组标题与小计配色可区分，无整表分组下拉。
- [ ] AC4：次数/Token 千分位、额度 USD 货币格式、耗时小数格式生效；额度仍为美元口径。
- [ ] AC5：Sheet1 合计行为 `SUBTOTAL` 公式；未筛选时等于全表，筛选后仅统计可见行。
- [ ] AC6：顶部元信息包含时间范围及筛选摘要（无筛选时为全部）。
- [ ] AC7：无数据行斑马纹；空分组原样空白。
- [ ] AC8：导出仍走一次遍历 + StreamWriter；500000 上限与聚合口径不回退。
- [ ] AC9：相关 Go 测试通过，覆盖新布局、公式合计、分组与缓存 Token 回归。

## Out of Scope

- 普通用户导出权限、CSV、异步导出/对象存储。
- 修改数据看板前端筛选器或下载交互。
- 数据库结构/索引迁移、Model 聚合语义改造。
- 数据行斑马纹、条件格式高亮、图表/透视表。
- Sheet2 扁平表化或 Excel 内分组下拉筛选。
- 改变聚合键、统计口径或导出上限语义。

## Technical Notes

- 主改文件：`controller/usedata.go`、`controller/usedata_test.go`；必要时同步 `.trellis/spec/backend/api-contracts.md` 导出场景的版式描述。
- excelize v2.9.0 StreamWriter 能力：`SetRow`/`RowOpts`/`excelize.Cell{StyleID,Formula,Value}`、`SetColWidth`、`SetPanes`、`MergeCell`、`AddTable`（写行后、Flush 前；每 Sheet 仅一个 Table）。
- 建议样式在文件级 `NewStyle` 预创建，写行时复用 StyleID。
- `SetPanes` / `SetColWidth` 必须在首行 `SetRow` 之前。
- Sheet1 合计推荐：`excelize.Cell{Formula: "SUBTOTAL(109,C{first}:C{last})", StyleID: totalStyle}`。
- 预览 xlsx 仅供评审，不纳入版本库。
