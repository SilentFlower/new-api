# Design — 渠道按模型预算、按模型降级与池子已用金额调整

## 1. 边界

- 策略正文（`model_budgets`）随现有 `PUT /period-policy` 整份替换，走 revision 乐观锁；用量调整是独立运行时操作，不触碰策略与 revision。
- 模型维度的池子与个人计数共用同一组模型 key；渠道级个人计数、规则计数不变。
- 降级目标选择从"策略级单目标"扩展为"模型级优先、策略级兜底"，一跳限制与目标侧预检不变。
- 两仓契约变更：`ChannelPeriodPolicyConfig.model_budgets`（含个人上限与 `fallback`）、`ChannelPeriodMetric.model`、两个新 PUT 端点、目标候选含本渠道、本人降级提示按原因携带目标。

## 2. 数据契约

### 2.1 策略配置（`dto/channel_period_policy.go`）

```go
type ChannelPeriodModelBudget struct {
	Model                string               `json:"model"`
	PoolDailyQuotaLimit  int                  `json:"pool_daily_quota_limit"`
	PoolWeeklyQuotaLimit int                  `json:"pool_weekly_quota_limit"`
	UserDailyQuotaLimit  int                  `json:"user_daily_quota_limit"`
	UserWeeklyQuotaLimit int                  `json:"user_weekly_quota_limit"`
	Fallback             ChannelLimitFallback `json:"fallback"`
	CreatedAt            int64                `json:"created_at"`
}
// ChannelPeriodPolicyConfig 增加
ModelBudgets []ChannelPeriodModelBudget `json:"model_budgets"`
```

- 无 `omitempty`：`bindChannelPeriodJSON` 的严格键比对要求客户端总是携带该键（空数组）。
- 兼容：`GetChannelPeriodPolicy` 在 `validateChannelPeriodStoredPolicy` 之前，若 `ModelBudgets == nil` 置为空切片（`Rules` 已有同样约定）；`NormalizeChannelPeriodConfig` 输出非 nil 切片，保证 DeepEqual 通过。`SchemaVersion` 保持 1。
- 归一化规则（`service/channel_period_rules.go:NormalizeChannelPeriodConfig`）：`Model` TrimSpace 非空、`len<=255`、精确唯一；四项额度 `0..MaxQuota`；`len<=64`；`CreatedAt`：previous 中同名条目存在则沿用其 `created_at`，否则 `now.Unix()`；客户端传入的 `created_at` 不可信，一律按上述重算。`Fallback.Model` TrimSpace，`ChannelID >= 0`，启用时 `Model` 非空且 `ChannelID == 0` 时目标模型 ≠ 本模型。输出按输入顺序保留。
- `SaveChannelPeriodPolicy`（持有 `channelID`）追加：模型级 `fallback.channel_id == channelID` 归一为 0 后再做上述同渠道校验；`channel_id > 0` 时目标渠道必须存在。策略级 `fallback` 仍拒绝指向自身。
- `validateChannelPeriodStoredPolicy`：Revision==0 时 `len(ModelBudgets)>0` 视为无效（与其余字段一致）。

### 2.2 指标（`dto.ChannelPeriodMetric`）

新增 `Model string json:"model,omitempty"`。渠道级指标为空；模型指标 `Scope="pool"|"user"`, `Period="daily"|"weekly"`, `Model="<name>"`，**仅在对应上限 > 0 时产出**（默认 0 的个人上限不产生指标，避免界面噪音）。`ChannelPeriodBlock.Metric.Model` 随之进入 429 上下文与降级选择。

### 2.3 用量调整请求

`PUT /api/channel/:id/pool-daily-quota`、`PUT /api/channel/:id/pool-weekly-quota`
```json
{"used_quota": 0, "model": ""}
```
响应 `{channel_id, period, model, used_quota}`。

## 3. 计数存储

- Key：`channel_pool_model_daily_quota:{cid}:{model}:{YYYY-MM-DD}`、`channel_pool_model_weekly_quota:{cid}:{model}:{周一日期}`；hash 布局与现有池子 key 相同（`user_id` 字段、`__pool`、`__since`），以便复用 `channelPeriodUsageAddScript`、`getChannelPeriodUsage` 与内存 bucket。
- `channelPeriodCounters(channelID, now, active)` 增加参数 `budget *dto.ChannelPeriodModelBudget`（记录/请求前检查传命中项；状态展示按每个预算逐个生成），在 index 0/1 之后、规则之前插入模型日/周两项，`createdAt = budget.CreatedAt`。为避免打乱 `GetChannelPeriodStatus` 里 `i < 2` / `active[i-2]` 的索引约定，模型计数**单独调用** `channelPeriodCounters` 的一个变体 `channelPeriodModelCounters(channelID, now, budget)`，不混入现有切片。
- 记录：`recordChannelPeriodUsage(ctx, channelID, userID, quota, modelName)` 从策略里查找 `modelName` 命中项，命中则把两个模型 key 追加进 Lua KEYS（位置 > 2 会同时累加用户字段、`__pool` 与 `__since`——用户字段即 R4 的个人按模型用量）与内存循环；未命中不产生 key。`RecordChannelUserQuotaUsage` / `RecordRelayChannelUserQuotaUsage` 增加 `modelName` 参数；relay 传 `relayInfo.OriginModelName`，任务传 `task.OriginModelName`，测试与旧调用传 `""`。
- 调整：`SetChannelPoolQuotaUsage(ctx, channelID, period, model, usedQuota)`：解析策略、校验 `model`、构造对应 counter（整体：现有 `channel_pool_*_quota` key；模型：模型 key），Redis 用单个 Lua 脚本 `HSET __pool` + `HSETNX __since` + `EXPIREAT`（与写脚本同过期规则 `end+86400`），内存直接写 bucket。只改 `__pool`，用户字段保留（文档化：调整后 `__pool` 是独立汇总）。

## 4. 判定与状态

- `GetChannelPeriodStatus(ctx, channel, userID)` 拆为内部 `getChannelPeriodStatus(ctx, channel, userID, modelName string)`：
  - 现有整体/规则指标逻辑不变。
  - 之后遍历 `policy.Config.ModelBudgets`：`modelName != ""` 时仅处理精确匹配项；为空时处理全部。每项读取模型日/周计数（`userUsed`, `poolUsed`, `since`），按上限 > 0 产出最多四条指标：`pool/daily`、`pool/weekly`、`user/daily`、`user/weekly`，`Limit=BaseLimit=对应额度`，`Source={Kind:"default"}`，`Enforced=true`，`Coverage`/`TrackingSince` 与池子一致（含 gap 判断）。模型指标追加在所有渠道级/规则指标之后，保证 `CheckChannelPeriodLimits` 的首个超限指标是更宽的原因。
  - 导出：`GetChannelPeriodStatus` 保持签名（`modelName=""`，供管理端/门户展示全部）；新增 `GetChannelPeriodStatusForModel(ctx, channel, userID, modelName)` 供请求前检查。
- `CheckChannelPeriodLimits(ctx, channel, userID)` 增加 `modelName` 参数；`CheckSelectedChannelPeriodLimits(c)` 取 `common.GetContextKeyString(c, constant.ContextKeyOriginalModel)` 传入。既有 `Blocked` 汇总与 `for metric` 拦截逻辑不变——模型指标只是多出的两条。
- 降级选择（`controller/channel_limit_fallback.go:prepareChannelLimitFallback`）：读取 `channel_period_block` 后调用 `service.SelectChannelLimitFallback(policy.Config, block.Metric) (dto.ChannelLimitFallback, bool)`：指标 `Model` 命中且该预算 `fallback.enabled` → 返回其目标（`ChannelID == 0` 时替换为源渠道 ID）；否则策略级 `fallback.enabled` → 策略级目标；都无 → `(_, false)` 返回原 429。现有 `!policy.Config.Fallback.Enabled` 门禁改为 `!selected`。
- `ResolveChannelLimitFallbackTarget` 不变（同渠道时 `fallback.ChannelID` 已被替换为源渠道，`IsChannelEnabledForGroupModel`、启用状态、Advanced Custom 路径校验照常）。`ChannelLimitFallbackInfo` 增加 `Model`（触发指标模型）；`TargetChannelID` 可等于 `SourceChannelID`。
- 同渠道降级后 `SetupContextForSelectedChannel(c, target, targetModel)` 重设 `original_model`，目标侧 `preflightChannelPeriodLimits` 以目标模型重跑：整体池子、目标模型自身预算与个人上限均生效；`channel_limit_fallback_used` 仍保证一跳。
- `GetChannelPeriodPolicyTargets` 返回列表包含本渠道；前端策略级选择器自行排除本渠道，模型级选择器把本渠道显示为"本渠道"。

## 5. 接口层

### new-api
- `router/channel-router.go`：两条 PUT，`authz.ChannelOperate`；`router/channel_router_test.go` 增加权限断言。
- `controller/channel_period_policy.go`：`SetChannelPoolDailyQuota` / `SetChannelPoolWeeklyQuota`，复用 `getChannelForUserLimit`、`bindChannelPeriodJSON`（严格键）、`recordManageAudit`，错误走 `respondChannelPeriodPolicyError`（`ErrInvalidChannelPeriodPolicy` → 400）。
- `web/src/features/channels/period-types.ts`：`channelPeriodModelBudgetSchema`、config 增加 `model_budgets: z.array(...).max(64)`、metric 增加 `model?: string`。
- `period-api.ts`：`setChannelPoolQuotaUsage(channelId, period, body)`。
- `channel-period-policy-panel.tsx`：模型预算列表（模型名、池子日/周、个人日/周、降级开关 + 目标渠道（含 "This channel"）+ 目标模型，增/删），保存随策略整体提交；策略级降级选择器排除本渠道；下方"Pool usage adjustment"表单（period select、target select：Pool / 各模型、金额），成功后失效 `['channels', id, 'period-policy']` 与用户限制状态查询。i18n 键：`Model budgets`、`Model`、`Add model budget`、`User weekly amount`、`Model fallback`、`This channel`、`Pool usage adjustment`、`Overall pool`、`Target`、`Pool usage updated`、`Failed to adjust pool usage`，六语种同步 `bun run i18n:sync`。
- 用户限制统一状态展示（`channel-user-limits-dialog.tsx` 中周期指标区）：指标标签带模型名。

### ai-fund worker
- `newapi_period_policy.js`：`normalizePeriodPolicyConfig` 增加 `model_budgets`（`Array.isArray`、`<=64`、每项 `{model 非空, pool_daily_quota_limit, pool_weekly_quota_limit, user_daily_quota_limit, user_weekly_quota_limit, fallback:{enabled,channel_id,model}, created_at}`，整数用 `periodInteger`）；`normalizePeriodLimitStatus` metric 增加 `model: typeof metric.model === 'string' ? metric.model : ''`，指标数量上限由 132 提高到 388（132 + 64×4）。
- `pool_period_limits.js`：`poolPeriodConfigToInternal` / `poolPeriodViewToDisplay` 处理 `model_budgets`（展示字段 `*_display`）；新增 `setPoolPeriodPoolUsage(env, poolId, period, model, usedQuotaDisplay, expectedChannelId, actorUserId)`，流程同 `setPoolLimitUserDailyQuota`（`resolvePoolChannelForWrite` → 读 before → 调 NewAPI → 审计 `pool.pool_<period>_quota.update` → `clearPoolLimitCache`）。
- `newapi_client.js`：`setNewApiChannelPoolQuotaUsage(env, channelId, period, model, usedQuota)`。
- `pool_limits.js` 本人降级提示：`reasons[]` 每项 `{scope, period, model, target_model}`，`target_model` 按 R5.2 从同版策略解析（模型级优先、策略级兜底）；至少一条原因有目标才视为可降级，否则不显示提示。`period.fallback.model` 保留为策略级目标以兼容旧前端字段。
- `index.js`：`PUT /api/admin/pools/:poolId/pool-(daily|weekly)-quota`。
- fixture `channel-period-contract.json`：policy/preview/status 增加 `model_budgets: []` 与含模型的示例指标。

### ai-fund frontend
- `api/index.js`：`updateAdminPoolUsage(poolId, period, { used_quota_display, model, expected_channel_id })`。
- `PoolPeriodPolicyEditor.vue`：模型预算区（模型名、池子每日/每周、个人每日/每周、降级开关 + 目标渠道（本渠道/其他）+ 目标模型，增删），随策略提交；目标选项来自 `period-policy/targets`（含本渠道）；`metricNames` 显示模型。
- 新组件 `PoolPeriodUsageEditor.vue`：周期（今日/本周）、目标（整体/模型下拉，来自当前策略 `model_budgets`）、金额；挂载于 `PoolLimitAdminModal.vue` 周期策略页签下方。
- `PoolLimitPanel.vue`："我的额度"与"号池额度"两组都可出现模型指标，标签 `今日 · <model>`；`fallbackNotice` 按原因显示 `<model> 今日额度已用完 → <target_model>`。

## 6. 取舍

- 复用现有 hash 布局与 Lua 脚本而非新脚本：模型 key 也会累计 `user_id` 字段（冗余但代价小），换来记录/读取/内存三处零新逻辑。
- 请求前检查只产出命中模型指标：避免非该模型请求被无关模型的 `Blocked` 影响（`Blocked` 是 OR 汇总）。
- 模型级降级"模型级优先、策略级兜底"：模型没配降级时仍能用渠道级目标，管理员不必为每个模型重复配置；首版不单独提供"仅此模型不降级"开关。
- 允许同渠道换模型：模型预算耗尽而渠道未耗尽是本需求的核心场景；策略级降级仍禁止自指，因为策略级触发意味着渠道整体已耗尽。
- 个人按模型用量复用模型 key 的用户字段：R1 已在写，不新增 key；代价是模型 key 的哈希字段数随活跃用户增长，与现有池子 key 同量级。
- `schema_version` 不升：升级会迫使 new-api web zod 与 ai-fund 归一化同时改字面量，但对旧客户端的保护效果与严格键比对相同；保持 1 减少改动面。
- 不做通配匹配：首版需求是单模型，通配引入优先级与重叠语义。

## 7. 回滚

- 代码回滚后：旧代码读取含 `model_budgets` 的存储配置——`common.Unmarshal` 忽略未知字段，`validateChannelPeriodStoredPolicy` 的 DeepEqual 不含该字段 → 正常；模型 key 在 Redis 自然过期。两仓需一起回滚，否则 ai-fund 保存策略 400。
