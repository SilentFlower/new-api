# 报表导出支持令牌筛选和快捷时间 - 技术设计

## 范围

本任务覆盖经典前端数据看板、后端导出接口和日志聚合查询。默认前端暂不纳入实现范围，因为当前导出报表入口存在于经典前端。

## 数据流

### 导出报表

1. 经典前端 `ExportModal` 打开时加载令牌下拉选项。
2. 管理员在导出弹窗选择 0 到多个令牌。
3. `useDashboardData.exportExcel` 将时间范围转换为秒级时间戳，并将多选令牌序列化为 `token_names` 查询参数。
4. `controller.ExportQuotaDataExcel` 解析 `start_timestamp`、`end_timestamp`、`token_names` 和兼容参数 `token_name`。
5. `model.GetLogSummaryByKey`、`model.GetLogDetailByKeyModel`、`model.GetLogsForExport` 使用同一组令牌名称过滤日志。
6. 后端保持 Excel Sheet 结构不变，只改变数据范围。

### 搜索快捷时间

1. `SearchModal` 展示 5 个快捷查询标签。
2. 点击标签时，前端在本地计算起止时间字符串并写回 `inputs.start_timestamp` / `inputs.end_timestamp`。
3. 后续搜索继续复用现有 `loadQuotaData` 流程，将时间字符串转为秒级时间戳请求 `/api/data` 或 `/api/data/self`。

## 接口契约

### `/api/data/export`

已有参数：

- `start_timestamp`: 秒级时间戳，必填。
- `end_timestamp`: 秒级时间戳，必填。
- `token_name`: 旧单值令牌名称参数，可选，继续兼容。

新增参数：

- `token_names`: 多值令牌名称参数，可选。

前端优先发送多值 `token_names`。后端解析规则：

- `token_names` 存在时，忽略空字符串项，并按数组做 `IN` 查询。
- `token_names` 不存在但 `token_name` 存在时，按单值查询。
- 两者都不存在或都为空时，不增加令牌过滤条件。

## 查询设计

模型层方法从单 `tokenName string` 扩展为 `tokenNames []string`。为了避免破坏调用语义，控制器负责把旧参数归一化为切片。

GORM 查询使用 `Where("token_name IN ?", tokenNames)`，这是 GORM 支持的跨 SQLite、MySQL、PostgreSQL 形式。空切片不添加条件，避免生成不可预期的空 `IN` 查询。

## 时间计算设计

快捷标签全部在浏览器本地时间计算：

- 查询 1 天内的：今天 `00:00:00` 到今天 `23:59:59`。
- 查询本周的：本周周一 `00:00:00` 到本周周日 `23:59:59`。
- 查询上周的：上周周一 `00:00:00` 到上周周日 `23:59:59`。
- 查询本月的：本月 1 日 `00:00:00` 到本月最后一日 `23:59:59`。
- 查询上月的：上月 1 日 `00:00:00` 到上月最后一日 `23:59:59`。

周一计算不依赖 `dayjs().startOf('week')`，避免 locale 差异导致周日起算。

## 兼容性

- 不改变 Excel 文件名、Sheet 名、表头和列宽。
- 不改变数据看板搜索接口。
- 不改变已有单令牌搜索下拉行为。
- `/api/data/export?token_name=xxx` 继续可用。
- 后端数据库查询必须保持 SQLite、MySQL、PostgreSQL 兼容。

## 风险与取舍

- 令牌名称本身不是全局唯一；现有管理员下拉用 `name\0username` 区分展示和选择，但导出需求按“令牌名称”过滤。若不同用户存在同名令牌，选择该名称会导出所有同名令牌日志，这是当前后端日志查询基于 `token_name` 的既有约束。
- 导出弹窗不默认带入搜索条件令牌，符合用户确认的独立筛选要求。

## 回滚

如需回滚，可移除前端导出多选参数、后端 `token_names` 解析和模型层 `IN` 过滤，恢复导出接口只按时间范围导出。
