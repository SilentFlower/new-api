# 渠道单用户每日额度契约

> 记录渠道级单用户每日额度的配置、自然日状态、Relay 软上限、正向记账、管理 API 与前端展示契约。

## 场景：按渠道与用户限制每日已结算额度

### 1. Scope / Trigger

- Trigger：修改 `user_daily_quota_limit`、渠道选取上下文、Relay 预扣前检查、渠道正向 `used_quota` 记账、异步任务差额结算、Midjourney 历史渠道切换、额度管理 API 或渠道用户限制 Dialog。
- 统计维度固定为 `channel_id + user_id + 服务端自然日`；同一用户的多个 API Token 共享额度，不同用户、渠道和自然日互不影响。
- 每日额度是软上限：请求前只检查已结算累计，不预占本次估算额度；检查通过的并发请求可以使最终累计略微超过上限。
- 定制逻辑必须保持薄层：自然日、Redis/内存状态和目标值调整归属 `service/channel_user_daily_quota.go`；Relay 错误转换归属独立 Controller/Relay 文件；管理 API 归属 `controller/channel_user_limits.go`；既有热点只保留 context、检查或正向累计调用。

### 2. Signatures

渠道模型、请求上下文和选渠快照：

```go
type Channel struct {
	UserDailyQuotaLimit *int `json:"user_daily_quota_limit"`
}

func (channel *Channel) GetUserDailyQuotaLimit() int

const ContextKeyChannelUserDailyQuotaLimit ContextKey = "channel_user_daily_quota_limit"
const ContextKeyChannelUserDailyQuotaUsed ContextKey = "channel_user_daily_quota_used"

type ChannelMeta struct {
	ChannelUserDailyQuotaLimit int
}

type TaskPrivateData struct {
	ChannelUserDailyQuotaTracked bool `json:"channel_user_daily_quota_tracked,omitempty"`
}
```

状态服务：

```go
var ErrChannelUserDailyQuotaExceeded error
var ErrChannelUserDailyQuotaUnavailable error

type ChannelUserDailyQuotaUsage struct {
	UserID    int   `json:"user_id"`
	UsedQuota int64 `json:"used_quota"`
}

func CheckChannelUserDailyQuota(
	ctx context.Context,
	channelID int,
	userID int,
	limit int,
) (int64, error)

func RecordChannelUserDailyQuota(
	ctx context.Context,
	channelID int,
	userID int,
	quota int,
) error

func RecordRelayChannelUserDailyQuota(
	ctx context.Context,
	relayInfo *relaycommon.RelayInfo,
	quota int,
)

func ListChannelUserDailyQuota(
	ctx context.Context,
	channelID int,
) ([]ChannelUserDailyQuotaUsage, int64, string, error)

func SetChannelUserDailyQuota(
	ctx context.Context,
	channelID int,
	userID int,
	usedQuota int,
) error
```

管理 API：

```text
GET /api/channel/:id/user-daily-quota
  permission: ChannelRead

PUT /api/channel/:id/user-daily-quota/:user_id
  permission: ChannelOperate
  body: {"used_quota": 500000}
```

成功列表响应的 `data` 固定包含：

```json
{
  "channel_id": 80,
  "limit": 1000000,
  "storage_mode": "redis",
  "reset_at": 1787241600,
  "page": 1,
  "page_size": 20,
  "total": 1,
  "items": [
    {
      "user_id": 123,
      "username": "alice",
      "display_name": "Alice",
      "used_quota": 600000,
      "limit": 1000000,
      "remaining_quota": 400000
    }
  ]
}
```

稳定 Relay 错误码：

```go
ErrorCodeChannelUserDailyQuotaExceeded    = "channel_user_daily_quota_exceeded"
ErrorCodeChannelUserDailyQuotaUnavailable = "channel_user_daily_quota_unavailable"
```

管理员错误日志审计字段：

```json
{
  "other": {
    "admin_info": {
      "channel_user_daily_quota": {
        "channel_id": 80,
        "user_id": 123,
        "limit": 1000000,
        "used": 1000000,
        "error_code": "channel_user_daily_quota_exceeded"
      }
    }
  }
}
```

个人目标值调整的管理审计 action 固定为 `channel.user_daily_quota_set`，参数至少包含 `channel_id`、`user_id` 和 `used_quota`。

### 3. Contracts

#### 配置与兼容性

- `user_daily_quota_limit` 只接受整数 `0..common.MaxQuota`；`nil`、历史 `NULL`、显式 `0` 和异常负值在读取时均按不限处理。
- 字段是普通可空整数列，依赖现有 GORM `AutoMigrate`，不得增加数据库方言专属 SQL 或数据库默认值。
- 更新请求显式传 `null` 时归一化为 `0`；创建、更新和复制 payload 必须保留显式零值。
- 字段属于非敏感渠道运营配置；渠道读权限可以查看，修改沿用现有非敏感渠道字段权限。
- `middleware.SetupContextForSelectedChannel` 每次初选或重试选渠后都必须刷新 `ContextKeyChannelUserDailyQuotaLimit`；`RelayInfo.InitChannelMeta` 将该值冻结到最终尝试的 `ChannelMeta`。
- `relaykit/` 只增加稳定错误码，不得反向依赖根模块；修改后必须在 `relaykit/` 内以 `GOWORK=off` 独立构建。

#### 自然日与状态存储

- Redis key 固定为 `channel_user_daily_quota:{channel_id}:YYYY-MM-DD`，类型为 Hash；field 是十进制 `user_id`，value 是已记录正向额度整数。
- 服务端使用 `time.Local` 计算自然日和 `reset_at`；新自然日使用新 key，不依赖定时清空。多实例部署必须配置一致时区。
- Redis key 的 TTL 必须覆盖当前自然日结束后至少 24 小时；正向累计使用 Lua 原子执行 `HINCRBY + EXPIRE`，目标值覆盖使用 Lua 原子执行 `HSET/HDEL + EXPIRE`。
- Relay 热路径只做单 field 读取，不得使用数据库查询、`KEYS` 或 `SCAN`；管理列表可以对当前渠道当前日期执行 `HGETALL`。
- 未配置 Redis 时使用互斥锁保护的进程内状态，API 返回 `storage_mode=memory`；已配置 Redis 但客户端或操作不可用时失败关闭，禁止降级到内存继续放行。
- `limit <= 0` 的检查和 `quota <= 0` 的记录必须快速返回，不访问 Redis。渠道 ID、用户 ID、目标值和累计值必须在写入前完成正数、上限与溢出校验。

#### Relay 检查与正向累计

- 检查顺序固定为：完成选渠和价格计算 -> 免费请求直接继续 -> 检查当日已结算额度 -> 预扣费 -> 实际上游调用。
- `used < limit` 时放行；`used >= limit` 时返回本地 `429`。不得把估算额度加入检查，也不得实现请求排队或严格预占。
- `429/503` 均设置 `skipRetry`，不得进入 `processChannelError`、渠道自动禁用、换渠重试或预扣费。
- 每次真实换渠重试都重新检查当前候选渠道；失败尝试没有正向结算，不增加旧渠道累计。
- 普通文本、图片、音频、Realtime、Responses、Alpha Search、视觉辅助、异步任务和 Midjourney 必须在各自真实预扣与上游调用前走同一领域检查。
- 正向累计与现有 `model.UpdateChannelUsedQuota` 调用点相邻，并使用最终 `RelayInfo.ChannelMeta.ChannelUserDailyQuotaLimit` 快照判断是否追踪；不得把累计塞入 `BillingSession.Settle` 或预扣逻辑。
- 同一业务结果必须复用现有一次性结算保护，重复终止事件、重试失败事件或重复回调不得重复累计。
- 后续退款、负向差额和管理员退款不自动回退每日累计；每日累计与渠道 `used_quota` 一致，表示历史正向使用量。
- 异步任务提交时冻结 `ChannelUserDailyQuotaTracked`。初始正向额度计入提交日，后续正向差额计入实际结算日；旧任务和限额关闭时提交的任务不得在之后启用限额时补记。
- Midjourney 历史操作切回原任务渠道时，必须同时同步 Gin context 与 `RelayInfo.ChannelMeta` 的渠道 ID、类型、Key、Base URL、并发限制和每日额度限制，确保检查、上游和记账归属同一最终渠道。

#### 管理 API 与前端

- 列表只通过 Model 层批量查询 `id/username/display_name`，不得返回邮箱、密码、余额、Token、渠道 Key、Redis 地址、请求体或响应体。
- `used_quota` 表示调整后的当日已使用额度，不是增量或剩余额度；`0` 删除当前用户 field，正数覆盖目标值，允许目标值达到或超过渠道上限。
- 个人调整只修改限额状态，不修改消费日志、用户余额、Token 余额、渠道历史 `used_quota` 或当前并发，也不取消在途请求。在途请求完成后继续把实际额度加到新目标值上。
- 管理状态存储错误必须在服务端记录脱敏日志，对客户端只返回本地化通用错误，不得暴露 Redis 地址、连接错误或数据库细节。
- 渠道管理使用独立 `ChannelUserLimitsDialog`；每日额度和当前并发分为两个 Tab，不在渠道主表持续轮询。
- 当前并发仅在 Dialog 打开且并发 Tab 激活时每 5 秒刷新；关闭 Dialog 或切换 Tab 后停止。分页总数收缩时必须回退到最后有效页。
- 调整确认必须展示用户、调整前金额和调整后金额；无 `ChannelOperate` 权限时操作保持禁用，并提供可聚焦的权限说明。
- 查询错误兜底、空状态、内存模式提示和调整结果均必须通过 `t(...)`，七种前端 locale 保持同一 key 集合。

### 4. Validation & Error Matrix

| 条件 | HTTP/错误码 | 行为 |
| --- | --- | --- |
| 配置缺失、`NULL` 或 `0` | 无错误 | 不检查、不记录、不访问 Redis |
| 配置小于 `0`、含小数或大于 `common.MaxQuota` | 管理 API 参数错误 | 不保存配置 |
| `used < limit` | 无错误 | 允许请求继续，可能因在途请求最终超额 |
| `used >= limit` | `429 channel_user_daily_quota_exceeded` | 不预扣、不访问上游、不重试、不禁用渠道 |
| Redis 已启用但不可用 | `503 channel_user_daily_quota_unavailable` | 失败关闭，不降级内存、不预扣、不访问上游 |
| 成功响应后的累计写入失败 | 保留原成功响应 | 记录带 request ID 的脱敏告警；后续检查在故障期间失败关闭 |
| 个人调整目标为 `0` | 管理 API 成功 | 删除当前自然日 field，不影响其他业务数据 |
| 个人调整目标超出 `0..common.MaxQuota` | 管理 API 参数错误 | 不修改状态 |
| 管理状态查询存储失败 | 本地化通用管理错误 | 服务端保留脱敏诊断，客户端不出现存储地址或底层错误 |
| Task/Midjourney 返回每日额度错误码 | 对应协议的 `429/503` | Controller 还原稳定错误并写一次统一错误日志 |

### 5. Good / Base / Bad Cases

- Good：渠道上限为 `100`，当前累计为 `80`，两个并发请求都通过检查并分别结算 `30`；最终累计为 `140`，随后新请求收到 `429`。
- Good：首个渠道真实失败后重试到第二个渠道，只检查并累计第二个最终成功渠道；首个渠道累计保持不变。
- Good：管理员把用户累计从 `600` 设置为 `200`，一个已在途请求随后结算 `50`，最终累计为 `250`，日志和余额均不改变。
- Good：跨日异步任务在提交日记录初始额度，在第二天只记录正向差额；旧任务没有追踪标记时不补记。
- Base：历史渠道字段为 `NULL`，Relay、任务和管理页面保持不限行为。
- Base：未配置 Redis 时单实例内存状态正常工作，并在 UI 明确提示数据范围。
- Bad：请求检查时把预估额度原子预占，会把软上限错误实现成严格限额并增加退款、超时和重试复杂度。
- Bad：按当前数据库中的渠道配置决定结算追踪，会让限额关闭时发起的旧请求在中途启用后被错误补记。
- Bad：个人调整同时修改用户余额或消费日志，会把限额控制状态误当成财务退款。
- Bad：通过 `SCAN channel_user_daily_quota:*` 构造管理列表，会把渠道级管理查询变成全库热扫描。

### 6. Tests Required

- Model/Controller：覆盖历史 `NULL`、正数、显式 `0/null`、负数、小数、超过 `common.MaxQuota`、非敏感字段分类和表单/API 往返。
- Service 内存模式：断言用户/渠道/日期隔离、软上限、超额后阻止、自然日切换、目标值覆盖、`0` 删除、正向累计和溢出保护。
- Service Redis 模式：断言 Hash field、原子累计/覆盖、TTL、列表、Redis 不可用时失败关闭，以及不限状态不访问 Redis。
- Controller/Relay：断言 `429/503` 稳定错误码、`skipRetry`、不上游、不预扣、不自动禁用和错误日志请求级去重。
- 最终渠道：断言 Relay 重试和 Midjourney 历史操作只累计最终实际渠道，失败渠道为零。
- 一次性记账：使用重复终止事件或重复回调断言只结算、只累计一次。
- 异步任务：覆盖跨日正向差额、负向退款不回退、旧任务和关闭限额时提交的任务不补记。
- 独立正向路径：至少覆盖普通 Relay、WebSocket、任务、Midjourney、工具调用和违规费用的实际累计。
- 管理 API：断言权限、分页、用户摘要最小字段、目标值调整、存储错误脱敏和结构化审计模板。
- 前端：断言表单金额换算、确认前后值、权限说明、内存提示、分页回退、并发轮询启停和错误兜底 i18n。
- 完成后运行相关 race 测试、`go test ./...`、`go vet ./...`、`relaykit` 独立 build/vet、前端定向测试、typecheck、build、lint、i18n sync 和 `git diff --check`。

### 7. Wrong vs Correct

#### Wrong：预扣后才检查每日额度

```go
if err := relayInfo.Billing.Reserve(quota); err != nil {
	return apiError(err)
}
return checkChannelUserDailyQuota(c)
```

问题：本地额度拒绝已经产生资金预扣，还可能被外围逻辑误判为上游失败。

#### Correct：价格已知后、预扣和上游调用前检查

```go
if !priceData.FreeModel {
	if apiErr := checkChannelUserDailyQuota(c); apiErr != nil {
		return apiErr
	}
}
return relayInfo.Billing.Reserve(priceData.QuotaToPreConsume)
```

#### Wrong：从实时渠道配置决定是否记录旧请求

```go
channel, _ := model.GetChannelById(relayInfo.ChannelId, true)
if channel.GetUserDailyQuotaLimit() > 0 {
	RecordChannelUserDailyQuota(ctx, relayInfo.ChannelId, relayInfo.UserId, quota)
}
```

问题：请求进行期间启用限额会回溯记录旧请求；重试或历史渠道切换也可能把额度记到错误渠道。

#### Correct：使用最终尝试冻结的 Relay 快照

```go
model.UpdateChannelUsedQuota(relayInfo.ChannelId, quota)
service.RecordRelayChannelUserDailyQuota(ctx, relayInfo, quota)
```

累计调用与正向渠道使用量相邻，内部根据 `ChannelMeta.ChannelUserDailyQuotaLimit` 快照快速决定是否追踪。

#### Wrong：个人“重置”同时退款

```go
model.IncreaseUserQuota(userID, oldUsedQuota-newUsedQuota)
service.SetChannelUserDailyQuota(ctx, channelID, userID, newUsedQuota)
```

#### Correct：只覆盖限额状态

```go
err := service.SetChannelUserDailyQuota(ctx, channelID, userID, newUsedQuota)
```

个人目标值调整不是财务操作，不得修改余额、Token、日志或渠道历史累计。
