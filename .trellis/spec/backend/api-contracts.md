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
