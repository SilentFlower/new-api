# 优化 ai-fund 日志页统计与图表联动设计

## 范围

本任务修改公共 API Key 日志查看器 `/log` 的前后端数据流：

- 前端：`web/default/src/features/token-logs/*`
- 后端 Controller：`controller/log.go`
- 后端 Model：`model/log.go`、必要时复用 `model/token_statistics.go`
- 路由保持现有路径，不新增公开管理能力。

不改 API Key 认证模型，不新增数据库字段，不引入新 UI 库。

## API 契约

### 查询参数

`GET /api/log/token/stat` 和 `GET /api/log/token/data` 扩展为接受和分页日志一致的过滤参数：

```text
type=<number>
model_name=<string>
request_id=<string>
start_timestamp=<unix seconds>
end_timestamp=<unix seconds>
```

兼容旧调用方：只传时间参数或不传参数时继续返回原有语义。

### 统计响应语义

`TokenLogStat` 保持现有字段：

```go
type TokenLogStat struct {
	Count            int `json:"count"`
	Quota            int `json:"quota"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	Rpm              int `json:"rpm"`
	Tpm              int `json:"tpm"`
}
```

字段口径：

- `count`：按当前 `type/model_name/request_id/time` 过滤后的日志数量。
- `rpm`：按当前 `type/model_name/request_id` 过滤后最近 60 秒日志数量。
- `quota/prompt_tokens/completion_tokens/total_tokens/tpm`：只统计消费日志。
- 当 `type` 是非消费类型时，用量字段为 0，`count/rpm` 仍按该类型联动。
- 当 `type=0` 全部时，`count/rpm` 覆盖全部日志类型，用量字段只覆盖消费日志子集。

### 图表响应语义

`GET /api/log/token/data` 返回：

- `model_stats`：按当前过滤条件统计模型调用分布；如果 `type=0`，按全部日志类型统计。
- `quota_data`：只返回当前过滤条件下的消费日志趋势数据；如果 `type` 是非消费类型，返回空数组。

`quota_data` 可继续复用 `QuotaData` 结构，但前端必须按 `created_at` 再聚合总 `quota`，不能把 `model_name` 当作趋势横轴维度。

## 后端设计

### 过滤参数结构

新增内部参数结构，避免 stat/data/table 三套解析分叉：

```go
type TokenLogFilterParams struct {
	TokenID        int
	LogType        int
	StartTimestamp int64
	EndTimestamp   int64
	ModelName      string
	RequestID      string
}
```

Controller 负责从 Gin query 解析该结构。Model 层只接收结构体。

### 过滤复用

新增或抽取 token 日志过滤 helper：

- 基础过滤：`token_id`
- 类型过滤：`type`，支持 `LogTypeUnknown` 表示全部
- 时间过滤：`created_at >= / <=`
- 模型过滤：复用 `applyExplicitLogTextFilter()` 或等价安全 LIKE 逻辑，兼容 ClickHouse。
- 请求 ID：精确匹配 `request_id = ?`

不要继续在 `GetTokenLogStat()`、`GetTokenModelStats()`、`GetTokenQuotaData()` 各自手写不一致过滤。

### 性能策略

1. API Key 验证改用 `GET /api/usage/token/`，避免无时间范围统计扫描，防止用户进入 `/log` 时直接触发 MySQL 全历史日志聚合导致 CPU/内存飙升。
2. `GetTokenLogStat()` 拆分为：
   - `count/rpm` 查询：按当前类型过滤，可使用 SQL `count(*)`。
   - 用量查询：仅在 `type=0` 或 `type=LogTypeConsume` 时执行消费日志聚合。
3. 用量查询仍需保留 Anthropic cache token 口径：
   - `total_tokens/tpm` 继续使用 `statisticTokenUsedForLog()` 或共享聚合逻辑。
   - 尽量避免对同一过滤范围重复调用 `sumStatisticTokenUsedFromLogQuery()`。
4. Go 侧日志扫描应只选择计算 token 必要的字段，避免沿用宽字段批量读取造成额外内存压力。
5. `model_stats` 使用 SQL 聚合即可，不解析 `Other`。
6. `quota_data` 使用现有日志聚合路径，但需要接收完整筛选参数；当非消费类型时直接返回空，避免无意义扫描。
7. `Log` 模型增加复合索引：
   - `idx_logs_token_created_at(token_id, created_at)`：覆盖默认全部类型下的请求数、RPM 和时间范围查询。
   - `idx_logs_token_type_created_at(token_id, type, created_at)`：覆盖消费用量、TPM、模型分布和趋势查询。
   通过 GORM tag 随 `AutoMigrate(&Log{})` 创建，主库日志表和独立 `LOG_SQL_DSN` 日志库均走同一迁移路径；ClickHouse 日志库保持现有 MergeTree 表结构，不做在线 ALTER。

## 前端设计

### 轻量验证

新增公共 API Key usage 封装，例如：

```ts
getTokenUsage(client): Promise<TokenUsageResponse>
```

`AuthPanel` 提交时调用该接口验证 Key。成功后进入 workspace；统计卡片随后按默认当天时间范围加载。

### 查询参数统一

将当前 `buildTokenLogTimeParams()` 替换或弱化为“统计/图表也使用完整过滤参数”：

- 表格：保留分页参数。
- 统计：使用 `type/model_name/request_id/start/end`。
- 图表：使用 `type/model_name/request_id/start/end`。

React Query key 也必须包含完整 `appliedFilters`，避免筛选后统计/图表复用旧缓存。

### 模型分布点击筛选

`TokenLogCharts` 接收回调：

```ts
onSelectModel(modelName: string): void
```

点击模型项时：

- 忽略空模型名。
- 更新 `draftFilters.model` 和 `appliedFilters.model`。
- 保持当前时间范围和类型。
- 表格分页重置为第一页。

分页状态当前在 `TokenLogsTable` 内部，建议把 `pageIndex` 重置能力上提到 workspace，或给表格传入 `resetSignal/modelFilterVersion`。优先选择小范围改动：在 `TokenLogsTable` 内监听 `appliedFilters` 变化后把 `pageIndex` 置 0，避免引入复杂状态提升。

### 消耗趋势

前端把 `quota_data` 先按 `created_at` 聚合：

```ts
Map<created_at, { quota: number; token_used: number; count: number }>
```

展示为总消耗趋势：

- 横轴：格式化后的时间 bucket。
- 纵轴：`quota`，展示时用 `formatLogQuota()`。
- tooltip：完整时间、消耗额度、请求数、token 用量。
- 不再直接 `slice(-12)` 原始行；如果需要控制点数，应按聚合后的时间点裁剪或抽样。
- 图表布局需要显式预留坐标轴空间：柱状绘图区、x 轴标签、y 轴刻度和 tooltip 都必须在卡片内容区域内渲染，避免标签超出卡片或被裁切。
- 当时间 bucket 较多时，横轴标签应抽样显示或做密度控制，不能把每个 bucket 的完整标签都强行渲染到同一行。

继续使用当前轻量自绘卡片或现有项目图表组件均可；不得引入新图表库。

### 非消费类型空态

当 `type` 是非消费类型：

- 模型调用分布仍展示该类型下的模型分布。
- 消耗趋势显示空态文案：`用量统计仅适用于消费日志`。
- `Usage/Tokens/TPM` 显示 0；`Requests/RPM` 仍显示当前类型结果。

## 测试与验证

- 后端 Model/Controller 测试覆盖：
  - stat 接收 `model_name/request_id/type/time` 并过滤。
  - `type=0` 时 count 包含全部类型，用量只包含消费日志。
  - 非消费 type 时用量为 0，count/rpm 可有值。
  - Anthropic cache token 口径保持不变。
  - model_name LIKE 过滤安全转义。
- 前端测试或审查点：
  - `getTokenLogStat/getTokenLogChartData` 发送完整筛选参数。
  - 认证阶段调用轻量 usage 接口，不调用 stat。
  - 点击模型分布后模型输入框更新并触发查询。
  - 消耗趋势按时间聚合总 quota。

## 风险

- ClickHouse LIKE 语义和普通数据库不同，必须复用现有 helper。
- `request_id` 筛选可能极窄，图表为空是正常结果。
- `TokenUsage` 接口响应字段使用 `code` 而不是 `success`，前端需要单独类型处理，不能复用 `TokenLogApiResponse`。
- 如果日志量极大且统计仍慢，后续可再考虑基于 `quota_data` 或专门汇总表优化，但这超出本任务默认范围。
- 线上创建复合索引会产生 IO 和写入放大，生产执行应放在低峰期，并先确认 MySQL online DDL 行为；代码迁移本身不直接在本次远程排查中执行。
- 自绘图表容易在小尺寸卡片内发生坐标轴越界；实现时需要用固定绘图区、内边距和标签密度控制做实际截图检查。
