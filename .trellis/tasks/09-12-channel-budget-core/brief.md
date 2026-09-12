# Brief — 预算内核收敛：v1 策略转预算列表，判定/计数/降级按行（契约不变）

## Goal

- 把周期策略的判定、计数、状态输出与降级选择统一到"预算行"内核上，删除下标与字符串键特判；对外 DTO、路由、Redis key、前端与 ai-fund 全部不变。

## Scope

- 新增 `service/channel_budget_plan.go`（内部类型、`buildChannelBudgetPlan` 由 v1 配置 + 渠道列 + 两张覆盖表构造、派生 id）、`service/channel_budget_usage.go`（身份/hash/key 映射、逐条目 Lua、记录与批量读取）、`service/channel_budget_guard.go`（`evaluateChannelBudgets`、检查入口、错误码映射）、`service/channel_limit_fallback_select.go`。
- 删除 `service/channel_period_guard.go`、`service/channel_period_usage.go`；`resolveChannelPeriodSources` 改为预算计划的 v1 投影（供 v1 预览与旧覆盖校验，阶段二随 v2 预览删除）。
- 新增 `RecordChannelUserModelQuotaUsage`，旧签名保留并委托；`RecordRelayChannelUserQuotaUsage` 传 `OriginModelName`；上游文件只改 `service/task_billing.go` 一行。
- `controller/channel_limit_fallback.go` 改用 `SelectChannelLimitFallback`（行级优先、策略级兜底）。旧 relay 日/周独立检查与降级预检里的直接调用保留：降级目标可能 revision=0，统一判定早返回时它们仍是唯一兜底；阶段二迁移后删除。`ResolveChannelUserEffectiveLimits` 经 v1 投影取基线。
- 阶段一不新增指标 JSON 字段（契约逐字节不变）；`ChannelPeriodBlock` 只增加内部 `row` 供降级选择；`period` 仍输出 `custom`。

## Non-Goals

- 任何 DTO 字段删除、路由变更、`schema_version` 变化、前端与 ai-fund 改动。

## Key Decisions

- 计数身份 `(window, schedule_id, models)`；v1 派生身份映射到旧 key：`(daily,"",[])` → 用户字段在 `channel_user_daily_quota`、`__pool` 在 `channel_pool_daily_quota`；weekly 同理；`(occurrence, ruleID, [])` → `channel_period_quota:{cid}:{ruleID}:{occStart}`；规则内日限与无时段日限共用计数；其余 → `channel_budget:{cid}:{identityHash}:{windowStart}`。
- Lua 脚本改为逐条目 `(key, expireAt, since, poolFlag)`，去掉 `i > 2` 特判；溢出预检与 HINCRBY 不变。
- 阶段一所有行 `on_exceed=inherit`，策略级默认动作来自 v1 `fallback`。
- revision=0 渠道由派生行统一覆盖，删除"无策略走旧入口"分支；`ContextKeyChannelUserDailyQuotaLimit/Weekly` 不再由周期判定写入，`RelayInfo` 对应字段保留恒为 0。

## Key Context

- 父任务 `design.md` §2-§5；父 PRD 背景与调用面。
- 旧实现锚点：`service/channel_period_guard.go:31-175`、`service/channel_period_usage.go:79-201`、`service/channel_period_rules.go:216-255`、`controller/channel_limit_fallback.go:40-70`。
- 约束：JSON 走 `common.*`；testify 表驱动 + miniredis；内存锁顺序 `channelPeriodUsageMemory` → 旧日 → 旧周。

## Risks / Deferred

- Lua 脚本重写：先 miniredis 全绿再合入；回滚只需回退代码。
- `RecordChannelUserQuotaUsage` 签名变更靠编译期发现遗漏；测试中的旧调用需同步。
- ai-fund fixture 必须逐字节不变，作为等价性证据。

## Acceptance

- AC1：`go test ./service/ ./controller/ ./model/` 现有周期、个人日/周、覆盖、降级测试不改断言通过；ai-fund `node --test src/*.test.js` 与 fixture 不变；`web` 渠道测试通过。
- AC3（内核）：表驱动测试证明派生 id 与身份 key 与旧 key 完全一致；miniredis 与内存记录结果一致。
- AC7：派生个人日/周行返回旧错误码；池子/occurrence 行返回 `channel_period_quota_exceeded`；revision=0 渠道 429 语义与旧入口一致（含 `skipRetry`）。
- 内核直接测试：构造含模型行的 plan 验证 `modelName` 精确匹配与 hash key 生成。

## Next Step

- 实现与聚焦验证已完成；Check-All 通过后进入 `trellis-update-spec`，再由 `trellis-push` 生成提交计划。
