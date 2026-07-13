# 数据看板 Excel 导出完整美化设计

## 范围边界

只改管理员导出展示层：

- `controller/usedata.go`：样式、版式、公式合计、冻结/筛选、元信息
- `controller/usedata_test.go`：适配新行布局与关键断言
- 如列标题/版式描述变化：更新 `.trellis/spec/backend/api-contracts.md` 导出场景说明

不改：

- `model.ProcessLogsForExport` 聚合语义、筛选语义、遍历策略
- 路由、权限、文件名规则、前端下载交互
- 数据库结构

## 现状

```text
ExportQuotaDataExcel
  -> 创建三 Sheet + StreamWriter
  -> 仅 boldStyle
  -> Sheet3 从第 1 行写表头与明细
  -> Sheet1/Sheet2 写完聚合结果
  -> Flush + HTTP 写出
```

问题：样式弱、无冻结/筛选、无元信息、Sheet1 无筛选感知合计。

## 目标版式

### 通用

每个 Sheet 顶部：

| 行 | 内容 |
|----|------|
| 1 | 标题（合并单元格） |
| 2 | 元信息：时间范围 + 筛选摘要（合并单元格） |
| 3 | 空行或直接进入内容（实现时固定一种，优先第 4 行起为数据表头，保持简单） |

建议固定：

- 第 1 行标题
- 第 2 行元信息
- 第 3 行空白
- 第 4 行起为业务内容

### Sheet1 汇总统计

```text
R1 标题
R2 元信息
R3 空白
R4 表头：分组 | API Key 名称 | 请求次数 | 请求 Token 数 | 请求额度 (USD)
R5..Rn 数据行
R(n+1) 合计行（SUBTOTAL）
```

- 冻结：`YSplit` 到表头行（第 4 行），使滚动时表头保留
- 筛选/Table 范围：`A4:E{n}`（**不含**合计行）
- 合计：
  - A=`合计`
  - C=`SUBTOTAL(109,C5:C{n})`
  - D=`SUBTOTAL(109,D5:D{n})`
  - E=`SUBTOTAL(109,E5:E{n})`
- 无数据时：仅表头；合计行可省略或写 0/空范围安全公式（实现选更简单且测试稳定的一种，推荐：无数据不写合计行，有数据才写）

### Sheet2 模型明细（分段表保留）

```text
R1 标题
R2 元信息
R3 空白
循环 block:
  分组标题行（合并 + 分组底色）
  段内表头（统一表头样式）
  模型数据行
  小计行（小计底色 + 静态数字）
  空行分隔
```

- 不做 AutoFilter / Table
- 冻结可仅冻结顶部元信息区（如 `YSplit=3`），避免与分段结构冲突

### Sheet3 请求日志

```text
R1 标题
R2 元信息
R3 空白
R4 表头
R5.. 明细（最多 500000）
```

- 冻结表头
- 对数据区开启筛选/Table
- 输入 Tokens 含缓存说明时用文本；输出 Tokens/额度/耗时尽量数值+格式

## 样式体系

在 `ExportQuotaDataExcel` 内预创建 StyleID（名称可本地化变量）：

| Style | 用途 |
|-------|------|
| titleStyle | 第 1 行标题 |
| metaStyle | 第 2 行元信息 |
| headerStyle | 表头：青绿底、白字、居中、边框 |
| textStyle | 文本左对齐 + 边框 |
| numberStyle | 整数千分位右对齐 + 边框 |
| moneyStyle | 货币右对齐 + 边框 |
| durationStyle | 耗时小数右对齐 + 边框 |
| centerStyle | 是否流式居中 + 边框 |
| groupTitleStyle | Sheet2 分组标题 |
| subtotalStyle* | Sheet2 小计（文本/数字/货币可分多个） |
| totalStyle* | Sheet1 合计（文本/数字/货币可分多个） |

色板（方案 2 青绿专业）：

- header `#0F766E` / fg `#FFFFFF`
- group `#E6F4F1`
- subtotal `#F0FDFA`
- total `#ECFDF5`
- border `#CCE3DE`
- meta text `#64748B`

## StreamWriter 约束与调用顺序

每个 Sheet：

1. `NewStreamWriter`
2. `SetColWidth`
3. `SetPanes`（如需要）
4. `SetRow` 顺序写行（行号严格递增）
5. 数据写完后：`AddTable`（仅 Sheet1/Sheet3，且范围不含合计）
6. `Flush`
7. 全部 Flush 成功后再写 HTTP 头与 `f.Write`

公式单元格：

```go
excelize.Cell{StyleID: moneyStyle, Formula: fmt.Sprintf("SUBTOTAL(109,E%d:E%d)", first, last)}
```

合并单元格：标题/元信息/分组标题使用 `MergeCell`；需确认与 StreamWriter 的兼容顺序（写相关行前/后按 excelize 文档与最小试验确定；若 MergeCell 与 stream 冲突，退化为首格写文本、不合并，但仍保留样式）。

## 元信息文案

```text
时间范围：YYYY-MM-DD HH:mm:ss ~ YYYY-MM-DD HH:mm:ss | 分组：a,b / 全部 | API Key：x,y / 全部
```

由 `startTimestamp/endTimestamp/groups/tokenNames` 在 Controller 本地格式化，不新增 API 参数。

## 契约影响

现有契约要求：

- 列语义与聚合维度不变
- 空分组保持空字符串
- 一次遍历 + StreamWriter + 500000 明细上限

本任务会导致：

- 表头不再位于第 1 行
- Sheet1 增加合计行与公式
- 部分列标题可能带 `(USD)` 等展示增强

因此实现时必须：

1. 更新 Controller 测试的行索引断言
2. 更新 `api-contracts.md` 中导出场景的版式/列标题描述，明确「数据表头不一定在第 1 行」与「Sheet1 含 SUBTOTAL 合计行」

## 风险与回滚

| 风险 | 处理 |
|------|------|
| `AddTable` 与合计行冲突 | 合计放筛选范围外；无数据不建 Table |
| MergeCell 与 StreamWriter 不兼容 | 降级为不合并，保留样式 |
| 公式在 WPS/Excel 打开前不计算 | 可接受；测试断言公式字符串 |
| 行布局变化导致旧测试失败 | 同步改测试，不以保留旧行号为优先 |
| 样式对象过多 | 预创建有限 StyleID，禁止每行 NewStyle |

回滚点：仅 Controller 展示层，可恢复为当前 bold-only 导出。

## 非目标

- Sheet2 扁平化
- 斑马纹
- Model 层改动
- 前端改动
