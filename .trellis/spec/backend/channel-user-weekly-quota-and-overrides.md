# 渠道单用户每周额度与个人覆盖契约

> 本规范约束渠道单用户每周额度、并发/日限/周限个人提额、统一管理 API，以及 ai-fund 通过 Cloudflare Worker BFF 接入时的权威边界。

## 1. 适用范围与触发条件

以下改动必须阅读并遵守本规范：

- 修改 `Channel.UserWeeklyQuotaLimit`、每周额度检查、累计、调整或刷新周期。
- 修改 `ChannelUserLimitOverride`、有效限制解析、缓存、到期或撤销语义。
- 修改渠道用户限制搜索、统一状态、个人覆盖列表或写接口。
- 新增日限、周限或并发的记账、任务结算、退款、Midjourney 错误映射路径。
- ai-fund 或其他外部管理端通过 BFF 读取或修改 NewAPI 渠道用户限制。

本功能是软上限控制：请求开始时根据已记录值判断是否放行，完成计费后记录正向额度；不预占未来消耗，因此允许单次请求使已用值超过上限。

## 2. 关键签名

### 模型与输入

```go
type Channel struct {
	UserWeeklyQuotaLimit *int `json:"user_weekly_quota_limit"`
}

func (channel *Channel) GetUserWeeklyQuotaLimit() int

type ChannelUserLimitOverride struct {
	Id                   int
	ChannelId            int
	UserId               int
	UserConcurrencyLimit *int
	UserDailyQuotaLimit  *int
	UserWeeklyQuotaLimit *int
	ExpiresAt            int64
	UpdatedBy            int
	CreatedAt            int64
	UpdatedAt            int64
}

type ChannelUserEffectiveLimits struct {
	BaseConcurrency      int
	BaseDailyQuota       int
	BaseWeeklyQuota      int
	OverrideConcurrency  *int
	OverrideDailyQuota   *int
	OverrideWeeklyQuota  *int
	EffectiveConcurrency int
	EffectiveDailyQuota  int
	EffectiveWeeklyQuota int
	ExpiresAt            int64
	Active               bool
}

type ChannelUserLimitOverrideInput struct {
	UserConcurrencyLimit *int
	UserDailyQuotaLimit  *int
	UserWeeklyQuotaLimit *int
	ExpiresAt            int64
}
```

### 服务接口

```go
func ResolveChannelUserEffectiveLimits(ctx context.Context, channel *model.Channel, userID int) (ChannelUserEffectiveLimits, error)
func ApplyChannelUserEffectiveLimits(c *gin.Context, channel *model.Channel) ChannelUserEffectiveLimits
func ReplaceChannelUserLimitOverride(ctx context.Context, channel *model.Channel, userID int, input ChannelUserLimitOverrideInput, updatedBy int) error
func DeleteChannelUserLimitOverride(ctx context.Context, channelID int, userID int) error

func CheckChannelUserWeeklyQuota(ctx context.Context, channelID int, userID int, limit int) (int64, error)
func RecordChannelUserWeeklyQuota(ctx context.Context, channelID int, userID int, quota int) error
func ListChannelUserWeeklyQuota(ctx context.Context, channelID int) ([]ChannelUserWeeklyQuotaUsage, int64, string, error)
func SetChannelUserWeeklyQuota(ctx context.Context, channelID int, userID int, usedQuota int) error
func GetChannelUserWeeklyQuotaUsage(ctx context.Context, channelID int, userID int) (int64, int64, string, error)
func RecordChannelUserQuotaUsage(ctx context.Context, channelID int, userID int, quota int) error
func RecordRelayChannelUserQuotaUsage(ctx context.Context, relayInfo *relaycommon.RelayInfo, quota int)
```

### 管理 API

| 方法 | 路径 | 契约 |
|------|------|------|
| `GET` | `/api/channel/:id/user-weekly-quota` | 分页返回当前自然周出现过的用户，包含默认值、覆盖值、生效值、已用值和刷新时间 |
| `PUT` | `/api/channel/:id/user-weekly-quota/:user_id` | 请求体 `{"used_quota": int}`，设置当前自然周已用值 |
| `GET` | `/api/channel/:id/user-limit-users?keyword=&p=&page_size=` | 按用户 ID、用户名或显示名搜索，不依赖请求或用量历史 |
| `GET` | `/api/channel/:id/user-limit-status/:user_id` | 返回并发、日限、周限的默认值、覆盖值、生效值、当前值、剩余值及存储模式 |
| `GET` | `/api/channel/:id/user-limit-overrides?p=&page_size=` | 分页返回当前有效覆盖，包含用户最小摘要及三项覆盖值、生效值、到期时间 |
| `PUT` | `/api/channel/:id/user-limit-overrides/:user_id` | 整条替换个人覆盖；三个限制字段均为 `null` 时等价于撤销 |
| `DELETE` | `/api/channel/:id/user-limit-overrides/:user_id` | 撤销个人覆盖并立即失效缓存 |

## 3. 核心契约

### 每周额度

- 周期使用服务端本地时区，自周一 `00:00:00` 起算，下周一 `00:00:00` 刷新。
- Redis 哈希键固定为 `channel_user_weekly_quota:{channel_id}:YYYY-MM-DD`，字段为用户 ID；Redis 关闭时使用进程内存存储。
- `nil`、`0` 或历史异常负值均表示不限；即使当前限制关闭，完成计费后的正向额度仍应进入日、周累计，便于以后启用限制和管理查看。
- 只记录正向额度。退款、回补或负差额不得减少日、周累计；`quota <= 0` 必须直接忽略。
- Redis 已启用但客户端不可用时，只有启用了正数周限的请求检查需要失败关闭；不得把存储故障当作不限。
- `used >= limit` 返回稳定的 `429` 且禁止换渠重试；存储不可用返回稳定的 `503` 且禁止换渠重试。
- 所有结算入口必须调用统一的 `RecordChannelUserQuotaUsage` 或 `RecordRelayChannelUserQuotaUsage`，保证一次正向消费同时进入日限与周限。

### 个人覆盖

- 覆盖只允许临时或永久提额，不允许降额。单项覆盖必须大于对应渠道默认值，且不超过并发 `1000` 或额度 `common.MaxQuota`。
- 渠道默认值为不限（`<= 0`）时，不允许为该项设置个人覆盖；覆盖不能把不限改成有限。
- 生效值为 `max(base, override)`。未设置、已过期、非法旧数据或读取失败时使用渠道默认值。
- `expires_at = 0` 表示永久；正数必须是未来 Unix 秒。不提供 `starts_at`，保存成功后立即生效。
- 三个覆盖字段均为 `null` 时删除整条记录；部分字段为 `null` 表示该项跟随渠道默认值。
- 数据库表是权威来源，唯一键为 `(channel_id, user_id)`；写入和删除后必须失效对应缓存。
- 缓存 TTL 不得超过 30 秒，也不得晚于 `expires_at`。Redis 关闭时内存缓存最多 4096 项，写入前清理过期项，达到上限时允许任意淘汰一项。
- Relay 读取覆盖失败时记录安全告警并回落渠道默认值，因为覆盖只会放宽限制；管理员查询和写入必须暴露错误，不得伪装成功。

### 管理与外部接入

- 用户搜索基于用户主表，不要求该用户已有请求、日周用量或个人覆盖记录；无历史用户的当前值返回 `0`。
- 统一状态和个人覆盖列表只返回管理所需的最小用户摘要，不得泄露密码、令牌或其他敏感字段。
- 管理写操作必须校验渠道、用户、管理员身份和输入，并写管理审计日志。
- Midjourney 等非标准响应路径必须通过统一状态码映射函数分别识别日限和周限，禁止在周限分支复用日限错误变量或解引用空错误。
- ai-fund 的 Cloudflare Worker 只承担鉴权、号池到 NewAPI 渠道映射、字段白名单、审计和转发；NewAPI 仍是限制配置、当前状态和校验规则的唯一权威来源。
- ai-fund 的 D1 映射读取在写路径必须失败关闭；D1 异常时返回配置不可用且不得调用 NewAPI。写成功后应重读 NewAPI 统一状态，避免用提交前快照推导结果。

## 4. 校验与错误矩阵

| 场景 | 预期结果 |
|------|----------|
| 周限为 `nil`、`0` 或负数 | 按不限处理，不执行请求前存储检查 |
| 周限大于 `common.MaxQuota` | 渠道保存失败，返回参数错误 |
| 已用周额度小于生效周限 | 放行请求 |
| 已用周额度等于或大于生效周限 | `429 channel_user_weekly_quota_exceeded`，跳过重试 |
| 启用正数周限且 Redis 已启用但不可用 | `503 channel_user_weekly_quota_unavailable`，跳过重试 |
| 免费请求或最终正向额度为 `0` | 不增加日、周累计 |
| 退款或负差额 | 不减少日、周累计 |
| 覆盖值小于或等于渠道默认值 | 拒绝保存 |
| 渠道默认值为不限但提交该项覆盖 | 拒绝保存 |
| 覆盖超过允许最大值 | 拒绝保存 |
| `expires_at < 0` 或正数且不晚于当前时间 | 拒绝保存 |
| 三个覆盖字段均为 `null` | 删除覆盖并失效缓存 |
| 覆盖已过期 | 不再生效，状态回落默认值 |
| 搜索无请求历史的现有用户 | 返回用户摘要；并发、日限、周限当前值均为 `0` |
| 管理查询读取存储或数据库失败 | 返回明确错误，不返回伪造状态 |
| Relay 覆盖读取失败 | 记录告警，使用渠道默认值继续执行 |

## 5. Good / Base / Bad 示例

### Good

- 渠道并发默认值为 `3`，个人覆盖为 `8`，一小时后到期；当前生效值为 `8`，到期后自动回落 `3`。
- 用户从未使用过该渠道，管理员仍可通过用户名搜索并配置个人覆盖，统一状态显示当前值 `0`。
- 一次实际扣费 `100` 同时调用统一记录函数，日累计和周累计都增加 `100`。

### Base

- 某项覆盖为 `null` 时，该项使用渠道默认值；其他非空覆盖项仍可独立生效。
- `expires_at = 0` 时覆盖永久有效，直到管理员替换或撤销。
- 周限关闭期间仍记录正向用量，重新启用后按当前自然周已有累计判断。

### Bad

- 只对有用量记录的用户开放个人覆盖选择。
- 覆盖值允许小于默认值，形成个人降额或临时封禁。
- Redis 故障时把已启用的周限当作不限继续放行。
- 在各结算路径分别调用日限或周限记录，造成某些路径漏记其中一个周期。
- ai-fund 在 D1 映射读取失败后继续使用猜测的渠道 ID 调用 NewAPI。

## 6. 必需测试

修改本契约覆盖的代码时，测试至少应保护以下可观察行为：

- 自然周从周一零点开始，跨周后旧值不可见，返回的 `reset_at` 指向下周一零点。
- Redis 与内存模式的检查、正向累计、列表、目标值调整和错误语义一致。
- Redis 已启用但客户端不可用时，正数周限失败关闭；关闭周限时不依赖存储。
- `RecordChannelUserQuotaUsage` 一次调用同时更新日、周累计，零值和负值不累计。
- 覆盖校验保护提额、最大值、永久/临时到期和全空撤销语义。
- 覆盖缓存不超过 30 秒和到期时间，写后失效；内存缓存清理过期项且最多 4096 项。
- 统一状态明确区分默认值、覆盖值、生效值，并覆盖无请求历史用户当前值为 `0`。
- 用户搜索不依赖请求、用量或覆盖记录，分页与关键字契约稳定。
- Midjourney 日限/周限响应分别得到正确 HTTP 状态，周限分支不访问日限空错误。
- ai-fund BFF 的 D1 写路径失败关闭、字段白名单、写后权威回读、迟到响应隔离和审计结果均有回归测试。

## 7. Wrong vs Correct

错误：在新的结算入口只补一类周期记录，或者复制两次调用后遗漏错误合并。

```go
RecordChannelUserDailyQuota(ctx, channelID, userID, quota)
```

正确：所有结算入口复用统一记录函数，让日、周记账保持同一覆盖面。

```go
if err := RecordChannelUserQuotaUsage(ctx, channelID, userID, quota); err != nil {
	return err
}
```

错误：在 Midjourney 周限分支继续读取日限错误，可能返回错误状态或触发空指针。

```go
if weeklyQuotaErr != nil {
	return dailyQuotaErr.StatusCode
}
```

正确：把响应描述交给统一映射函数，分别识别日限和周限错误码。

```go
if status, ok := channelUserQuotaMidjourneyHTTPStatus(response); ok {
	return status
}
```
