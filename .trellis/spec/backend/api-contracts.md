# API 契约规范

> 记录管理 API 的可执行契约，尤其是跨前端、Controller、Model 的查询参数和边界行为。

## 场景：管理 API 多值查询参数

### 1. Scope / Trigger

- Trigger: 管理 API 新增或修改查询参数，且参数会从前端传到 Controller，再影响 Model 层数据库查询。
- 适用范围: `/api/*` 管理接口中的 GET 查询参数，特别是多选筛选条件。

### 2. Signatures

- Controller 签名保持 Gin 独立函数模式：

```go
func SomeAdminEndpoint(c *gin.Context)
```

- 多值查询参数读取使用 Gin 的数组读取能力：

```go
values, ok := c.GetQueryArray("field_names")
```

- 需要兼容前端或历史调用的数组格式时，可额外读取括号形式：

```go
bracketValues, bracketOk := c.GetQueryArray("field_names[]")
```

### 3. Contracts

- 前端发送多值参数时，优先使用重复 key 的查询字符串：

```text
field_names=a&field_names=b
```

- 后端参数归一化规则：
  - 支持重复 key：`field_names=a&field_names=b`。
  - 如历史或第三方调用已存在，兼容单值旧参数：`field_name=a`。
  - 如需要兼容 axios 默认数组风格，可同时支持：`field_names[]=a&field_names[]=b`。
  - 归一化时必须 trim、去空、去重。
  - 多值参数存在时，空项应被忽略；归一化后为空表示不增加过滤条件。

### 4. Validation & Error Matrix

| 条件 | 行为 |
|------|------|
| 必填时间或分页参数缺失 | 使用现有管理 API 错误响应返回 `{success:false,message:"..."}` |
| 多值参数不存在 | 不增加该维度过滤条件 |
| 多值参数只包含空字符串 | 不增加该维度过滤条件 |
| 新多值参数和旧单值参数同时存在 | 以新多值参数为准，避免混合语义 |
| 多值参数有重复值 | 去重后再传给 Model 层 |

### 5. Good/Base/Bad Cases

- Good: `GET /api/data/export?start_timestamp=1&end_timestamp=2&token_names=a&token_names=b` 只筛选 `a`、`b`。
- Base: `GET /api/data/export?start_timestamp=1&end_timestamp=2` 保持原有全量行为。
- Bad: `GET /api/data/export?token_names=&token_names= ` 不应生成空 `IN` 查询。

### 6. Tests Required

- Controller 或参数归一化测试：
  - 重复 key 能读取多个值。
  - 括号数组 key 能兼容读取。
  - 旧单值参数仍可用。
  - 空白值和重复值被过滤。
- Model 查询测试：
  - 非空切片生成对应筛选结果。
  - 空切片保持原查询范围。
  - 查询写法必须兼容 SQLite、MySQL、PostgreSQL。

### 7. Wrong vs Correct

#### Wrong

```go
tokenName := c.Query("token_names")
tx = tx.Where("token_name = ?", tokenName)
```

#### Correct

```go
tokenNames, _ := c.GetQueryArray("token_names")
if len(tokenNames) > 0 {
    tx = tx.Where("token_name IN ?", tokenNames)
}
```

## 场景：渠道启停与编辑保存状态隔离

### 1. Scope / Trigger

- Trigger: 渠道状态字段 `status` 同时出现在创建表单、编辑表单、列表启停操作中，前端 payload 一旦混用会触发 `错误：无效的参数`，或导致禁用渠道在保存后被误改为启用。
- 适用范围: `/api/channel` 管理接口、`web/default` 渠道表单、`web/classic` 渠道表单与列表操作。

### 2. Signatures

- 创建渠道可以携带初始状态：

```typescript
POST /api/channel
type AddChannelRequest = {
  channel: Partial<Channel> & { status?: number }
}
```

- 编辑渠道必须使用普通更新接口，且 payload 中禁止出现 `status`：

```typescript
PUT /api/channel/
type UpdateChannelPayload = Partial<Channel> & { id: number }
```

- 单个启停必须使用专用状态接口：

```go
type ChannelStatusRequest struct {
	Status int `json:"status"`
}

func UpdateChannelStatus(c *gin.Context)
```

```text
POST /api/channel/:id/status
Body: {"status": 1 | 2}
```

- 批量启停必须使用批量状态接口：

```go
type ChannelStatusBatchRequest struct {
	Ids    []int `json:"ids"`
	Status int   `json:"status"`
}

func BatchUpdateChannelStatus(c *gin.Context)
```

```text
POST /api/channel/status/batch
Body: {"ids": [1, 2], "status": 1 | 2}
```

### 3. Contracts

- 创建契约：
  - `transformFormDataToCreatePayload()` 可以把 `formData.status` 写入 `channel.status`，作为新渠道初始状态。
  - 后端创建接口按现有渠道创建逻辑处理初始状态。
- 编辑契约：
  - `transformFormDataToUpdatePayload()` 必须省略 `status`，即使表单状态字段存在也不能透传。
  - 后端 `UpdateChannel` 会先读取原始 JSON body；只要 body 顶层存在 `status` key，就返回 `MsgInvalidParams`。
  - 编辑保存只负责名称、模型、分组、密钥、配置等可编辑字段，不负责启停状态。
- 启停契约：
  - 单个启停只调用 `POST /api/channel/:id/status`。
  - 批量启停只调用 `POST /api/channel/status/batch`。
  - `status` 只允许 `common.ChannelStatusEnabled` (`1`) 或 `common.ChannelStatusManuallyDisabled` (`2`)。
  - 状态变更成功后需要刷新渠道缓存并重置代理客户端缓存。

### 4. Validation & Error Matrix

| 条件 | 行为 |
|------|------|
| `PUT /api/channel/` body 含有顶层 `status` | 返回 `{success:false,message:"无效的参数"}` 对应的 i18n 错误 |
| `POST /api/channel/:id/status` 的 `id` 不是整数 | 返回 `MsgInvalidParams` |
| 单个启停 body 缺少 `status` 或 JSON 无法绑定 | 返回 `MsgInvalidParams` |
| 单个启停 `status` 不是 `1` 或 `2` | 返回 `MsgInvalidParams` |
| 批量启停 `ids` 为空 | 返回 `MsgInvalidParams` |
| 批量启停 `status` 不是 `1` 或 `2` | 返回 `MsgInvalidParams` |
| 编辑保存禁用渠道 | 渠道保持禁用，不得因为表单默认值变成启用 |

### 5. Good/Base/Bad Cases

- Good: 用户在列表点击禁用，前端调用 `POST /api/channel/10/status` 且 body 为 `{"status":2}`，接口返回成功后列表刷新。
- Base: 用户编辑一个已禁用渠道的模型或分组，前端调用 `PUT /api/channel/`，body 不含 `status`，保存后仍为禁用。
- Bad: 用户编辑已禁用渠道时，前端把表单默认 `status:1` 一起提交到 `PUT /api/channel/`，会被后端拒绝或造成状态回退风险。

### 6. Tests Required

- Controller 测试：
  - `UpdateChannel` 收到 `{"id":1,"status":2}` 时返回 `success:false`。
  - `UpdateChannelStatus` 只接受 `1` 和 `2`，拒绝 `0`、`3`、缺失字段和非法 `id`。
  - `BatchUpdateChannelStatus` 拒绝空 `ids`，并只接受 `1` 和 `2`。
- 前端转换测试或审查点：
  - `transformFormDataToCreatePayload()` 创建 payload 可包含 `channel.status`。
  - `transformFormDataToUpdatePayload()` 更新 payload 不包含 `status`。
  - 列表启停动作调用 `updateChannelStatus()` 或 `batchUpdateChannelStatus()`，不调用 `updateChannel()`。
- 回归场景：
  - 禁用渠道后打开编辑弹窗并保存非状态字段，渠道状态仍为禁用。
  - 点击禁用按钮不会显示 `错误：无效的参数`。

### 7. Wrong vs Correct

#### Wrong

```typescript
await updateChannel(channel.id, {
  ...formPayload,
  status: CHANNEL_STATUS.MANUALLY_DISABLED,
})
```

#### Correct

```typescript
await updateChannel(channel.id, formPayload)
await updateChannelStatus(channel.id, CHANNEL_STATUS.MANUALLY_DISABLED)
```

## 场景：公共 API Key 日志统计筛选联动

### 1. Scope / Trigger

- Trigger: 公共 API Key 日志页 `/log` 修改统计、图表、表格的查询参数或统计口径；或修改 `logs` 表索引来支撑公共日志统计性能。
- 适用范围: `GET /api/log/token/stat`、`GET /api/log/token/data`、`GET /api/log/token`、`GET /api/usage/token/`，以及 `web/default/src/features/token-logs/*`。
- 目标: 统计卡片、模型分布、消耗趋势和日志表格必须使用同一组筛选条件，且不能在 API Key 验证阶段触发全历史日志统计。

### 2. Signatures

- API Key 验证使用轻量只读接口：

```text
GET /api/usage/token/
Authorization: Bearer <api-key>
```

- 公共日志统计和图表接口支持完整筛选参数：

```text
GET /api/log/token/stat?type=<int>&model_name=<string>&request_id=<string>&start_timestamp=<unix>&end_timestamp=<unix>
GET /api/log/token/data?type=<int>&model_name=<string>&request_id=<string>&start_timestamp=<unix>&end_timestamp=<unix>
```

- Model 层使用统一筛选结构：

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

- `logs` 表必须保留公共日志统计所需索引：

```text
idx_logs_token_created_at(token_id, created_at)
idx_logs_token_type_created_at(token_id, type, created_at)
```

### 3. Contracts

- 认证契约：
  - `/log` 输入 API Key 后，前端只调用 `/api/usage/token/` 验证 Key 和读取基础 token 信息。
  - `/api/usage/token/` 必须优先使用 `TokenAuthReadOnly` 写入的 `token_id` / `id` 上下文；只能把 `Authorization` header 作为兼容 fallback。
  - `sk-xxx-suffix` 这类带后缀 Authorization 也必须由认证上下文稳定定位 token，不能在 usage 接口内重新做不完整解析。
- 筛选契约：
  - `type/model_name/request_id/start_timestamp/end_timestamp` 必须从前端一路传到 Controller 和 Model。
  - `model_name` 沿用公共日志表格的安全 LIKE 语义；`request_id` 使用精确匹配。
  - 旧调用方只传时间参数或不传新参数时必须继续可用。
- 统计口径：
  - `count/rpm` 跟随当前日志类型筛选；`type=0` 表示全部日志类型。
  - `quota/prompt_tokens/completion_tokens/total_tokens/tpm` 只统计消费日志。
  - Anthropic token 总量口径是 `prompt_tokens + completion_tokens + cache_read_input_tokens + cache_creation_input_tokens`。
  - 非消费类型筛选时，用量类字段为 0，趋势图返回空态。
- 索引契约：
  - 普通 SQLite/MySQL/PostgreSQL 日志库通过 `AutoMigrate(&Log{})` 创建复合索引。
  - 独立 `LOG_SQL_DSN` 日志库也必须走同一 `migrateLOGDB()` 路径。
  - ClickHouse 日志库不通过 GORM 索引 tag 迁移，继续使用现有 MergeTree 表结构。

### 4. Validation & Error Matrix

| 条件 | 行为 |
|------|------|
| API Key 无效、禁用或超限 | 保持现有认证错误提示，不调用全历史统计兜底 |
| `type=0` 或缺省 | 请求数/RPM 覆盖全部日志类型，用量类指标只覆盖消费日志 |
| `type` 是错误、退款、管理等非消费类型 | 请求数/RPM 正常联动，用量类指标为 0，趋势图显示消费日志空态 |
| `model_name` 含 `%` 通配符 | 只使用现有安全 LIKE helper，避免未转义 LIKE |
| `request_id` 为空 | 不增加请求 ID 过滤 |
| `request_id` 非空 | 使用 `request_id = ?` 精确匹配 |
| 时间范围为空 | 不在认证阶段使用统计接口；统计接口旧调用保持兼容 |
| 日志表缺少复合索引 | 需要通过迁移或发布操作单补齐，避免大日志量下按单列 token 索引回表扫描 |

### 5. Good/Base/Bad Cases

- Good: 用户筛选 `type=2&model_name=claude%&request_id=req_1`，统计、模型分布、趋势图和表格都只反映该 API Key 下匹配消费日志。
- Base: 用户只传 `start_timestamp/end_timestamp`，统计和图表保持旧时间筛选语义。
- Bad: API Key 验证时调用 `/api/log/token/stat` 且不带时间范围，会扫描该 token 全历史日志。
- Bad: `type=5` 错误日志筛选下仍计算 quota/tokens，会把非消费日志混入用量口径。

### 6. Tests Required

- Controller 测试：
  - usage 接口能使用 `TokenAuthReadOnly` 上下文定位 token，兼容带后缀 Authorization。
  - stat/data 接口解析 `type/model_name/request_id/start/end` 并传入 Model。
- Model 测试：
  - `type=0` 时 count 包含全部类型，用量只包含消费日志。
  - 非消费 type 时用量为 0，且不扫描消费日志趋势。
  - Anthropic cache token 口径包含 cache read 和 cache creation。
  - AutoMigrate 会创建 `idx_logs_token_created_at` 和 `idx_logs_token_type_created_at`。
- 前端验证：
  - API Key 验证只调 `/api/usage/token/`。
  - 统计、图表、表格请求参数和 React Query key 都包含完整筛选条件。
  - 点击模型分布项会回填模型输入框并自动查询。

### 7. Wrong vs Correct

#### Wrong

```typescript
// 认证阶段不应调用统计接口；无时间范围会触发全历史日志扫描。
await getTokenLogStat(client)
```

```go
// 用量统计不能复用当前非消费类型筛选。
tx := LOG_DB.Where("token_id = ? AND type = ?", tokenID, logType)
```

#### Correct

```typescript
await getTokenUsage(client)
await getTokenLogStat(client, buildTokenLogQueryParams(appliedFilters))
```

```go
usageParams := tokenLogFilterWithType(params, LogTypeConsume)
usageQuery, err := applyTokenLogFilterParams(LOG_DB.Model(&Log{}), usageParams, true)
```

## 场景：数据看板 Excel 分组导出与大数据量生成

### 1. Scope / Trigger

- 适用于数据看板的 Excel 导出接口，以及复用同一日志筛选条件的导出聚合逻辑。
- 当导出列、聚合维度、明细行上限或日志遍历方式发生变化时，必须同步检查本契约。
- 日志库可能是独立 ClickHouse，不能假设日志表存在可靠的自增主键。

### 2. Signatures

```http
GET /api/data/export?start_timestamp=<秒>&end_timestamp=<秒>&username=<用户>&token_name=<API Key>&group=<分组>
Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
```

```go
func ProcessLogsForExport(
    ctx context.Context,
    startTimestamp int64,
    endTimestamp int64,
    username string,
    tokenNames []string,
    groups []string,
    handleDetail func(log *Log, cacheReadTokens int, cacheCreationTokens int) error,
) ([]LogSummaryByKey, []LogDetailByKeyModel, error)
```

### 3. Contracts

- `start_timestamp`、`end_timestamp` 必填且必须为合法整数；`group`、`token_name` 支持重复查询参数并按集合筛选。
- 每个 Sheet 顶部包含标题与导出元信息（时间范围、分组/API Key 筛选摘要）；业务数据表头不保证位于第 1 行。
- “汇总统计”数据列表固定为：`分组`、`API Key 名称`、`请求次数`、`请求 Token 数`、`请求额度 (USD)`，聚合维度为分组、API Key、用户名；有数据时底部合计行使用 `SUBTOTAL` 公式且位于筛选范围之外。
- “模型明细”保持分段表：分组标题 + 段内表头 + 模型数据 + 静态小计；聚合维度为分组、API Key、用户名、模型；标题必须同时展示分组和 API Key，小计不能跨分组合并；不做整表分组下拉筛选。
- “请求日志”数据列表固定为：`时间`、`分组`、`API Key`、`模型`、`输入 Tokens`、`输出 Tokens`、`额度消耗 (USD)`、`耗时(s)`、`是否流式`、`渠道 ID`、`请求 ID`。
- “请求日志”的输入 Token 单元格必须按内容选择样式：存在 cache read 或 cache creation 说明时写文本并左对齐；没有缓存说明时写数值并使用 `#,##0`、右对齐。不能仅把数值放入文本样式，否则千分位和数值对齐会失效。
- “请求日志”只写入按 `created_at` 升序排列的前 500000 条，保持既有静默截断行为，不新增拒绝或告警文案；“汇总统计”和“模型明细”仍覆盖筛选范围内的全部日志。
- Anthropic 输入 Token 展示与聚合必须包含 cache read 和 cache creation Token，且同一行的 `other` 字段只解析一次。
- 历史日志的空分组保持为空字符串，不能擅自映射为默认分组或其他展示值。
- 数据库日志只能遍历一次：同一次遍历完成全量聚合，并通过回调流式写入限定数量的请求明细。
- 数据库读取必须使用带请求上下文的 `Rows()`、最小字段选择和稳定排序；三个工作表均使用 `excelize.StreamWriter`，写入完成后必须全部 `Flush()` 再返回文件。
- Sheet1/Sheet3 可对数据区启用筛选/Table 与冻结窗格；样式与数字格式属于展示层增强，不得改变聚合口径或导出上限。

### 4. Validation / Error Matrix

| 条件 | 行为 |
|---|---|
| 缺少开始或结束时间 | 返回参数错误，不执行日志扫描 |
| 时间参数无法解析为整数 | 返回参数错误，不生成工作簿 |
| 数据库查询、扫描或遍历结束检查失败 | 终止导出并返回服务端错误 |
| 明细回调或工作表流式写入失败 | 立即停止遍历并返回服务端错误 |
| 请求上下文取消 | 数据库遍历尽快终止，不能继续生成完整文件 |
| 日志超过 500000 条 | 明细保留前 500000 条，汇总继续处理全部日志 |
| 筛选结果为空 | 返回包含三张工作表和表头的有效工作簿 |

### 5. Good / Base / Bad Cases

- Good: 同一 API Key 在两个分组均有日志时，汇总和模型小计分别展示，请求日志逐行保留原始分组。
- Good: 筛选结果超过 500000 条时，前 500000 条请求日志按时间升序写入，后续日志只参与全量汇总。
- Good: 无缓存说明的输入 Token `3000` 作为数值单元格显示为 `3,000`；存在缓存说明时显示为 `3000 (缓存读 100)` 等文本。
- Base: 历史日志分组为空，三张工作表对应分组单元格为空，其他统计不受影响。
- Bad: 为生成三张工作表分别查询日志库，造成三次大范围扫描和重复 JSON 解析。
- Bad: 使用依赖 `id` 游标的批量遍历读取 ClickHouse 日志，导致默认 `id=0` 的记录遗漏或循环异常。
- Bad: 为减少内存而让汇总也只统计前 500000 条，改变既有数据口径。

### 6. Tests Required

- Model 测试：验证相同 API Key 的不同分组分别聚合，汇总与模型明细包含分组，明细回调保持时间顺序。
- Model 测试：验证回调错误和已取消上下文会向调用方返回错误。
- Controller 测试：生成真实工作簿后重新打开，验证三张工作表、元信息、数据表头、筛选结果、分组列、Anthropic cache Token 文本展示、无缓存输入 Token 的 `#,##0` 数值样式，以及汇总合计 `SUBTOTAL` 公式。
- 回归测试：原有公开聚合方法必须保持可用，并复用新的单次遍历实现。

### 7. Wrong vs Correct

#### Wrong

```go
// ClickHouse 日志的 id 可能全部为 0，不能依赖主键游标推进批次。
LOG_DB.Where(filters).FindInBatches(&logs, 1000, processBatch)
```

```go
// 三张工作表不能分别扫描同一批日志。
summary := GetLogSummaryByKey(...)
details := GetLogDetailByKeyModel(...)
logs := GetLogsForExport(...)
```

#### Correct

```go
summary, details, err := ProcessLogsForExport(
    request.Context(), start, end, username, tokenNames, groups, writeDetailRow,
)
```

```go
rows, err := query.WithContext(ctx).
    Select("created_at, username, token_name, model_name, group, prompt_tokens, completion_tokens, quota, use_time, is_stream, channel_id, request_id, other").
    Order("created_at asc").
    Rows()
```

#### Wrong

```go
// 无条件使用文本样式会让纯数字 Token 失去千分位格式和右对齐。
inputTokenCell := styledCell(styles.text, formatExportInputTokens(promptTokens, cacheRead, cacheWrite))
```

#### Correct

```go
inputTokenCell := styledCell(styles.number, promptTokens)
if cacheRead > 0 || cacheWrite > 0 {
    inputTokenCell = styledCell(styles.text, formatExportInputTokens(promptTokens, cacheRead, cacheWrite))
}
```
