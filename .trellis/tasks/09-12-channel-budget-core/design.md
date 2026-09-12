# Design — 预算内核收敛

技术设计以父任务 `design.md` §2-§5 为准。本文只列文件级落点与阶段一特有约束。

## 文件计划

- 新增 `service/channel_budget_plan.go`：内部类型、`buildChannelBudgetPlan`（由 v1 配置 + 渠道列派生）、派生 id 常量、分组/身份/优先级解析 `resolveChannelBudgetRows`。
- 新增 `service/channel_budget_usage.go`：`newChannelBudgetCounter`（身份到 key 的映射）、逐条目 Lua 脚本、`recordChannelBudgetUsage`、`readChannelBudgetCounter`；旧 `channel_period_usage.go` 删除，`channelPeriodUsageMemory` / `channelPeriodUsageBucket` / `channelPeriodNow` 名称保留。
- 新增 `service/channel_budget_guard.go`：`evaluateChannelBudgets`、`GetChannelPeriodStatus`、`CheckChannelPeriodLimits`、`CheckSelectedChannelPeriodLimits`、`ChannelPeriodAPIError`（错误码按行的 `models` 判定）。旧 `channel_period_guard.go` 删除。
- `service/channel_period_rules.go`：保留时段解析与 `NormalizeChannelPeriodConfig`；`resolveChannelPeriodSources` 改为预算计划的 v1 投影（签名不变，供 v1 预览、`ResolveChannelUserEffectiveLimits` 与 `ReplaceChannelUserLimitOverride` 使用）。
- 新增 `service/channel_limit_fallback_select.go`：`SelectChannelLimitFallback`。
- `controller/channel_limit_fallback.go`：用 `SelectChannelLimitFallback` 的结果替代 `policy.Config.Fallback`；旧日/周直接检查保留（目标渠道可能 revision=0）；`ChannelLimitFallbackInfo` 新字段推迟到阶段二。
- `service/channel_user_quota_usage.go`：新增 `RecordChannelUserModelQuotaUsage(ctx, channelID, userID, quota, modelName)`，旧 `RecordChannelUserQuotaUsage` 委托；`RecordRelayChannelUserQuotaUsage` 传 `OriginModelName`。上游文件只改 `service/task_billing.go` 一行传 `task.Properties.OriginModelName`。
- `dto/channel_period_policy.go`、`relay/channel_user_daily_quota.go`、`relay/channel_user_weekly_quota.go`：阶段一不改动。
- 新增 `service/channel_budget_plan_test.go`：旧 key 映射、同身份共用、implicit 行、优先级、模型精确匹配、降级选择、内存/Redis 等价。

## 阶段一约束

- 不改 Redis key、不改 Lua 对旧 key 的写入语义（用户字段、`__pool`、`__since`、过期）。
- `period` 输出保持 `custom`；不新增指标 JSON 字段以保 fixture 逐字节不变。
- 旧个人日/周检查保留，`CheckSelectedChannelPeriodLimits` 继续把渠道级个人日/周上限写回上下文键（转 `int`）。

## 实施偏差记录（阶段一）

- 未新增指标 JSON 字段：为保证 ai-fund fixture 与前端契约逐字节不变，`budget_id` 等字段推迟到阶段二随 v2 一起暴露；行信息只经 `ChannelPeriodBlock.row` 供降级选择使用。
- 保留 `relay/channel_user_daily_quota.go` / `weekly` 与降级预检里的旧个人日/周检查：降级目标渠道可能没有策略（revision=0），统一判定在该情况下早返回，旧检查仍是唯一兜底。阶段二迁移后所有渠道都有 v2 策略，再删除这些检查。
- `resolveChannelPeriodSources` 保留为预算计划的 v1 投影而非删除：v1 预览响应的 `limits/sources` 形状与旧个人覆盖校验直接依赖它，阶段二随 v2 预览一起删除。
- `RecordChannelUserQuotaUsage` 签名不变，新增 `RecordChannelUserModelQuotaUsage`；上游文件只改 `service/task_billing.go` 一行（build 分支最薄接入）。
- 旧 `channelPeriodUsageMemory` / `channelPeriodUsageBucket` / `channelPeriodNow` 名称保留，测试夹具无需改动。
