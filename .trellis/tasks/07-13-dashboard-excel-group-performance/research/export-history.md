# 数据看板 Excel 导出历史契约研究

## 原始导出契约

来源：`.trellis/tasks/archive/2026-03/03-28-dashboard-export-excel/prd.md`。

- 管理员通过 `GET /api/data/export` 下载 Excel。
- 文件包含“汇总统计”“模型明细”“请求日志”三个 Sheet。
- Sheet 1 按 API Key 汇总，Sheet 2 按 API Key + 模型展示，Sheet 3 按时间升序展示消费日志。
- 文件名包含导出时间范围，普通用户导出、CSV、异步导出不在原始范围内。
- 三个 Sheet 均来自 `logs` 表，避免 `quota_data` 历史字段不完整的问题。

## 分组与 API Key 筛选契约

来源：`.trellis/tasks/archive/2026-07/07-06-classic-dashboard-stat-filters/design.md`。

- `/api/data/export` 接收 `groups`、`token_names` 重复 key，并兼容括号数组和旧单值参数。
- 三个 Sheet 必须使用同一组时间、分组和 API Key 筛选条件。
- `logs.group` 是保留字，必须使用跨数据库列名处理。
- 同名 API Key 可能属于不同用户或分组，导出聚合不能因名称相同而错误混合。

## Anthropic 缓存 Token 契约

来源：`.trellis/tasks/archive/2026-07/07-08-anthropic-cache-token-statistics/prd.md`。

- Sheet 1、Sheet 2 的请求 Token 数包含普通输入、Anthropic 缓存读取、缓存写入和输出。
- Sheet 3 继续分别展示输入 Token、缓存读写说明和输出 Token，避免双算误导。
- 为保持历史数据统计准确性，导出聚合不能退回简单的数据库 `sum(prompt_tokens + completion_tokens)`。

## 本任务继承的兼容要求

- 保持三个 Sheet、文件名、管理员权限和前端下载流程。
- 请求日志 Sheet 保持现有最多 500000 行行为，不新增拒绝、提示或时间范围限制。
- Sheet 1、Sheet 2 仍统计完整匹配范围。
