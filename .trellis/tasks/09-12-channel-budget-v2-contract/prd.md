# 预算 v2 契约：配置/覆盖/用量接口、迁移废弃旧列、new-api 前端预算表

父任务：`.trellis/tasks/09-12-channel-budget-unification`（需求 R3、R4、R6；验收 AC2、AC4、AC5、AC6、AC8、AC10）。依赖 `09-12-channel-budget-core` 合入。

## Goal

对外暴露预算列表契约（schema_version=2），管理员在一张预算表里配置渠道级、时段、按模型的日/周/整段额度与超限动作；个人覆盖与用量调整按行操作；启动迁移把 v1 策略、渠道列与两张覆盖表并入 v2 并废弃旧列、旧接口。

## Requirements

- R3.1-R3.5、R4.1-R4.4、R6.1-R6.5（父 PRD）。
- R7 阶段一推迟到本任务的收尾项（迁移完成后所有渠道都有 v2 策略，revision=0 不再存在）：
  - R7.1 删除 `relay/channel_user_daily_quota.go`、`relay/channel_user_weekly_quota.go` 的独立检查、`controller/channel_limit_fallback.go` 降级预检里的直接调用，以及 `CheckSelectedChannelPeriodLimits` 的 revision=0 早返回与上下文键回写；`relaycommon.RelayInfo.ChannelUserDailyQuotaLimit/Weekly` 字段与 `relay/vision_assist.go` 的上下文键复制一并删除；Midjourney 等非标准响应路径的错误码映射改由统一判定的错误码驱动。
  - R7.2 `ChannelPeriodMetric` 新增 `budget_id`、`budget_name`、`models`、`schedule_id`、`schedule_name`，`period` 由 `custom` 改为 `occurrence`；`ChannelLimitFallbackInfo` 新增 `budget_id`、`models`。
  - R7.3 删除 `resolveChannelPeriodSources` v1 投影，预览改为 `PreviewChannelBudgetPolicy` 的按行输出。
- 与 ai-fund 同版本部署；本子任务完成时 fixture 生成脚本产出新 `channel-period-contract.json` 供子任务 ai-fund 使用。

## Acceptance Criteria

- [ ] AC2、AC4：按模型池子/个人日限与同渠道模型降级的全链路测试（`controller/channel_limit_fallback*_test.go` 新增用例）。
- [ ] AC5：迁移测试（sqlite + 专用 MySQL/PostgreSQL DSN）：v1 策略 + 渠道列 + 两张覆盖表 → v2 等价；重复启动 revision 不变；v2 未知字段/缺字段 400；revision 冲突 409。
- [ ] AC6：按行覆盖/用量接口的契约测试（400 矩阵、审计、revision 不变、隔离）。
- [ ] AC8：前端预算表增删改与回读、渠道抽屉无日/周字段、旧接口 404；`bun run typecheck && bun run lint && bun test src/features/channels && bun run i18n:sync` 无 diff。
- [ ] AC10：回滚演练记录在任务 `research/rollback-drill.md`。
- [ ] AC11 / R7：旧检查删除后，降级目标侧（含同渠道换模型）的个人日/周上限仍由统一判定拦截，全链路测试覆盖；`relay` 包不再引用 `ContextKeyChannelUserDailyQuotaLimit/Weekly`。

## Out of Scope

- ai-fund 改动（子任务 ai-fund）。
- 废弃列与旧表的物理删除。
