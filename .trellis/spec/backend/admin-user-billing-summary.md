# 管理员用户账务批量摘要契约

## 场景：跨系统管理列表批量读取账务与远端账号事实

### 1. Scope / Trigger

- Trigger：新增或修改管理员批量用户账务摘要、钱包或订阅剩余额度排序、NewAPI 远端状态/角色展示，或调整订阅懒重置投影。
- 适用范围：`POST /api/user/billing-summary`、对应 DTO、Service 聚合排序、Model 批量读取和订阅周期投影。
- 目标：最多三次批量查询返回调用方明确指定的用户，支持跨批次稳定排序，且读取操作不写账务、不同步账号权限。

### 2. Signatures

```http
POST /api/user/billing-summary
Authorization: 管理员会话
Content-Type: application/json
```

```go
type AdminUserBillingSummaryRequest struct {
	UserIDs   []int  `json:"user_ids"`
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"`
}

func GetAdminUserBillingSummaries(
	ctx context.Context,
	request dto.AdminUserBillingSummaryRequest,
) (*dto.AdminUserBillingSummaryResponse, error)

func GetUserBillingSummaryRows(ctx context.Context, userIDs []int) ([]UserBillingSummaryRow, error)
func GetActiveUserSubscriptionsByUserIDs(ctx context.Context, userIDs []int, now int64) ([]UserSubscription, error)
func GetSubscriptionPlansForBillingSummary(ctx context.Context, planIDs []int) ([]SubscriptionPlan, error)

func ProjectUserSubscriptionCycle(
	subscription UserSubscription,
	plan SubscriptionPlan,
	now int64,
) (UserSubscription, bool, error)

func ValidateSubscriptionResetConfiguration(period string, customSeconds int64) error
```

- 自定义重置周期上限使用 `model.MaxSubscriptionResetCustomSeconds`。

### 3. Contracts

- 请求契约：
  - `user_ids` 必填，只允许正整数；严格去重后数量为 1 至 500。
  - `sort_by` 允许 `user_id`、`wallet_quota`、`subscription_remaining`，缺省为 `user_id`。
  - `sort_order` 允许 `asc`、`desc`，缺省为 `desc`。
- 查询契约：
  - Router 只注册管理员路由；Controller 只绑定和映射错误；Service 负责编排与排序；Model 只批量读取和投影。
  - 正常路径固定批量读取用户、有效订阅、相关套餐，不允许逐用户数据库或 HTTP 查询。
  - 用户查询只选择 `id`、`quota`、`used_quota`、`group`、`status`、`role`，不得加载或返回密码、邮箱、Access Token、API Key、支付数据。
- 响应契约：
  - 单项 `status` 为 `ok`、`not_found` 或 `error`。
  - `remote_status`、`remote_role` 原样返回 `User.Status`、`User.Role` 整数值，只用于展示，不触发启停、提权、降权或门户同步。
  - 钱包余额排序使用原始可用 `quota`，不能减去累计 `used_quota`。
  - `amount_total = 0` 表示无限订阅；没有有效订阅表示已知有限零值。
  - `sort_key.kind` 为 `finite`、`infinite`、`unknown`；未知始终置底，同类同值按 `user_id desc`。
  - 订阅剩余额度降序时无限在有限之前，升序时无限在有限之后。
- 周期投影契约：
  - 摘要读取与预消费写路径必须复用 `ProjectUserSubscriptionCycle`，读取路径不得保存投影结果。
  - 自定义周期必须在创建、更新和投影入口统一校验，秒数范围为 `1..MaxSubscriptionResetCustomSeconds`。
  - 长期逾期的自定义周期必须按整周期数进行常数时间跳跃，禁止按周期循环；时间加法和区间计算必须检查 `int64` 溢出及前向推进。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| JSON 非法、`user_ids` 为空或包含非正整数 | 返回管理 API 参数错误，不查询数据库 |
| 去重后用户数量超过 500 | 返回批量数量超限错误，不查询数据库 |
| `sort_by` 或 `sort_order` 非法 | 返回参数错误 |
| 用户不存在或已软删除 | 单项返回 `not_found`，`sort_key.kind = unknown` |
| 套餐缺失、订阅数据非法、周期投影失败或聚合溢出 | 该用户返回 `error`，其他用户继续返回 |
| 任一批量数据库查询失败 | 整体返回数据库错误，日志不得包含敏感字段 |
| 自定义周期秒数小于 1 或超过上限 | 套餐创建/更新拒绝；投影返回错误 |
| 自定义时间相加或投影区间溢出 | 投影返回错误，不循环、不写入错误周期值 |
| 自定义周期长期逾期 | 算术跳至最后一个已到期周期，不能逐周期遍历 |

### 5. Good / Base / Bad Cases

- Good：500 个用户按 1 秒自定义周期逾期多年，摘要通过算术跳跃完成投影，查询次数仍固定。
- Good：同一用户同时存在有限和无限订阅，响应保留有限订阅明细，但 `sort_key.kind` 为 `infinite`。
- Base：用户没有有效订阅，返回空 `items`、零 `finite_remaining` 和有限零排序键。
- Base：AI Fund 并列展示门户状态与 `remote_status`，只提示差异，不在列表刷新时同步。
- Bad：循环调用单用户详情或订阅接口，形成 N+1 HTTP/数据库查询。
- Bad：使用 `quota - used_quota` 作为钱包余额，或使用特殊大整数冒充无限订阅。
- Bad：用 `for next <= now` 逐秒推进自定义周期，导致批量接口被历史数据阻塞。

### 6. Tests Required

- Service 参数测试：空列表、非正 ID、去重、500 上限、非法排序字段与方向。
- Model 测试：最小字段选择、软删除排除、有效订阅过滤、套餐批量读取和敏感字段排除。
- 聚合排序测试：钱包原始额度、有限/无限/混合/无订阅、未知置底、升降序和 `user_id desc` 并列规则。
- 周期投影测试：正常推进、初始化下次重置、1 秒周期长期逾期、超大周期拒绝、时间加法溢出，以及预消费保存结果与只读投影一致。
- Controller/Router 测试：响应包含 `remote_status`、`remote_role`，不含敏感字段；未认证和普通用户被拒绝，管理员可访问。
- 回归命令：`go test ./... -count=1`、`go vet ./...`，并对周期投影、Service、Controller、Router 运行目标竞态检查。

### 7. Wrong vs Correct

#### Wrong

```go
for next > 0 && next <= now {
	base = time.Unix(next, 0)
	next = calcNextResetTime(base, plan, subscription.EndTime)
}
```

```go
for _, userID := range userIDs {
	user, _ := GetUserById(userID, false)
	subscriptions, _ := GetAllActiveUserSubscriptions(userID)
}
```

#### Correct

```go
elapsed := projectionEnd - baseUnix
periods := elapsed / periodSeconds
lastReset := baseUnix + periods*periodSeconds
```

```go
users, err := model.GetUserBillingSummaryRows(ctx, userIDs)
subscriptions, err := model.GetActiveUserSubscriptionsByUserIDs(ctx, userIDs, now)
plans, err := model.GetSubscriptionPlansForBillingSummary(ctx, planIDs)
```
