# 渠道限额重构：统一预算列表（含按模型日/周预算、统一覆盖与用量调整）

## Goal

管理员在一张"预算"表里配置并看懂渠道的全部金额限制：范围（个人/池子）、周期（日/周/时段整段）、模型（全部/指定）、上限、已用、超限动作。渠道级、时间规则、按模型三类限额不再是三套字段、三套计数、三套接口，新增维度只是表里多一行。首个直接受益场景：为 `gpt-6-astra` 这类高价模型单独设池子/个人的日/周上限，耗尽后按行配置拒绝或同渠道换低价模型。

重构分两阶段交付：阶段一内核收敛、对外契约不变；阶段二暴露 v2 契约、迁移并废弃旧列与旧接口，ai-fund 同步。本任务为父任务，持有需求集、任务图与跨子任务验收；实现在三个子任务中进行。

## Background / 已确认事实

### 现状：四代限额叠加

- 第一代：渠道表列 `user_daily_quota_limit` / `user_weekly_quota_limit` / `user_concurrency_limit`（`model/channel.go:54-56`），渠道更新校验 `controller/channel.go:475-478,975-976,1134-1137`；Redis key `channel_user_daily_quota:{cid}:{date}` / `channel_user_weekly_quota:{cid}:{weekStart}`（`service/channel_user_daily_quota.go:275`、`service/channel_user_weekly_quota.go:277`），hash 只含 `user_id` 字段，`list` 按字段名解析用户 ID；旧错误码 `ErrorCodeChannelUserDailyQuotaExceeded` / `WeeklyQuotaExceeded`（`service/channel_period_guard.go:ChannelPeriodAPIError`）。Relay 侧旧检查 `relay/channel_user_daily_quota.go:15`、`relay/channel_user_weekly_quota.go`，降级预检 `controller/channel_limit_fallback.go:56-59` 也直接调用旧检查。
- 第二代：`channel_user_limit_overrides` 表（`model/channel_user_limit_override.go:12`，并发 + 日 + 周 + 到期），`service/channel_user_limit_override.go`；revision=0 时 max 语义、revision>0 时替换语义（`service/channel_period_guard.go:63-74`）。规范要求"只允许提额，不允许降额"。
- 第三代：周期策略 JSON（`dto/channel_period_policy.go`）：顶层池子日/周 + 时间规则（date_range/weekly，每条 4 项额度）+ 单目标降级；`channel_user_period_overrides` 表按规则整段特批；`channel_quota_tracking` 缺口表。计数 key `channel_pool_daily_quota:{cid}:{date}`、`channel_pool_weekly_quota:{cid}:{weekStart}`、`channel_period_quota:{cid}:{ruleID}:{occStart}`（`service/channel_period_usage.go:channelPeriodCounters`），hash 字段 `user_id` / `__pool` / `__since`，Lua 脚本按 KEYS 下标 `i > 2` 区分池子累加。
- 第四代（未实施，已由本任务替代）：`.trellis/tasks/09-11-channel-pool-model-budget-override` 计划再加 `model_budgets` 与两个池子用量调整接口；其调研锚点保留在 `research/legacy-09-11/brief.md`。

### 结构性问题

- 判定 `GetChannelPeriodStatus`（`service/channel_period_guard.go:31-175`）靠 `i < 2` / `active[i-2]` 下标和 `"user_daily"` / `"pool_custom"` 字符串键区分指标；`resolveChannelPeriodSources`（`service/channel_period_rules.go:216`）以固定 5 个键解析优先级；任何新维度都需要变体函数。
- 计数写入 `recordChannelPeriodUsage` 重读策略并把停用规则强制置为启用后再解析 occurrence（`service/channel_period_usage.go:93-120`）。
- 降级为策略级单目标，选择在 `controller/channel_limit_fallback.go:prepareChannelLimitFallback`（读 `channel_period_block`、`ResolveChannelLimitFallbackTarget`、`SetupContextForSelectedChannel` 重设 `original_model`）；目标候选 `GetChannelPeriodPolicyTargets` 排除本渠道。
- 策略写入 `bindChannelPeriodJSON` 严格键集合比对（`controller/channel_period_policy.go:109`），读取要求 `Normalize(config) DeepEqual config`（`service/channel_period_policy.go:validateChannelPeriodStoredPolicy`）。旧代码读到 `schema_version=2` 会判定策略无效并返回 503，因此阶段二不可仅回退代码。
- 现有一次性迁移（`model/main.go:607,660`）以库元数据判断幂等，无选项标记；本任务的数据迁移以每渠道策略 `schema_version` 判断幂等。

### 界面与消费方

- new-api 前端：`channel-period-policy-panel.tsx`（池子额度 + 规则 + 降级）、`channel-user-limits-dialog.tsx`（个人用量表与调整，`MAX_QUOTA` 常量）、`channel-period-override-editor.tsx`（整段特批）、渠道抽屉 `drawers/sections/channel-user-daily-quota-limit-field.tsx` / `channel-user-weekly-quota-limit-field.tsx`，类型 `types.ts:63-64,298-308,523-524`，默认值 `constants.ts:292-293`，`period-types.ts`、`period-api.ts`。
- ai-fund worker：`newapi_client.js` 调用 `user-daily-quota`（1253,1427）、`user-weekly-quota`（1312,1448）、`user-limit-users`（1472）、`user-limit-status`（1506）、`user-limit-overrides`（1524,1616,1640）、`period-policy`（2196-2229）、`period-rules/.../user-overrides`（2248）、`PUT /api/channel/`（1672，写渠道列）；`newapi_period_policy.js` 白名单归一化；`pool_period_limits.js` 严格键集合与金额换算；`index.js` 路由 `pools/:poolId/user-daily-quota|user-weekly-quota|user-limit-overrides|limits`；fixture `fixtures/channel-period-contract.json`。前端 `PoolPeriodPolicyEditor.vue`、`PoolLimitAdminModal.vue`、`PoolLimitPanel.vue`、`api/index.js`。两仓需同版本部署。
- 第零步已完成（未提交）：周期策略额度改 int64，上界 `common.MaxPeriodQuota`（10^15）；旧渠道列与旧个人覆盖仍受 `common.MaxQuota` 约束，本任务阶段二废弃它们。

### 调用面

- 请求前检查入口 `CheckSelectedChannelPeriodLimits(c)`（`service/channel_period_guard.go:219`），调用方 `controller/relay.go`、`relay/channel_user_daily_quota.go`、`relay/channel_user_weekly_quota.go`、`relay/mjproxy_handler.go`、`controller/channel_limit_fallback.go`。
- 计数入口 `RecordChannelUserQuotaUsage(ctx, channelID, userID, quota)`（`service/channel_user_quota_usage.go:19`），调用方 `service/quota.go`、`text_quota.go`、`tool_billing.go`、`task_billing.go`、`violation_fee.go`；relay 侧持有 `relayInfo.OriginModelName`，任务侧持有 `task.Properties.OriginModelName`。

## Requirements

### R1 统一预算模型（内核语义）

- R1.1 预算行：`{id, name, enabled, scope: user|pool, window: daily|weekly|occurrence, schedule_id, models: []string, limit: int64, on_exceed: {mode: inherit|reject|fallback, channel_id, model}, created_at}`。`models` 为空 = 全部模型；非空按客户端原始模型名（`OriginModelName` / `ContextKeyOriginalModel`）精确匹配，不做通配、前缀或映射后上游名。
- R1.2 时段：`schedules: [{id, name, enabled, kind: date_range|weekly, start_local, end_local, start_at, end_at, start_weekday, end_weekday, start_time, end_time, created_at}]`，语义与现有规则一致（半开区间、服务器时区、DST 边界拒绝、已开始计量不得改边界、删除转停用）。`window=occurrence` 必须引用 `schedule_id`；`window=daily|weekly` 可选引用 `schedule_id` 表示"仅在该时段内生效"。
- R1.3 判定：请求命中的行 = `enabled` 且模型匹配且（无时段或时段当前活跃）。同一 `(scope, window, models)` 键内按"个人覆盖 > date_range 时段行 > weekly 时段行 > 无时段行"取一条生效；不同键的行全部并行约束。任一生效行 `used >= limit` 即超限（软上限、不预占），第一条超限行的 `on_exceed` 决定动作，`inherit` 用策略级 `default_on_exceed`。
- R1.4 计数身份 = `(window, schedule_id, models)`，同身份的个人行与池子行共用一个计数（个人读用户字段，池子读 `__pool`），与 v1 行为一致。一次结算把命中的所有身份一并原子累加（Redis 单 Lua / 内存单锁），脚本输入为逐 key 条目，无下标特判。由 v1 映射出的身份沿用原 key，上线不归零。
- R1.5 状态指标 `ChannelPeriodMetric` 增加 `budget_id`、`budget_name`、`models`、`schedule_id`、`schedule_name`；`period` 取值 `daily|weekly|occurrence`（原 `custom` 更名）。旧错误码映射：`scope=user`、`window=daily|weekly`、`models` 为空且无时段的行超限 → `ChannelUserDailyQuotaExceeded` / `WeeklyQuotaExceeded`；其余 → `channel_period_quota_exceeded`。
- R1.6 降级：行级 `fallback` 允许 `channel_id = 0` 表示同渠道换模型，仅当该行 `models` 非空且目标模型不在本行 `models` 内；策略级 `default_on_exceed` 仍禁止指向本渠道。一跳限制、目标侧以目标模型重跑预检、按目标模型计费、日志 `channel_limit_fallback` 含触发行与模型，沿用现有契约。

### R2 阶段一：内核收敛，对外契约不变（子任务 budget-core）

- R2.1 v1 策略 + 渠道列在读取时转换为 `schedules + budgets`：顶层池子日/周 → 2 行；渠道列个人日/周 → 2 行；每条规则 → 1 个时段 + 最多 4 行（user/daily、pool/daily、user/occurrence、pool/occurrence）；旧个人覆盖表与规则整段特批表 → 对应行的个人覆盖。派生 id 稳定且可逆（见 design.md §3）。
- R2.2 `GetChannelPeriodStatus` / `CheckChannelPeriodLimits` / `CheckSelectedChannelPeriodLimits` / `recordChannelPeriodUsage` / 降级选择改为遍历行，删除下标与字符串键特判。旧 Relay 检查 `checkChannelUserDailyQuota` / `Weekly` 及降级预检的直接调用在阶段一保留（降级目标渠道可能 revision=0，统一判定早返回时它们是唯一兜底），阶段二迁移后所有渠道都有 v2 策略再删除。
- R2.3 对外可观察行为等价：现有 service/controller/model/前端周期测试不改断言即通过；阶段一不新增指标 JSON 字段，指标形状逐字节不变；ai-fund fixture 回放不变；`schema_version` 仍为 1。

### R3 阶段二：v2 契约（子任务 budget-v2-contract）

- R3.1 `schema_version = 2`，配置 = `{schema_version, default_on_exceed, schedules, budgets}`；写入仍为 `{expected_revision, config}` 严格键集合、revision CAS。保存始终写 v2；读取遇到 v1 存储仅在迁移窗口内转换（R6）。`POST /period-policy/preview` 返回每行当前是否活跃、生效来源与下一切换点。
- R3.2 校验：行 ≤ 128、时段 ≤ 64（含停用）；`limit` 为 `0..MaxPeriodQuota`（0 = 不限，不提供零额封禁）；`models` 去空白、非空、行内唯一；同 `(scope, window, models)` 键内同类型时段相交拒绝；`occurrence` 必须引用存在的时段；同渠道降级仅限 `models` 非空的行；行名 1..80 字。
- R3.3 统一个人覆盖：新表 `channel_user_budget_overrides(channel_id, user_id, budget_id, limit, expires_at, updated_by, created_at, updated_at)`，唯一键 `(channel_id, user_id, budget_id)`；接口 `PUT/DELETE /api/channel/:id/budgets/:budget_id/user-overrides/:user_id` body `{limit, expires_at}`，`GET /api/channel/:id/budget-user-overrides?page&page_size` 列表；只对 `scope=user` 的行有效；语义为替换且必须高于该行基础额度（保持提额约束）。
- R3.4 统一用量：`GET /api/channel/:id/budgets/:budget_id/usage?scope=user&page&page_size` 列出当前窗口各用户已用（替代旧日/周用量列表）；`PUT /api/channel/:id/budgets/:budget_id/usage` body `{scope: pool|user, user_id, used_quota}` 直接设置该行当前窗口的 `__pool` 或指定用户字段，不改其他行、不改 revision，审计 before/after，权限 `authz.ChannelOperate`。
- R3.5 `GET /period-policy/targets` 返回含本渠道的候选并标记 `self: true`；`user-limit-status/:user_id` 返回并发 + v2 `period_limits`，移除旧日/周有效额度结构。

### R4 new-api 管理界面（子任务 budget-v2-contract）

- R4.1 周期策略面板改为预算表：列 = 名称、范围、周期、时段、模型、上限、已用/剩余（进度）、超限动作、启用；行编辑走抽屉；时段为独立可复用列表；策略级默认动作在表下方。
- R4.2 用量列表、用量调整、个人特批均从行上打开，按 `budget_id` 操作；`channel-user-limits-dialog.tsx` 的日/周用量页签改为按行选择。
- R4.3 指标标签形如 `gpt-6-astra · 今日`，时段行显示时段名；本人门户同规则。
- R4.4 渠道抽屉删除个人日/周字段，只保留并发；六语种 i18n 同步；zod schema 升为 v2；v1 预览/保存路径删除。

### R5 ai-fund 跟进（子任务 budget-ai-fund）

- R5.1 worker：策略/状态归一化改为 v2 白名单；`poolPeriodConfigToInternal` / `ToDisplay` 按行换算；BFF 改为按行的用量列表/调整与个人覆盖；`PUT /pools/:poolId/limits` 只保留并发；目标候选含本渠道（仅供模型行）；本人降级提示按行携带 `models` 与目标。
- R5.2 前端：策略编辑器改为预算表 + 时段列表；用量调整与特批按行；本人面板指标标签含模型/时段。
- R5.3 fixture 由 new-api 实际响应生成；规范 `.trellis/spec/backend/newapi-pool-limits.md` 同步；两仓同版本部署。

### R6 迁移与废弃旧列（子任务 budget-v2-contract）

- R6.1 启动迁移：`migrateDB` 之后对每个渠道执行，若策略不存在或 `schema_version=1`，把渠道列个人日/周（>0）与 v1 策略转换为 v2 并按 revision CAS 写入（revision +1）；`channel_user_limit_overrides` 的日/周字段 → `channel_user_budget_overrides`（budget_id 为派生 id），`channel_user_period_overrides` → 同表（budget_id 为规则 occurrence 行 id）。幂等：v2 策略跳过，覆盖表按唯一键 upsert；只用 GORM，三库兼容；单渠道失败记录日志并继续，启动不中断，下次启动重试。
- R6.2 迁移后代码不再读写渠道表 `user_daily_quota_limit` / `user_weekly_quota_limit`：Go 字段保留供 GORM 但 `json:"-"`，渠道创建/更新忽略并不再校验，`channel_user_limit_overrides` 的日/周字段忽略。列与旧表物理保留一版，标注废弃，后续版本删除。
- R6.3 下线接口：`GET/PUT /user-daily-quota*`、`/user-weekly-quota*`、`/period-rules/:rule_id/user-overrides/:user_id`；`user-limit-overrides` 仅接受 `user_concurrency_limit` 与 `expires_at`，携带日/周字段返回 400。
- R6.4 遗留 Redis 计数 key 由派生行继续使用，上线不归零。
- R6.5 回滚：上线前备份 `channel_period_policies`、`channel_user_limit_overrides`、`channel_user_period_overrides`；回滚 = 回退代码 + 恢复三表备份（旧列在迁移后未被新代码修改）。

## Acceptance Criteria

- [ ] AC1 / R2.3：子任务 budget-core 合入后，`service`、`controller`、`model` 现有周期/降级/个人限额测试全绿；上线前后同一渠道的池子日/周、规则 occurrence、个人日/周已用值不变（key 沿用）。
- [ ] AC2 / R1.3 R3：整体池子日限 100、`gpt-6-astra` 池子日限 20：该模型累计 20 后 429 或降级，其他模型可用至整体 100，整体耗尽后全部 429；周限、个人范围同理。
- [ ] AC3 / R1.1 R1.4：按原始模型名精确累计与判定，映射后上游名不影响；Redis 与内存结果一致；新行的 `tracking_since` 为其 `created_at`；修改行的 `models` 后计数身份变化并从修改时刻起算。
- [ ] AC4 / R1.6：模型行日限 500、整体 600、行级降级同渠道 `gpt-6-mini`：模型到 500 且整体未到 → 改用 `gpt-6-mini` 并按其计费，日志含触发行/原模型/目标；整体到 600 → 策略级动作；目标自身预算耗尽 → 429 不二跳。
- [ ] AC5 / R3.1 R6.1：迁移后每个渠道策略为 v2 且与迁移前 v1 + 渠道列语义等价（预览指标一致）；重复启动不改 revision；未知字段或缺字段 400；revision 冲突 409。
- [ ] AC6 / R3.3 R3.4：按行的个人覆盖与用量调整只影响该行、该用户；revision 不变；审计含 before/after；越界、不存在的行、对池子行做个人覆盖 → 400。
- [ ] AC7 / R1.5：派生的渠道级个人日/周行超限时旧错误码不变；其他行返回 `channel_period_quota_exceeded`。
- [ ] AC8 / R4 R6.2 R6.3：预算表可增删改行与时段并回读一致；渠道抽屉无日/周字段；旧接口返回 404；typecheck、lint、前端测试通过。
- [ ] AC9 / R5：ai-fund 管理端与本人面板按 v2 工作，worker 全量测试与前端构建通过；fixture 来自 new-api 实际响应。
- [ ] AC10 / R6.5：按回滚步骤恢复后，旧版本 new-api 可正常读取策略并放行请求。

## Out of Scope

- 并发限制（租约模型，非计数）仍走 `channel_user_limit_overrides.user_concurrency_limit` 与渠道列，不进入预算表。
- 模型通配、前缀、分组匹配；monthly / 滚动窗口（v2 枚举可后续追加）。
- 多跳降级、按价格自动挑目标。
- 池子上限的临时特批（改上限而非改已用）；个人覆盖降额。
- 废弃列与旧表的物理删除（下一版本）。

## 任务图

| 子任务 | 范围 | 验收 | 顺序 |
| --- | --- | --- | --- |
| `budget-core` | R1、R2 | AC1、AC3（内存/Redis 等价部分）、AC7 | 先行 |
| `budget-v2-contract` | R3、R4、R6 | AC2、AC4、AC5、AC6、AC8、AC10 | 依赖 core 合入 |
| `budget-ai-fund` | R5 | AC9 | 依赖 v2 契约冻结 |
