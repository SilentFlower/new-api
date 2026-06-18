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

