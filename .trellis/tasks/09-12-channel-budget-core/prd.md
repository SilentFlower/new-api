# 预算内核收敛：v1 策略转预算列表，判定/计数/降级按行（契约不变）

父任务：`.trellis/tasks/09-12-channel-budget-unification`（需求 R1、R2；验收 AC1、AC3、AC7）。本文只记录子任务自身的范围与验收，事实与背景见父 PRD。

## Goal

把周期策略的判定、计数、状态输出与降级选择统一到"预算行"内核上，删除下标与字符串键特判，使后续 v2 契约与按模型预算只是"多一行"。对外 DTO、路由、Redis key、前端与 ai-fund 全部不变。

## Requirements

- R1.1-R1.6（父 PRD）：内部模型 `channelBudgetPlan`、计数身份与 key 映射、分组优先级、错误码映射、行级降级选择（阶段一所有行的 `on_exceed` 均为 `inherit`，策略级来自 v1 `fallback`）。
- R2.1：`buildChannelBudgetPlan(view, channel)` 由 v1 配置 + 渠道列 + 两张覆盖表构造，派生 id 见父 design §2。
- R2.2：`evaluateChannelBudgets` 替代 `GetChannelPeriodStatus` 内部实现；`recordChannelBudgetUsage` 替代 `recordChannelPeriodUsage`；新增 `RecordChannelUserModelQuotaUsage(ctx, channelID, userID, quota, modelName)`，旧 `RecordChannelUserQuotaUsage` 签名保留并委托，relay 入口传 `OriginModelName`，上游 `service/task_billing.go` 只改一行；`prepareChannelLimitFallback` 改用 `SelectChannelLimitFallback`。旧 relay 日/周独立检查、降级预检里的直接调用与 `CheckSelectedChannelPeriodLimits` 的 revision=0 早返回全部保留：降级目标渠道可能没有策略，此时它们是唯一兜底；删除推迟到阶段二迁移完成后。
- R2.3：对外等价。阶段一不新增任何指标 JSON 字段，`period` 仍输出 `custom`，指标 JSON 逐字节不变；`ChannelPeriodBlock` 只增加内部字段 `row`（`json:"-"`）供降级选择使用。

## Acceptance Criteria

- [x] AC1：`go test ./service/ ./controller/ ./model/` 现有周期、个人日/周、覆盖、降级测试不改断言通过；ai-fund `fixtures/channel-period-contract.json` 与 `node --test src/*.test.js` 不变通过；`web` 渠道测试通过。
- [x] AC3（内核部分）：新增表驱动测试证明 v1 配置 → plan 的派生 id、身份 key 映射与旧 key 完全一致（daily/weekly/occurrence 三种、有无规则日限），Redis（miniredis）与内存记录结果一致。
- [x] AC7：派生的渠道级个人日/周行超限返回旧错误码；池子/occurrence 行返回 `channel_period_quota_exceeded`；revision=0 渠道仅配置渠道列时 429 语义与旧入口一致（含 `skipRetry`）。
- [x] 内核直接测试：`modelName` 精确匹配过滤（阶段一无模型行，测试用构造的 plan 验证匹配与身份 hash key 生成）。

## Out of Scope

- 任何 DTO 字段删除、路由变更、schema_version 变化、前端与 ai-fund 改动。
