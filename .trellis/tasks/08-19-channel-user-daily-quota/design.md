# 技术设计 — 渠道级用户每日额度限制与用量可视化

## 1. 设计目标与关键决定

本功能在现有 `channel_id + user_id` 并发限制基础上增加渠道单用户每日额度软上限，并为管理员提供指定渠道的当日额度和当前并发视图。

关键决定：

- 每日额度是软上限：请求前只检查已经结算的当日累计，不预占请求估算额度。
- 检查时累计低于上限的请求可以完成；并发中的请求可能使最终累计超过上限，超额后新请求被阻止。
- 当日累计表示渠道正向使用量，与现有 `Channel.UsedQuota` 语义一致；后续退款不自动回退历史使用量。
- 每日额度和当前并发只在指定渠道的管理 Dialog 中查询，不在渠道主表持续轮询。
- Redis 已配置但不可用时检查失败关闭；未配置 Redis 时使用与现有并发限制一致的单实例内存模式。
- 每日额度配置为 `0` 时完全跳过检查和记录，使关闭状态保持零额外热路径开销；当天中途启用后从启用时开始累计，不回溯当天早先用量。

## 2. 薄层架构边界

完整定制逻辑进入独立领域文件，现有热点只保留配置传递、检查或累计调用：

- `service/channel_user_daily_quota.go`：独占自然日周期、Redis/内存状态、额度检查、正向累计、列表和单用户目标值调整。
- `service/channel_user_concurrency.go`：继续独占并发租约；仅扩展可枚举用户索引和只读统计，不改变获取、续租、释放语义。
- `controller/channel_user_limits.go`：独占管理 API、用户摘要拼装、权限内响应和额度错误审计信息。
- `controller/channel_user_daily_quota.go`：独占 Gin context 中的渠道额度检查和协议错误转换；主 Relay 只调用窄检查入口。
- `model/channel.go`：只增加渠道配置字段和归一化 getter。
- `middleware/distributor.go`：只把归一化配置写入已选渠道 context，重试时沿用同一入口刷新。
- 现有计费文件只在正向结算成功的位置调用每日额度累计，不复制 Redis、日期或个人调整逻辑。
- 前端新增独立 Dialog；现有渠道 Provider、行操作和 Dialog 注册只增加入口。

不为本功能重构现有 BillingSession、Relay 重试、并发租约或渠道表单框架。每个旧文件的修改必须能用一句话说明必要性。

## 3. 渠道配置

在 `model.Channel` 增加普通可空整数列：

```go
UserDailyQuotaLimit *int `json:"user_daily_quota_limit"`
```

- `nil`、历史 `NULL`、显式 `0` 和异常负值均按不限处理。
- 正数范围为 `1..common.MaxQuota`，使用现有 `AutoMigrate`，不写数据库方言专属 SQL。
- getter 返回归一化后的 `int`，并通过新的 context key 传到每次实际选渠尝试。
- `relay/common.ChannelMeta` 增加 `ChannelUserDailyQuotaLimit int`，`RelayInfo.InitChannelMeta` 从当前选渠 context 读取，使检查和后续记账都使用最终实际尝试的配置快照；重试换渠时随 `ChannelMeta` 一起刷新。
- 创建、更新和复制渠道必须保留显式零值；字段归类为非敏感渠道运营配置。
- 前端表单按当前额度显示模式输入，通过 `quotaUnitsToDollars` 和 `parseQuotaFromDollars` 在显示值与内部额度单位之间转换。

## 4. 每日额度状态服务

### 4.1 Redis 数据结构

当前自然日使用一个渠道级 Hash：

```text
channel_user_daily_quota:{channel_id}:YYYY-MM-DD
```

- field：十进制 `user_id`。
- value：该用户在该渠道当前自然日已记录的正向额度，使用整数。
- Redis hash tag 固定为渠道 ID，便于与渠道级状态保持同槽。
- key 的过期时间覆盖当前自然日结束后至少 24 小时；新日使用新 key，逻辑重置不依赖定时任务。
- 服务端以 `time.Local` 计算自然日和 `reset_at`，与项目现有每日重置语义一致；多实例部署必须使用一致时区。
- 每次正向额度按实际写入发生时的服务端自然日选择 key；跨日异步任务的初始扣费和后续正向差额可以落入不同日期。

请求前检查只执行一次 `HGET` 等价操作：

```text
limit <= 0 -> 直接通过，不访问状态存储
used < limit -> 通过
used >= limit -> 返回超限
```

结算累计使用 Lua 或事务管道原子执行正向 `HINCRBY` 和过期时间刷新。累计前验证 `quota > 0`、渠道 ID 和用户 ID，使用安全整数边界，禁止负数和溢出写入。

管理查询使用 `HGETALL` 读取当前渠道/日期的用户额度并排序分页；个人调整使用原子 `HSET` 写入目标值，目标值为 `0` 时使用 `HDEL` 删除 field。Relay 热路径不使用 `KEYS`、`SCAN` 或数据库聚合。

### 4.2 内存模式

未配置 Redis 时使用互斥锁保护的 `map[periodKey]map[userID]int64`：

- 检查、累计、列表和目标值调整在同一短临界区完成。
- 每次访问清理已过期日期 bucket。
- 仅代表当前实例，API 返回 `storage_mode=memory`，前端明确提示。

### 4.3 错误与可用性

- 超限：HTTP `429`，稳定错误码 `channel_user_daily_quota_exceeded`。
- Redis 已配置但不可用：HTTP `503`，稳定错误码 `channel_user_daily_quota_unavailable`。
- 两类错误均 `skipRetry`，不得进入 `processChannelError`、渠道自动禁用或计费预扣。
- 管理员审计信息放入 `other.admin_info.channel_user_daily_quota`，包含 channel ID、user ID、limit、used 和错误码。
- 请求成功后的累计写入失败只记录带 request ID 的安全告警，不改写已经完成的上游响应；Redis 故障期间后续请求会在前置检查处失败关闭。

## 5. Relay 与计费接入

### 5.1 主 Relay

每次选渠尝试的数据流：

```text
选渠并刷新 context
  -> 请求准备与价格估算
  -> 免费请求直接继续
  -> 检查当前渠道/用户已结算的每日额度
  -> 现有预扣费
  -> 实际上游调用
  -> 成功后现有结算记录实际额度
```

- 检查放在价格已知、预扣费和上游调用之前，免费请求不访问每日额度状态。
- 每次重试重新检查候选渠道；失败尝试没有成功结算，因此不会增加旧渠道累计。
- Responses WebSocket 每个实际计费 turn 走同一检查；连接级并发租约行为不变。

### 5.2 正向累计

- 每日额度累计与现有 `model.UpdateChannelUsedQuota` 的正向记账点对齐；只有对应资金结算成功、即将写入正向渠道使用量时，才调用每日额度服务。
- 普通 Relay 使用最终 `RelayInfo.ChannelMeta.ChannelUserDailyQuotaLimit` 作为追踪快照；快照为 `0` 时跳过，正数时按最终渠道、用户和实际正向额度累计。
- 不把累计职责放进 `BillingSession.Settle`：该会话只负责资金与 Token 生命周期，而渠道 `used_quota` 由后续消费记账路径更新；保持两者相邻才能避免结算失败或日志路径差异造成口径漂移。
- 文本、图片、音频、Realtime、视觉辅助、工具调用、普通任务、Midjourney 和违规费用等现有正向 `UpdateChannelUsedQuota` 调用点只增加一次窄累计调用，并复用各自已有的一次性结算/记账保护避免重复累计。
- 异步任务提交时把 `ChannelUserDailyQuotaTracked bool` 写入 `TaskPrivateData`；初始正向扣费按提交日累计，后续正向差额按实际差额记账日累计，负向调整不回退。旧任务及限额关闭时提交的任务该字段为 `false`，之后启用限额也不补记。

### 5.3 特殊入口检查

- 普通文本、图片、音频、Responses、Claude、Gemini、Realtime：由主计费准备入口检查。
- Alpha Search：在其独立预扣入口检查。
- 异步任务提交：每次实际提交尝试在价格计算后、预扣和上游请求前检查。
- 视觉辅助：按实际辅助渠道、当前用户和辅助请求价格检查。
- Midjourney：在已确定渠道和正向价格后、HTTP 上游调用前检查。
- 不产生渠道额度的 `count_tokens`、纯数据库任务查询和管理端渠道测试不做每日额度检查。

## 6. 当前并发统计

现有租约 key 保持不变：

```text
channel_user_concurrency:{channel_id}:{user_id}
```

新增渠道级用户索引：

```text
channel_user_concurrency_users:{channel_id}
```

- 获取租约的同一 Lua 调用在成功后 `SADD user_id`，不增加 Redis 往返次数。
- 释放租约的同一 Lua 调用在用户租约集合为空时 `SREM user_id`。
- 索引设置 TTL；进程崩溃遗留成员由管理查询清理过期租约后移除。
- 查询先读取索引，再用 pipeline 清理各用户过期 member 并取得 `ZCARD`；不使用全库 `SCAN`。
- 内存模式直接从现有租约 map 计算当前有效数量。
- 统计 API 只返回当前并发数，不暴露 lease ID、Redis key 或过期 member。

## 7. 管理 API

新增路由：

```text
GET    /api/channel/:id/user-daily-quota
PUT    /api/channel/:id/user-daily-quota/:user_id
GET    /api/channel/:id/user-concurrency
```

- 两个 GET 使用 `ChannelRead`；个人调整使用 `ChannelOperate`。
- GET 支持现有分页参数，响应包含渠道 ID、配置上限、`storage_mode`、`reset_at`、总数和用户列表。
- 用户列表通过 Model 层一次查询 ID、username、display_name，不返回邮箱、余额、Token 或认证信息。
- 个人调整请求体为 `{"used_quota": <内部额度整数>}`，字段表示调整后的已使用额度；正数使用 `HSET` 覆盖当前自然日值，`0` 删除 field。剩余额度由渠道上限派生，消费日志、余额、渠道历史累计和当前并发不变。
- 渠道不存在、用户 ID 非法、状态存储不可用和权限不足使用现有管理 API 响应规范。

## 8. 前端设计

- 渠道编辑表单在现有用户并发配置附近增加“单用户每日额度上限”，`0` 表示不限。
- 渠道行操作菜单增加“用户限制状态”，打开独立 `ChannelUserLimitsDialog`。
- Dialog 使用两个 Tabs：
  - “每日额度”：用户、当日使用、上限、剩余、个人调整操作。
  - “当前并发”：用户、当前并发、并发上限。
- 每日额度视图支持分页、手动刷新、“调整后的已使用额度”金额输入、调整确认和成功后的 query invalidation。
- 当前并发 Tab 打开时按固定间隔自动刷新，并提供手动刷新；关闭 Dialog 后停止轮询。
- `storage_mode=memory` 时显示“仅当前实例”状态提示，Redis 模式不显示多实例警告。
- 所有新增文案使用 `useTranslation()`，通过 `i18n-translate` 流程补齐七种语言。

## 9. 性能与风险

| 风险 | 处理 |
| --- | --- |
| Relay 热路径延迟 | 仅在启用且非免费请求上增加一次 Redis `HGET` 等价检查；无数据库查询，无 key 扫描。 |
| 并发请求超额 | 属于已确认软上限语义；达到或超过上限后阻止后续请求，并配合已有用户并发限制控制峰值。 |
| Redis 故障绕过限制 | Redis 已配置时失败关闭，不降级到本地内存。 |
| 结算后 Redis 写失败 | 记录安全告警；后续检查在 Redis 未恢复前返回 503，恢复后不伪造丢失用量。 |
| 多实例时区不一致 | 使用服务端本地时区并在部署说明中要求所有实例时区一致。 |
| 并发索引残留 | 查询时清理过期租约和空用户，索引自身带 TTL。 |
| 个人调整与在途请求竞态 | 调整覆盖当前已记录值；在途请求完成后继续按实际额度累加。 |
| 上游同步冲突 | 新逻辑集中在独立文件，原有文件仅增加字段、context、检查或累计调用。 |

## 10. 回滚

- 将 `user_daily_quota_limit` 配置为 `0` 可立即关闭额度检查，状态 key 等待 TTL 自动清理。
- 当前并发统计异常时可撤销用户索引扩展，不影响原租约 key 和限制行为。
- 删除新增管理 Dialog、API 和领域文件并撤销窄接入即可回滚；数据库新增列可保留，不影响旧版本。
