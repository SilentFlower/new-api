# Brief — 渠道限额重构：统一预算列表（含按模型日/周预算、统一覆盖与用量调整）

## Goal

- 把渠道级个人日/周限、池子日/周限、时间规则额度、按模型日/周额度统一为一种"预算行"（范围 × 周期 × 模型 × 上限 × 超限动作），管理员在一张表里配置与查看；分两阶段交付，阶段二迁移并废弃旧列与旧接口，ai-fund 同步。

## Scope

- 父任务持有需求集、任务图与集成验收；实现拆为三个子任务：
  1. `09-12-channel-budget-core`（R1、R2）：v1 策略 + 渠道列 + 两张覆盖表在读取时转为预算列表；判定、计数、状态、降级选择全部按行；Redis key、DTO、路由、前端、ai-fund 不变。
  2. `09-12-channel-budget-v2-contract`（R3、R4、R6）：`schema_version=2` 配置（`default_on_exceed` + `schedules` + `budgets`）；新表 `channel_user_budget_overrides` 与按行的个人覆盖/用量列表/用量调整接口；启动迁移把 v1 策略、渠道列日/周、两张覆盖表并入 v2；下线旧日/周用量、规则特批接口，渠道列与旧覆盖字段停止读写；new-api 前端改为预算表 + 时段列表 + 按行操作，渠道抽屉删除日/周字段。
  3. `09-12-channel-budget-ai-fund`（R5）：worker 归一化/BFF/路由与前端组件迁移到 v2，fixture 由 new-api 实际响应生成。
- 首个业务场景：`gpt-6-astra` 单独的池子/个人日周上限，耗尽后按行配置拒绝或同渠道换 `gpt-6-mini`。

## Non-Goals

- 并发限制不进预算表，仍走 `channel_user_limit_overrides.user_concurrency_limit` 与渠道列。
- 模型通配/前缀/分组匹配；monthly 与滚动窗口；多跳降级；按价格自动选目标。
- 池子上限临时特批（改上限）；个人覆盖降额。
- 废弃列与旧表的物理删除（下一版本）。

## Key Decisions

- 旧列与旧覆盖表**迁移并废弃**（用户选择）：启动时一次性迁移为 v2，代码不再读写渠道表日/周列与旧覆盖表日/周字段；列与表物理保留一版。回滚 = 回退代码 + 恢复三表备份。
- 计数身份 = `(window, schedule_id, models)`，个人行与池子行同身份共用一个计数（与 v1 一致）；由 v1 派生的身份沿用旧 Redis key，上线不归零。修改行的 `models` 视为换身份，从修改时刻起算。
- 判定语义一句话："命中的每一行都要过"；同 `(scope, window, models)` 键内按个人覆盖 > date_range 时段 > weekly 时段 > 无时段取一条，不同键并行；第一条超限行的 `on_exceed` 决定动作，`inherit` 用策略级默认动作。
- 行级降级允许同渠道换模型（仅 `models` 非空的行，目标模型不在本行内）；策略级默认动作禁止指向本渠道；一跳限制与目标侧重跑预检不变。
- 模型按客户端原始模型名精确匹配。
- 旧错误码保留：派生的渠道级个人日/周行超限仍返回 `ChannelUserDailyQuotaExceeded` / `Weekly`；其余行 `channel_period_quota_exceeded`。
- 契约：`period-policy` URL 不变、body 升 v2；`ChannelPeriodMetric` 在阶段二增加 `budget_id` / `budget_name` / `models` / `schedule_*`，`period` 由 `custom` 改 `occurrence`；阶段一不新增任何字段，指标 JSON 逐字节不变。
- 额度为 int64，上界 `common.MaxPeriodQuota`（第零步已完成，未提交）。

## Key Context

- 现状与结构性问题：`service/channel_period_guard.go:31-175`（下标/字符串键特判）、`service/channel_period_usage.go`（Lua `i > 2`）、`service/channel_period_rules.go:216`、`controller/channel_limit_fallback.go`、`controller/channel_period_policy.go:109`（严格键集合）、`model/channel.go:54-56`、`model/channel_user_limit_override.go:12`、`model/channel_user_period_override.go`。
- 计数入口 `service/channel_user_quota_usage.go:19` 及五处调用方；请求前检查 `CheckSelectedChannelPeriodLimits` 及 relay/controller/mjproxy 调用方；旧 relay 日/周独立检查 `relay/channel_user_daily_quota.go:15`。
- 前端：`web/src/features/channels/components/dialogs/channel-period-policy-panel.tsx`、`channel-user-limits-dialog.tsx`、`channel-period-override-editor.tsx`、`drawers/sections/channel-user-*-quota-limit-field.tsx`、`period-types.ts`、`period-api.ts`、`types.ts`、`constants.ts`。
- ai-fund：`worker/src/newapi_client.js`（旧接口调用 1253-1672、2196-2248）、`newapi_period_policy.js`、`pool_period_limits.js`、`pool_limits.js`、`index.js`、`fixtures/channel-period-contract.json`；前端 `PoolPeriodPolicyEditor.vue`、`PoolLimitAdminModal.vue`、`PoolLimitPanel.vue`。
- 约束：JSON 走 `common.*`；三库兼容只用 GORM；`lockForUpdate`；testify 表驱动 + miniredis；DTO 零值规则；relaykit 独立可构建；旧任务 `09-11-channel-pool-model-budget-override` 已被替代，其调研锚点在 `research/legacy-09-11/brief.md`。

## Risks / Deferred

- 阶段二为单向迁移：旧代码读到 v2 策略会判定无效并返回 503，回滚必须恢复三表备份；上线前备份并演练（AC10）。
- 阶段二与 ai-fund 必须同版本部署；new-api 先上（启动迁移），ai-fund 紧随。
- 旧 relay 日/周独立检查在阶段一保留作为无策略目标渠道的兜底，阶段二迁移后删除；`RecordChannelUserQuotaUsage` 签名不变，新增 `RecordChannelUserModelQuotaUsage`。
- 迁移逐渠道 CAS，冲突渠道跳过并在下次启动重试；单渠道失败记录日志不中断启动。
- 延后：monthly 窗口、通配匹配、个人覆盖降额、废弃列物理删除、旧任务 09-11 丢弃与第零步提交（需用户确认）。

## Acceptance

- AC1：core 合入后现有周期/降级/个人限额测试不改断言通过；上线前后已用值不变。
- AC2：整体池子日限 100、`gpt-6-astra` 日限 20 → 该模型累计 20 后 429/降级，其他模型可用至 100，整体耗尽后全部 429；周限、个人范围同理。
- AC3：按原始模型名精确累计；Redis 与内存一致；新行 `tracking_since` 为 `created_at`。
- AC4：模型行 500、整体 600、同渠道降级 `gpt-6-mini` → 改用目标并按其计费，日志含触发行；整体到 600 走策略级；目标自身耗尽 429 不二跳。
- AC5：迁移后每渠道策略为 v2 且与 v1 + 渠道列语义等价；重复启动 revision 不变；未知/缺字段 400；冲突 409。
- AC6：按行覆盖/用量只影响该行该用户；revision 不变；审计 before/after；越界/不存在行/池子行个人覆盖 400。
- AC7：派生个人日/周行旧错误码不变。
- AC8：预算表增删改回读一致；渠道抽屉无日/周字段；旧接口 404；typecheck/lint/前端测试通过。
- AC9：ai-fund 管理端与本人面板按 v2 工作，worker 全量测试与前端构建通过。
- AC10：回滚演练通过。

## Next Step

- 确认本 Brief 后：`python3 ./.trellis/scripts/task.py start .trellis/tasks/09-12-channel-budget-core`，按其 `implement.md` 第 1 步起。
