# Brief — 渠道按模型预算、按模型降级与池子已用金额调整（含 ai-fund 联动）

## Goal

- 在现有渠道周期策略上，为指定模型单独设置池子与个人的日/周上限（与渠道级上限并行判定），支持按模型配置降级目标（可同渠道换更便宜的模型），并允许管理员直接把池子（整体或某模型）当日/当周"已使用"金额设为目标值；new-api 与 ai-fund 两仓同步。

## Scope

- 策略配置新增 `model_budgets`，每行：模型名、池子日/周上限、个人日/周上限（默认 0 = 不设）、`fallback {enabled, channel_id, model}`（`channel_id = 0` 同渠道）、`created_at`。按客户端**原始模型名精确匹配**。
- 命中模型按 `(channel, model)` 单独累计日/周用量（Redis + 内存），同一 key 的用户字段即个人按模型用量；渠道级池子/个人计数不变。整体与模型上限任一耗尽即 429 或降级。
- 判定顺序：渠道级/规则指标在前，模型级池子与个人指标在后（仅上限 > 0 时产出）；请求前检查只产出命中模型的指标，管理端与本人状态展示全部模型指标。
- 降级选择"模型级优先、策略级兜底"：超限指标带模型且该模型配了降级 → 用其目标（可同渠道）；否则策略级降级；都无 → 429。目标侧以目标模型重跑预检，仍一跳，按目标模型计费；策略级降级仍禁止自指。
- 新增 `PUT /api/channel/:id/pool-daily-quota` / `pool-weekly-quota`，body `{used_quota, model}`（空 model = 整体），直接设置当前周期池子汇总计数；整体/各模型/个人计数相互独立，不改策略 revision，审计 before/after。
- new-api 前端：周期策略面板模型预算行（四项额度 + 模型级降级选择器，目标渠道含"本渠道"）与池子用量调整表单；i18n 六语种同步。
- ai-fund worker：策略/状态归一化与入站白名单增加新字段（指标上限提到 388）；目标候选含本渠道；新增 `PUT /api/admin/pools/:poolId/pool-(daily|weekly)-quota` BFF；本人降级提示每条原因带 `model` 与 `target_model`；合同 fixture 与规范文档更新。
- ai-fund 前端：策略编辑器模型预算行（含个人上限与模型级降级）；新增号池用量调整组件；本人面板两组显示模型指标，降级提示显示 `<model> → <target_model>`。

## Non-Goals

- 时间规则内的按模型上限、规则整段计数的调整。
- 模型名通配、前缀或分组匹配。
- 按模型的个人特批与按模型的个人用量调整。
- 池子上限的临时特批（改上限而非改已用）。
- 多跳降级、降级链或按价格自动挑选目标。

## Key Decisions

- R2 是"直接设置已使用金额"（镜像现有个人 `user-daily-quota` 调整），不是临时改上限；无到期概念。
- 匹配依据为原始模型名精确匹配，不用模型映射后的上游名，与日志、Ability 口径一致。
- 模型计数复用现有 hash 布局与 Lua 累加脚本（key `channel_pool_model_{daily,weekly}_quota:{cid}:{model}:{date}`），个人按模型用量直接读同一 key 的用户字段，不新增 key；用量调整只改 `__pool`。
- 模型级指标仅在上限 > 0 时产出，默认 0 的个人上限不产生指标、不影响界面。
- 模型级降级允许同渠道换模型（核心场景），模型未配降级时兜底到策略级目标；`fallback.channel_id` 等于本渠道保存时归一为 0。
- `schema_version` 保持 1；`model_budgets` 不加 `omitempty`（严格键比对要求客户端总是携带空数组）；旧存储配置读取时 nil→空切片。两仓需同版本部署，旧 ai-fund 对新 new-api 保存策略会得到 400 而非静默丢字段。

## Key Context

- 策略 DTO `dto/channel_period_policy.go`；归一化 `service/channel_period_rules.go:NormalizeChannelPeriodConfig`；读取/保存/校验 `service/channel_period_policy.go:47,145,259`。
- 计数 `service/channel_period_usage.go`（`channelPeriodCounters:79`、Lua `channelPeriodUsageAddScript`、`recordChannelPeriodUsage`）；写入入口 `service/channel_user_quota_usage.go:19`，调用方 relay（`relayInfo.OriginModelName`）与 `service/task_billing.go:267`（`task.OriginModelName`）。
- 判定 `service/channel_period_guard.go:31` `GetChannelPeriodStatus`、`:181` `CheckChannelPeriodLimits`、`:219` `CheckSelectedChannelPeriodLimits`（`ContextKeyOriginalModel`）。
- 降级 `controller/channel_limit_fallback.go:prepareChannelLimitFallback`（读 `channel_period_block`、`ResolveChannelLimitFallbackTarget`、`SetupContextForSelectedChannel` 重设 `original_model`）；自指校验 `service/channel_period_policy.go:160`；目标候选 `controller/channel_period_policy.go:90`。
- 个人用量调整参照 `controller/channel_user_limits.go:134` / `service/channel_user_daily_quota.go:211`；策略写入严格键比对 `controller/channel_period_policy.go:109`。
- 前端 `web/src/features/channels/period-types.ts`、`period-api.ts`、`components/dialogs/channel-period-policy-panel.tsx`。
- ai-fund：`worker/src/newapi_period_policy.js`、`pool_period_limits.js`、`newapi_client.js`、`pool_limits.js:385-400`（本人降级提示）、`index.js:7385-7450`、`fixtures/channel-period-contract.json`；前端 `PoolPeriodPolicyEditor.vue`、`PoolLimitAdminModal.vue`、`PoolLimitPanel.vue`、`api/index.js`；规范 `.trellis/spec/backend/newapi-pool-limits.md`。
- 约束：JSON 走 `common.*`；testify 表驱动 + miniredis；DTO 零值规则；数据库无新表（模型计数只在 Redis/内存，策略仍存 TEXT）。

## Risks / Deferred

- `RecordChannelUserQuotaUsage` / `CheckChannelPeriodLimits` 签名变更跨 relay/service/controller，靠编译期发现遗漏；测试中的旧调用需同步。
- Lua 脚本新增 set 变体，先经 miniredis 测试再部署；回滚只需回退代码，模型 key 自然过期。
- 同渠道降级复用源渠道上下文重设，需在全链路测试覆盖多 key 渠道与 Advanced Custom 路径。
- 两仓必须同版本部署（design §7）。
- 延后：规则内按模型上限、通配匹配、按模型个人特批/用量调整、多跳降级。

## Acceptance

- AC1：整体日限 100、`gpt-6-astra` 日限 20 → 该模型累计 20 后 429/降级，其他模型可用至整体 100，整体耗尽后全部 429；周限同理。
- AC2：按原始模型名累计，映射后上游名不影响；Redis/内存一致；新增预算后 `tracking_since` 为其 `created_at`。
- AC3：统一状态与本人状态返回带 `model` 的指标；请求前检查不产生未命中模型的 enforced 指标。
- AC4：设置整体已用为 0 → 新请求立即可用；设置 ≥ 上限 → 立即 429；模型调整不影响整体与个人；revision 不变；审计 before/after。
- AC5：`model` 不在预算列表或 `used_quota` 越界 → 400。
- AC6：两端管理界面可配置并回读一致；ai-fund 本人面板显示模型指标；两仓现有测试全绿。
- AC7：旧存储策略读取/预览/保存正常；未知字段仍 400。
- AC8：`gpt-6-astra` 个人日限 50 → 该用户该模型累计 50 后 429/降级，其他用户与其他模型不受影响；渠道级个人日限独立生效；个人上限为 0 不产生指标。
- AC9：模型池子日限 500、整体 600、模型降级目标同渠道 `gpt-6-mini`：模型到 500 且整体未到 → 改用 `gpt-6-mini` 成功并按其价格结算，日志含原模型/目标模型/触发模型；整体到 600 → 走策略级降级或 429；`gpt-6-mini` 自身预算耗尽 → 429 不二跳；模型未配降级但策略级已启用 → 走策略级目标。
- AC10：保存同渠道目标模型等于本模型、启用但模型为空、指定渠道不存在 → 400；`channel_id` 等于本渠道保存后回读为 0。
- AC11：ai-fund 本人面板降级提示显示"`gpt-6-astra` 今日额度已用完 → `gpt-6-mini`"。

## Next Step

- 新会话 `/trellis:continue` → 复核 Brief 后 `task.py start`，按 `implement.md` A.1 起：在 `dto/channel_period_policy.go` 增加 `ChannelPeriodModelBudget`（含个人上限与 `Fallback`）、`Config.ModelBudgets`、`Metric.Model`。
