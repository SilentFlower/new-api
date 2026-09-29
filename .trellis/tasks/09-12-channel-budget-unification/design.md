# Design — 渠道限额统一为预算列表

## 1. 边界与阶段

- 阶段一（budget-core）只改 `service/` 内核与 `controller/channel_limit_fallback.go` 的选择逻辑：v1 配置在读取后转换为内部 `budgetPlan`，所有判定、计数、状态输出、降级选择基于 `budgetPlan`。DTO、路由、前端、ai-fund 不变。
- 阶段二（budget-v2-contract）把 `budgetPlan` 的形状暴露为 `schema_version=2` 契约，新增统一覆盖表与按行用量接口，删除旧接口与旧列读写，执行启动迁移，改造 new-api 前端。
- 阶段三（budget-ai-fund）ai-fund worker 与前端迁移到 v2。

## 2. 内部模型（阶段一引入，阶段二直接序列化）

```go
// service/channel_budget_plan.go
type channelBudgetSchedule struct { ID, Name string; Enabled bool; Kind string; StartAt, EndAt int64; StartWeekday, EndWeekday int; StartTime, EndTime string; CreatedAt int64 }
type channelBudgetRow struct {
	ID, Name string; Enabled bool
	Scope string      // user | pool
	Window string     // daily | weekly | occurrence
	ScheduleID string // occurrence 必填；daily/weekly 可选
	Models []string   // 空 = 全部；已排序去重
	Limit int64
	OnExceed dto.ChannelBudgetAction // {Mode: inherit|reject|fallback, ChannelID, Model}
	CreatedAt int64
}
type channelBudgetPlan struct { Revision int; DefaultOnExceed dto.ChannelBudgetAction; Schedules []channelBudgetSchedule; Rows []channelBudgetRow }
```

- 阶段一由 `buildChannelBudgetPlan(policy dto.ChannelPeriodPolicyView, channel *model.Channel)` 从 v1 + 渠道列构造；阶段二由 v2 配置直接构造，同一函数签名，渠道列参数删除。
- 派生 id（阶段一构造、阶段二迁移都使用，保证同一行在两阶段身份一致）：`legacy-user-daily`、`legacy-user-weekly`、`legacy-pool-daily`、`legacy-pool-weekly`、`rule-<ruleID>-user-daily`、`rule-<ruleID>-pool-daily`、`rule-<ruleID>-user-occurrence`、`rule-<ruleID>-pool-occurrence`；时段 id 沿用规则 id。新建行由服务端分配 UUID。

## 3. 计数身份与 key

- 身份 `identity = (window, scheduleID, models)`；`identityHash = 前 12 位 hex(sha1(window + "|" + scheduleID + "|" + strings.Join(models, ",")))`。
- 每个身份在某一时刻对应一个窗口 `[start, end)`：daily/weekly 为自然日/周（带 schedule 时同样是自然日/周，schedule 只决定是否生效）；occurrence 为时段当前 occurrence。
- key 映射 `budgetCounterKeys(channelID, identity, window) (userKey, poolKey string)`：
  - `(daily, "", [])` → userKey `channel_user_daily_quota:{cid}:{date}`，poolKey `channel_pool_daily_quota:{cid}:{date}`；weekly 同理。个人行读 userKey 的用户字段，池子行读 poolKey 的 `__pool`；写入时两把 key 都累加用户字段，poolKey 另累加 `__pool`（与 v1 完全一致，旧 `list` 解析不受影响）。
  - `(occurrence, ruleID, [])` → 两者同为 `channel_period_quota:{cid}:{ruleID}:{occStart}`。
  - `(daily|weekly, ruleID, [])`（规则内的日限）→ 与无时段身份共用同一 key：v1 中规则日限只是改上限，不换计数。
  - 其余（含任何 `models` 非空）→ 两者同为 `channel_budget:{cid}:{identityHash}:{windowStart}`。
- Lua 脚本改为逐条目输入：`KEYS[i]` 与 `ARGV = [userID, delta, now, (expireAt_i, since_i, poolFlag_i)...]`，`poolFlag_i=1` 时维护 `__pool` 与 `__since`。溢出预检与 HINCRBY 逻辑不变。内存路径同样按条目循环，锁顺序：`channelPeriodUsageMemory` → 旧日 → 旧周（保持现状）。
- 写入路径 `recordChannelBudgetUsage(ctx, channelID, userID, quota, modelName)`（对外入口为新增的 `RecordChannelUserModelQuotaUsage`，旧 `RecordChannelUserQuotaUsage` 签名保留并委托，避免改动上游调用方）：解析 plan → 取当前命中身份集合（`enabled=false` 的行仍计数，与 v1 停用规则一致；时段未活跃的 daily/weekly 行不额外产生身份，因为其身份与无时段行相同）→ 生成条目 → 一次脚本。relay 入口传 `OriginModelName`，任务结算传 `task.Properties.OriginModelName`，其余调用方经旧签名传 `""`。
- `tracking_since = max(windowStart, row.CreatedAt, common.StartTime)`，与 `__since`（HSETNX）取较早者用于 coverage 判断，沿用 `channel_quota_tracking` 缺口逻辑。修改行的 `models` 等价于换身份，从修改时刻起算，界面提示。

## 4. 判定与状态

```go
func evaluateChannelBudgets(ctx, plan, channel, userID int, modelName string, now time.Time) (dto.ChannelPeriodStatus, error)
```

1. 计算每个时段是否活跃及下一切换点（复用 `channelPeriodOccurrence`）。
2. 过滤命中行：`enabled` 且（`modelName==""` 或匹配）且（无时段或活跃）。`modelName==""` 用于管理端/本人展示全部行；请求前检查传原始模型名。
3. 分组：key = `(scope, window, models)`。组内取生效行：个人覆盖 > date_range > weekly > 无时段；未生效行仍输出指标但 `enforced=false`（保持现有"遮盖期间仍显示"的行为）。
4. 读计数：按身份去重后批量 HMGET（Redis pipeline）或内存读取。
5. 产出 `ChannelPeriodMetric`：`Scope`、`Period`（daily|weekly|occurrence）、`Limit`、`BaseLimit`、`OverrideLimit`、`Used`、`Remaining`、`ResetAt`、`TrackingSince`、`Coverage`、`Enforced`、`Source{Kind, RuleID→ScheduleID, RuleName→ScheduleName, StartAt, EndAt, ExpiresAt}`、新增 `BudgetID`、`BudgetName`、`Models`。阶段一不新增任何 JSON 字段，`Period` 仍输出 `custom`，行信息只经内部 `ChannelPeriodBlock.row` 供降级选择；阶段二随 v2 一并新增字段并改为 `occurrence`。
6. `Blocked` = 任一 `enforced && limit>0 && used>=limit`；`CheckChannelPeriodLimits` 返回第一条超限行的 `ChannelPeriodBlock{Metric, Row}`。
7. 错误码：`Row.ID` 前缀 `legacy-user-` 或（`scope=user && models 为空 && scheduleID==""`）→ 旧日/周错误码；其余 `channel_period_quota_exceeded`。
8. 阶段一 `CheckSelectedChannelPeriodLimits` 保留 revision=0 早返回，并继续把渠道级个人日/周指标的上限写回 `ContextKeyChannelUserDailyQuotaLimit/Weekly`（转 `int`）；`relay/channel_user_daily_quota.go` / `weekly` 的独立检查与 `controller/channel_limit_fallback.go` 降级预检的直接调用保留，因为降级目标渠道可能没有策略，此时它们是唯一兜底。阶段二迁移后所有渠道都有 v2 策略，再删除这些检查、早返回分支、`relaycommon.RelayInfo.ChannelUserDailyQuotaLimit/Weekly` 字段与 `relay/vision_assist.go` 中的上下文键复制。

## 5. 降级选择

- `selectChannelBudgetFallback(plan, block) (dto.ChannelBudgetAction, bool)`：`block.Row.OnExceed.Mode`：`reject` → false；`fallback` → 该行动作（`ChannelID==0` 时替换为源渠道 id）；`inherit` → `plan.DefaultOnExceed`（`fallback` 才返回 true）。
- `prepareChannelLimitFallback` 用返回的动作替代 `policy.Config.Fallback`；`ChannelLimitFallbackInfo` 增加 `BudgetID`、`Models`；`TargetChannelID` 可等于源渠道。`ResolveChannelLimitFallbackTarget` 不变。同渠道时 `SetupContextForSelectedChannel(c, source, targetModel)` 重设 `original_model`，目标侧预检以目标模型重跑 `evaluateChannelBudgets`。
- 校验：行级 `fallback` 且 `ChannelID==0` 要求 `models` 非空且 `Model ∉ models`；`ChannelID>0` 要求渠道存在；策略级禁止指向本渠道（保存时 `ChannelID==channelID` → 400；行级等于本渠道保存时归一为 0）。

## 6. v2 契约（阶段二）

```json
{
  "schema_version": 2,
  "default_on_exceed": {"mode": "reject", "channel_id": 0, "model": ""},
  "schedules": [{"id": "…", "name": "国庆", "enabled": true, "kind": "date_range", "start_local": "2031-10-01T00:00", "end_local": "2031-10-08T00:00", "start_at": 0, "end_at": 0, "start_weekday": 0, "end_weekday": 0, "start_time": "", "end_time": "", "created_at": 0}],
  "budgets": [
    {"id": "legacy-pool-daily", "name": "池子 · 今日", "enabled": true, "scope": "pool", "window": "daily", "schedule_id": "", "models": [], "limit": 2250000000, "on_exceed": {"mode": "inherit", "channel_id": 0, "model": ""}, "created_at": 0},
    {"id": "…", "name": "gpt-6-astra 今日", "enabled": true, "scope": "pool", "window": "daily", "schedule_id": "", "models": ["gpt-6-astra"], "limit": 10000000, "on_exceed": {"mode": "fallback", "channel_id": 0, "model": "gpt-6-mini"}, "created_at": 0}
  ]
}
```

- DTO：`dto.ChannelBudgetPolicyConfig`、`dto.ChannelBudgetSchedule`、`dto.ChannelBudgetRow`、`dto.ChannelBudgetAction`；`ChannelPeriodPolicyInput/View` 的 `Config` 类型替换；v1 结构体删除。`bindChannelPeriodJSON` 严格键集合按 v2 生成。
- 归一化 `NormalizeChannelBudgetConfig(config, previous, now)`：id/created_at 由服务端建立并沿用；名称去空白；`models` 去空白排序去重；行 ≤ 128、时段 ≤ 64；`limit` `0..MaxPeriodQuota`；时段校验复用现有解析；删除的时段/行转停用（保持计数身份）；同 `(scope, window, models)` 键内同类型时段相交拒绝；`occurrence` 必须引用存在时段；降级校验见 §5。
- `validateChannelBudgetStoredPolicy`：`Normalize(config) DeepEqual config`，`schema_version` 必须为 2（迁移后不再出现 v1）。
- 预览 `PreviewChannelBudgetPolicy` 返回 `{config, revision, timezone, now, next_change_at, rows: [{budget_id, active, enforced, source}]}`，替代 `limits/sources` 字典。
- 状态 `ChannelPeriodStatus` 结构不变，指标按 §4 输出，`period` 用 `occurrence`。

## 7. 覆盖与用量（阶段二）

- 表 `channel_user_budget_overrides`：`Id`、`ChannelId`、`UserId`、`BudgetId varchar(64)`、`Limit int64`、`ExpiresAt bigint(index)`、`UpdatedBy`、`CreatedAt`、`UpdatedAt`；唯一 `(channel_id, user_id, budget_id)`；加入普通与快速迁移列表。读取带 30s 本地缓存（复用现有覆盖缓存模式，`revision>0` 严格读取的分支删除，统一严格读取 + 缓存失效发布）。
- `channel_user_limit_overrides` 只保留 `UserConcurrencyLimit`、`ExpiresAt`；日/周字段 Go 侧 `json:"-"`、不再读写。
- 接口（`router/channel-router.go`，`authz.ChannelOperate` 写 / `ChannelRead` 读）：
  - `GET /api/channel/:id/budgets/:budget_id/usage?scope=user&page&page_size&keyword`：当前窗口用户字段列表（Redis HSCAN / 内存 map），返回 `{items:[{user_id, username, display_name, used_quota}], total, window:{start,end}}`；`scope=pool` 返回 `{used_quota, tracking_since}`。
  - `PUT /api/channel/:id/budgets/:budget_id/usage` `{scope, user_id, used_quota}`：Redis 单脚本 `HSET`（0 时 `HDEL`）+ `HSETNX __since` + `EXPIREAT`；池子只改 `__pool`。审计 `channel.budget_usage_set`。
  - `PUT/DELETE /api/channel/:id/budgets/:budget_id/user-overrides/:user_id` `{limit, expires_at}`；`GET /api/channel/:id/budget-user-overrides?page&page_size`。审计 `channel.budget_user_override_set/delete`。
  - 删除：`user-daily-quota*`、`user-weekly-quota*`、`period-rules/*`。`user-limit-overrides` 输入只含并发；`user-limit-status/:user_id` 返回 `{concurrency:{base,override,effective,expires_at}, period_limits}`。
- 前端 `period-api.ts` 增加对应函数；`channel-user-limits-dialog.tsx` 的日/周页签改为"按行"选择器 + 用量表；`channel-period-override-editor.tsx` 改为按行；渠道抽屉删除两字段文件与类型/默认值。

## 8. 迁移（阶段二）

- `model/channel_budget_migrate.go: migrateChannelBudgetsV2()`，在 `migrateDB()` 与 `migrateDBFast()` 完成后调用；`SQL_DSN` 与 `LOG_SQL_DSN` 均只涉主库。
- 逐渠道（分页 500）：读 `channel_period_policies`；若无记录或 `config.schema_version==1`，构造 v2：v1 转换（§2 派生 id）+ 渠道列 `user_daily/weekly_quota_limit > 0` → `legacy-user-daily/weekly` 行；`ReplaceChannelPeriodPolicy` CAS 写入，冲突则跳过（下次启动重试）。
- 覆盖迁移：`channel_user_limit_overrides` 中 `user_daily/weekly_quota_limit > 0` → upsert `channel_user_budget_overrides(budget_id=legacy-user-daily/weekly, limit, expires_at)`；`channel_user_period_overrides` → upsert `(budget_id=rule-<rule_id>-user-occurrence, limit=user_period_quota_limit, expires_at)`。已过期记录跳过。
- 幂等：v2 策略跳过；upsert 唯一键；单渠道失败 `common.SysError` 并继续。写入与读取全部 GORM，无原生 SQL。
- 回滚：迁移前备份三表；回滚 = 回退代码 + 恢复三表。旧列在新代码下不再更新，因此恢复后即迁移前状态。

## 9. ai-fund（阶段三）

- `newapi_period_policy.js`：v2 白名单（`default_on_exceed`、`schedules[]`、`budgets[]`、指标新增字段）；指标数量上限 = 128 + 64×2 冗余取 400。
- `pool_period_limits.js`：`poolPeriodConfigToInternal/ToDisplay` 按行换算 `limit_display`；新增按行用量/覆盖 BFF；`pool_limits.js` 删除日/周旧列写入，`PUT /pools/:poolId/limits` 只保留并发。
- `index.js`：`GET/PUT /api/admin/pools/:poolId/budgets/:budgetId/usage`、`PUT/DELETE /api/admin/pools/:poolId/budgets/:budgetId/user-overrides/:userId`、`GET /api/admin/pools/:poolId/budget-user-overrides`；删除 `user-daily-quota`、`user-weekly-quota` 路由。
- 前端：`PoolPeriodPolicyEditor.vue` 预算表 + 时段列表；`PoolLimitAdminModal.vue` 按行用量/覆盖；`PoolLimitPanel.vue` 标签含模型/时段，降级提示 `<models> 今日额度已用完 → <target>`。
- fixture 从 new-api `controller/channel_period_policy_test.go` 实际响应生成。

## 10. 取舍

- 身份共用（个人行与池子行同 key）而非每行独立 key：与 v1 一致、迁移零风险、Redis 字段数不翻倍；代价是"个人行"和"池子行"必须同 `(window, schedule, models)` 才共用，界面按此分组展示即可。
- 规则内日限与无时段日限共用计数：v1 语义即"改上限不换计数"，保持不变。
- 迁移一次性完成而非懒转换：避免废弃列被无限期读取，回滚路径明确。
- `on_exceed.mode=inherit` 而非行级布尔：让"大多数行用策略默认动作、个别模型行同渠道换模型"不需要重复配置。
- `period-policy` URL 保留，仅 body 升 v2：减少路由与权限表改动，ai-fund 只改 body 处理。
- 不引入 monthly：v2 枚举后续追加即可，不影响契约形状。

## 11. 兼容与回滚

- 阶段一可单独上线与回退：计数 key 与契约不变。
- 阶段二与 ai-fund 必须同版本部署；new-api 先上（迁移在启动时完成），ai-fund 紧随。回退按 §8。
- 旧错误码保留；`period=custom` → `occurrence` 是唯一的状态字段取值变化，ai-fund 归一化同步。
