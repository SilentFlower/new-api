# 渠道按模型预算、按模型降级与池子已用金额调整（含 ai-fund 联动）

## Goal

在现有渠道周期策略（池子日/周预算 + 时间规则 + 超限降级）之上：

1. **按模型的池子预算（R1）**：除整体池子日/周上限外，可为指定模型（如 `gpt-6-astra`）单独设置池子日/周上限。整体与模型上限并行判定，任一耗尽即按现有开关拒绝（429）或降级。
2. **池子已用金额调整（R2）**：管理员可直接把池子（整体或某模型）当日/当周"已使用"金额设为目标值（含清零），与现有"调整个人当日/本周已用金额"同一交互模式。
3. **按模型的个人上限（R4）**：同一模型预算行可附带个人日/周上限，默认 0（不设），与渠道级、规则级个人限制并行、互不冲突。
4. **按模型的降级（R5）**：模型预算行可配置自己的降级目标（可同渠道换模型）；某模型额度耗尽而整体未耗尽时，降级到该模型配置的低价目标，按目标模型计费。

new-api 后端、new-api 管理前端、ai-fund worker 与前端同步支持。

## Background / 已确认事实

- 策略配置 `dto/channel_period_policy.go:4`：顶层只有 `pool_daily_quota_limit` / `pool_weekly_quota_limit`；规则内有 `pool_daily_quota_limit` / `pool_period_quota_limit`；无模型维度。
- 池子用量 `service/channel_period_usage.go:79-83`：Redis hash key 按 `channel_id` + 周期，字段 `user_id` 与 `__pool` 汇总、`__since` 统计起点；Lua 脚本 `channelPeriodUsageAddScript` 对第 3 个及之后的 key 同时累加 `__pool`。**未记录模型**。写入入口 `RecordChannelUserQuotaUsage(ctx, channelID, userID, quota)`（`service/channel_user_quota_usage.go:19`）；调用方 `RecordRelayChannelUserQuotaUsage` 持有 `relayInfo.OriginModelName`（`relay/common/relay_info.go:127`），异步任务 `service/task_billing.go:267` 持有 `task.OriginModelName`（`model/task.go:84`）。
- 判定 `service/channel_period_guard.go:31` `GetChannelPeriodStatus(ctx, channel, userID)` 产出 `ChannelPeriodMetric`（scope=user/pool，period=daily/weekly/custom）；`CheckChannelPeriodLimits` 对任一 `Enforced && Used >= Limit` 返回 `ChannelPeriodBlock` → 429；请求前入口 `CheckSelectedChannelPeriodLimits(c)`（`:219`）可从 `constant.ContextKeyOriginalModel` 取原始模型名。降级链路据 `channel_period_block` 触发（`controller/channel_limit_fallback.go`）。
- 个人已用金额调整现状：`PUT /api/channel/:id/user-daily-quota/:user_id {used_quota}`（`router/channel-router.go:47`，`controller/channel_user_limits.go:134`，`service/channel_user_daily_quota.go:211` → `store.set`），权限 `authz.ChannelOperate`，审计 `channel.user_daily_quota_set`；周同理。个人日周计数与池子计数是**不同 key**，互不影响。
- 策略写入 `controller/channel_period_policy.go:109` `bindChannelPeriodJSON` 做严格键集合比对（多/少字段均 400）；`service/channel_period_policy.go:259` `validateChannelPeriodStoredPolicy` 要求 `Normalize(config) DeepEqual config`。
- ai-fund：`worker/src/newapi_period_policy.js:64` 白名单归一化（新字段不显式加入即被丢弃）、`:119` 状态归一化只接受 scope/period 白名单；`worker/src/pool_period_limits.js:36` 浏览器入站 `assertPeriodPolicyFields` 严格键集合；个人已用调整 BFF `worker/src/pool_limits.js:757` `setPoolLimitUserDailyQuota`，路由 `worker/src/index.js:7420`；前端 `PoolPeriodPolicyEditor.vue`（池子日/周金额 + 规则）、`PoolLimitAdminModal.vue`（个人用量表格与调整）、`PoolLimitPanel.vue:160`（个人/号池分组指标）；合同 fixture `worker/src/fixtures/channel-period-contract.json` 被 `pool_limits.test.js` / `pool_period_limits.test.js` 引用。ai-fund 规范 `.trellis/spec/backend/newapi-pool-limits.md`。
- new-api 前端：`web/src/features/channels/period-types.ts`（zod `schema_version: z.literal(1)`）、`period-api.ts`、`components/dialogs/channel-period-policy-panel.tsx`、`channel-user-limits-dialog.tsx`（个人用量调整交互，`:473`）。new-api 规范 `.trellis/spec/backend/channel-period-budget-fallback.md`。

## Requirements

### R1 按模型的池子预算

- R1.1 策略配置新增 `model_budgets` 列表，每项 `{model, pool_daily_quota_limit, pool_weekly_quota_limit, created_at}`；额度语义与整体一致（`0` 不限，正数上限）。模型名去首尾空白、非空、精确唯一（区分大小写），最多 64 项。
- R1.2 匹配依据：客户端请求的**原始模型名**（`OriginModelName` / `ContextKeyOriginalModel`）精确匹配；不做通配、前缀或模型映射后的上游名。
- R1.3 判定：整体池子与命中模型的模型池子同时检查，任一 `used >= limit` 即超限，沿用现有 429 / 降级开关；降级原因携带模型名。
- R1.4 用量：命中 `model_budgets` 的模型按模型单独累计日/周用量（Redis 与内存都支持），整体池子累计不变。模型计数自该条目 `created_at` 起统计（`tracking_since` / `coverage` 语义与现有池子一致）。
- R1.5 状态输出：`ChannelPeriodMetric` 新增 `model` 字段（整体为空）；请求前检查只产出命中模型的指标并 `enforced`；管理端/本人状态展示全部模型预算指标。
- R1.6 首版不支持时间规则内的按模型上限。

### R2 池子已用金额调整

- R2.1 新增 `PUT /api/channel/:id/pool-daily-quota` 与 `PUT /api/channel/:id/pool-weekly-quota`，body `{used_quota: int>=0, model: string}`（`model` 空串 = 整体池子），权限 `authz.ChannelOperate`，审计 `channel.pool_daily_quota_set` / `channel.pool_weekly_quota_set`。
- R2.2 语义：把当前自然日/自然周的池子汇总计数设为目标值；后续消费在此基础上累加。整体与各模型计数相互独立，调整其一不影响其他；不改动个人日周计数，不改策略 revision。
- R2.3 `model` 非空时必须存在于当前策略 `model_budgets`，否则 400；`used_quota > MaxQuota` 400。
- R2.4 首版不支持时间规则整段计数的调整。

### R4 按模型的个人上限

- R4.1 `model_budgets` 每项增加 `user_daily_quota_limit` / `user_weekly_quota_limit`，`0` = 不设（默认）。
- R4.2 用量：命中模型的个人日/周用量按 `(channel, model, user)` 单独累计（与 R1.4 同一计数 key 的用户字段），渠道级个人日/周计数不变。
- R4.3 判定：产出 `scope=user, model=<name>, period=daily|weekly` 指标（仅上限 > 0 时产出）；耗尽即 429 或走 R5 模型降级。渠道级个人限、规则内个人限与个人特批照常独立生效。
- R4.4 首版不做按模型的个人特批与按模型的个人用量调整。

### R5 按模型的降级

- R5.1 `model_budgets` 每项增加 `fallback: {enabled, channel_id, model}`；`channel_id = 0` 表示同渠道换模型，`> 0` 表示指定渠道。
- R5.2 选择规则：超限指标带 `model` 且该模型 `fallback.enabled` → 使用该模型目标；否则若策略级 `fallback.enabled` → 使用策略级目标；都无 → 429。整体池子/渠道级个人指标在判定顺序中先于模型指标，因此"总额也耗尽"自然走策略级降级。
- R5.3 校验：启用时 `model` 非空；同渠道（`channel_id` 为 0 或等于本渠道，保存时归一为 0）目标模型必须不同于本模型；指定渠道必须存在。策略级降级仍不允许指向自身。
- R5.4 目标侧沿用现有一跳限制与预检：降级后用目标模型重跑周期预检（整体池子、目标模型自身预算与个人上限），目标也耗尽则 429，不二跳；目标必须通过分组/Token 模型权限校验；按目标模型价格结算；降级日志含原模型、目标渠道、目标模型与触发指标（含模型）。
- R5.5 目标候选接口返回本渠道及其模型供模型级降级选择；策略级选择器仍排除本渠道。

### R3 管理界面与 ai-fund 联动

- R3.1 new-api 周期策略面板：模型预算行的增删改（模型名、池子日/周、个人日/周、降级开关 + 目标渠道（含本渠道）+ 目标模型）；池子用量调整表单（周期、目标：整体/模型、金额）。
- R3.2 ai-fund worker：策略归一化与入站白名单增加 `model_budgets`（含个人上限与 `fallback`）；状态归一化增加 `metric.model` 并放宽指标数量上限；新增 `PUT /api/admin/pools/:poolId/pool-daily-quota` / `pool-weekly-quota`（body `{used_quota_display, model, expected_channel_id}`），沿用 `resolvePoolChannelForWrite` 防错渠、审计与缓存失效；本人降级提示按每条原因携带 `model` 与该原因对应的目标模型。
- R3.3 ai-fund 管理端：策略编辑器支持模型预算行（含个人上限与模型级降级选择）；号池用量调整表单；本人面板"我的额度"与"号池额度"显示模型指标（如 `今日 · gpt-6-astra`），降级提示显示触发模型与对应目标模型。
- R3.4 兼容：`schema_version` 保持 1；旧存储配置缺少 `model_budgets` 读取时视为空列表且不判为无效；两仓需同版本部署（旧 ai-fund 对新 new-api 保存策略会因严格键比对得到 400，不会静默丢字段）。

## Acceptance Criteria

- [ ] AC1 / R1.1-R1.3：整体池子日限 100、`gpt-6-astra` 日限 20；该模型累计 20 后其请求 429（或降级），其他模型仍可用直到整体 100；整体耗尽后所有模型均 429。周限同理。
- [ ] AC2 / R1.2 R1.4：按原始模型名累计；模型映射后的上游名不影响；Redis 与内存存储结果一致；新增模型预算后 `tracking_since` 为其 `created_at`，之前消费不计入。
- [ ] AC3 / R1.5：管理端统一状态与 ai-fund 本人状态返回带 `model` 的池子指标；请求前检查对未命中模型不产生额外 enforced 指标。
- [ ] AC4 / R2：设置整体池子当日已用为 0 后新请求立即可用；设置为 ≥ 上限后新请求立即 429；模型池子调整不影响整体与个人计数；策略 revision 不变；审计记录 before/after。
- [ ] AC5 / R2.3：`model` 不在预算列表、`used_quota` 越界 → 400。
- [ ] AC6 / R3：new-api 与 ai-fund 管理端均可配置模型预算与调整池子用量并回读一致；ai-fund 本人面板显示模型号池指标；两仓现有测试全绿。
- [ ] AC7 / R3.4：旧存储策略（无 `model_budgets`）读取、预览、保存均正常；策略写入对未知字段仍 400。
- [ ] AC8 / R4：`gpt-6-astra` 个人日限 50 → 该用户该模型累计 50 后 429/降级，其他用户与其他模型不受影响；渠道级个人日限仍独立按渠道总用量生效；个人上限为 0 时不产生该指标。
- [ ] AC9 / R5：模型池子日限 500、整体日限 600、模型降级目标同渠道 `gpt-6-mini`：模型到 500 且整体未到 → 请求改用 `gpt-6-mini` 成功并按其价格结算，日志含原模型/目标模型/触发模型；整体到 600 → 走策略级降级或 429，不用模型目标；`gpt-6-mini` 自身预算也耗尽 → 429 不二跳；模型未配降级但策略级已启用 → 走策略级目标。
- [ ] AC10 / R5.3：保存同渠道目标模型等于本模型、启用但模型为空、指定渠道不存在 → 400；`channel_id` 等于本渠道保存后回读为 0。
- [ ] AC11 / R3：ai-fund 本人面板降级提示显示"`gpt-6-astra` 今日额度已用完 → `gpt-6-mini`"形式的模型级原因。

## Out of Scope

- 时间规则内的按模型上限、规则整段计数调整。
- 模型名通配、前缀或分组匹配。
- 按模型的个人特批与按模型的个人用量调整。
- 池子上限的临时特批（改上限而非改已用）。
- 多跳降级、降级链或按价格自动挑选目标。
