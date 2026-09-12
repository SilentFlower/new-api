# Design — 预算 v2 契约

技术设计以父任务 `design.md` §6-§8 为准。本文列文件级落点。

## 后端

- `dto/channel_period_policy.go`：v2 结构体替换 v1；`ChannelPeriodMetric` 字段去 `omitempty`，`period` 取值 `occurrence`。
- `service/channel_budget_policy.go`：`NormalizeChannelBudgetConfig`、`validateChannelBudgetStoredPolicy`、`PreviewChannelBudgetPolicy`；`buildChannelBudgetPlan` 改为直接来自 v2；`service/channel_period_rules.go` 归一化改为时段级。
- `model/channel_user_budget_override.go`（新表，额度列为 `quota_limit bigint`）+ `model/main.go` 两处迁移列表；`service/channel_budget_migrate.go` 启动迁移（`main.go` 主节点在 `MigrateRetiredFrontendOptions` 后调用）；`model/channel_user_limit_override.go` 日/周字段 `json:"-"`；`model/channel.go` 两列 `json:"-"`，`GetUserDailyQuotaLimit/Weekly` 删除。
- `controller/channel.go`：删除日/周校验与 normalize；`controller/channel_period_policy.go`：严格键按 v2；新增 `controller/channel_budget.go`（用量列表/调整与行级提额接口）；删除 `controller/channel_user_daily_quota.go`、`channel_user_weekly_quota.go`、`channel_user_limits.go` 中日/周部分；`router/channel-router.go` 与 `router/channel_router_test.go` 权限断言。
- `service/channel_user_daily_quota.go` / `weekly`：保留 store 读写供派生行 key 使用，删除独立检查与管理列表；`relaycommon.RelayInfo` 日/周字段与 `relay/vision_assist.go` 上下文键复制删除。
- 审计键：`channel.budget_usage_set`、`channel.budget_user_override_set/delete`。

## 前端（web/src/features/channels）

- `period-types.ts`：v2 zod（`budgets`、`schedules`、`default_on_exceed`）；`period-api.ts`：按行用量/覆盖函数；删除旧日/周用量与规则特批 API。
- `components/dialogs/channel-period-policy-panel.tsx` 按父 PRD R4.1 实现预算表：独立时段列、已用/剩余进度、行编辑抽屉、表下方默认动作。池子行直接展示汇总，个人行展示当前窗口用量最高用户的进度并标注用户，详情仍按行展开；未保存身份不查询用量。抽屉与父表共享草稿，关闭保留草稿，预览/保存保持整份策略的 revision CAS。
- `channel-budget-usage.tsx` 承载按行用量面板与用量页签；`channel-period-amount.tsx` 承载金额输入，`lib/channel-period-label.ts` 承载行标签；`channel-period-override-editor.tsx` 改按行；`channel-user-limits-dialog.tsx` 日/周页签替换为“预算用量”页签，个人覆盖页签新增行级提额列表。
- 删除 `drawers/sections/channel-user-daily-quota-limit-field.tsx`、`channel-user-weekly-quota-limit-field.tsx` 及测试；`types.ts`、`constants.ts`、`lib/channel-form-errors.ts` 清理。
- i18n 六语种 `bun run i18n:sync`。

## 迁移与回滚

父 design §8。迁移测试放 `service/channel_budget_migrate_test.go`（内存 SQLite）；三库 DSN 模式沿用 `model/channel_period_policy_database_test.go`。

## 本轮检查修复约定

- 撤销提额以新覆盖表内 `quota_limit=0` 的非生效记录保存；读取/分页均过滤，迁移的唯一键冲突不覆盖它；重新提额可替换该记录。旧表保持原状，回滚仍恢复备份。
- 仅已声明的 schema_version=1 可进入兼容转换；未知或缺失版本不得转换或迁移覆盖。无策略记录单独视为旧渠道列输入。
- 预算行计数身份（模型、窗口、整段时段）变化时重置其 created_at 为变更时间；无计数身份变化时保持原时间，共用身份仍复用已存在计数。
- 用量调整的审计前值读取失败必须返回错误，停止调整和成功审计。
- AC10 补充实际旧版本（本任务前基线）在恢复后读取策略，并验证未超限放行与超限拒绝。
