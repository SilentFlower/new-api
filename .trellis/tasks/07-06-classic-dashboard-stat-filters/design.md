# 旧版数据看板统计筛选升级设计

## 范围边界

本任务只触达旧版数据看板搜索/导出链路：

- 前端：`web/classic/src/hooks/dashboard/useDashboardData.js`、`SearchModal.jsx`、`ExportModal.jsx`。
- 后端 Controller：`controller/usedata.go`、`controller/log.go`。
- 后端 Model：`model/usedata.go`、`model/usedata_rankings.go`、`model/log.go`、`model/token.go`。

不改默认新版 UI，不改日志页搜索，不改数据库结构。

## 查询参数契约

新增多值参数使用重复 key：

```text
groups=default&groups=vip
token_names=key-a&token_names=key-b
```

兼容形式：

- `groups[]`
- `token_names[]`
- 旧单值 `group`
- 旧单值 `token_name`

归一化规则：

1. 新多值参数存在时优先使用新参数。
2. 新参数不存在时读取旧单值参数。
3. trim、去空、去重。
4. 归一化为空切片表示不过滤该维度。

## 后端数据流

### 令牌选项

`/api/data/token-names` 返回管理员令牌选项时补充分组：

- `name`
- `username`
- `group`

前端使用 `name + username + group` 组成唯一 value，既能区分同名令牌，也能按分组联动过滤。

### 数据看板主图

`/api/data/` 管理员查询新增 `groups` 与 `token_names`：

- 无分组、无多令牌、无单令牌时，沿用 `quota_data` 聚合表。
- 有分组或令牌筛选时，从 `logs` 表按小时、模型、令牌、用户聚合，确保分组过滤基于真实请求日志。

`/api/data/self` 支持 `token_names`，普通用户不暴露分组筛选。

### 管理员用户排行/趋势

`/api/data/users` 新增 `groups` 与 `token_names`，按条件从 `logs` 表聚合用户维度数据；无筛选时保持现有 `quota_data` 路径。

### 统计卡片

`/api/log/stat` 和 `/api/log/self/stat` 新增多值筛选：

- `groups`
- `token_names`

`model.SumUsedQuota` 保持旧签名兼容的同时增加多值过滤能力，或新增内部 helper，避免重复拼接条件。

### 导出报表

`/api/data/export` 新增 `groups` 参数，传入：

- `GetLogSummaryByKey`
- `GetLogDetailByKeyModel`
- `GetLogsForExport`

三个 Sheet 使用同一过滤条件。

## 数据库兼容

- `logs.group` 过滤必须使用 `logGroupCol` 或等价跨库列名变量。
- 聚合查询使用 GORM `IN ?`，避免数据库专用数组语法。
- 小时桶仍使用现有 `(created_at - created_at % 3600)` 表达式，保持当前数据库兼容行为。

## 前端状态设计

`inputs` 新增：

- `groups: []`
- `token_names: []`

保留旧 `username` 用于管理员手动过滤用户；令牌多选不再通过选择单个令牌强行覆盖 `username`。

令牌选项拆分为：

- `allTokenOptions`：接口返回全集。
- `tokenOptions`：根据已选分组过滤后的展示列表。

导出弹窗接收 `groupOptions`、`tokenOptions`，本地维护导出分组和令牌选择；导出令牌列表随导出分组过滤。

## 风险与取舍

- 管理员选择同名令牌且不选用户名时，后端 `token_names` 只按名称过滤，会包含所有同名令牌。为避免误导，前端标签保留用户名和分组；本任务不新增 token_id 维度，因为日志历史已有 `token_name` 和 `token_id` 混用风险，扩大为 token_id 过滤需要另行评估历史数据兼容。
- 分组过滤需要从 `logs` 表聚合，时间范围过大时可能比 `quota_data` 慢；只有使用分组或令牌筛选时走日志聚合，未筛选路径仍沿用聚合表。
