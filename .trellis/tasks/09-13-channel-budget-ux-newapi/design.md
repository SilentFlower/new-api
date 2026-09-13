# Design — new-api 渠道限额界面改版

技术设计以父任务 `design.md` §1–§3、§5 为准。本文列文件级落点与实现顺序中的约束。

## 后端

- `controller/channel_budget.go`：新增 `GetChannelBudgetUsageSummary`，遍历 `GetChannelPeriodPolicy` 返回的已保存行；池子行读池子字段，个人行取按行用量第一页第一名并用现有个人状态逻辑取生效上限；复用现有服务，不新增存储访问路径。
- `router/channel-router.go`：`GET /:id/budgets/usage-summary`，中间件与 `GET /:id/budgets/:budget_id/usage` 相同；`router/channel_router_test.go` 加权限断言。
- `controller/channel_period_policy_test.go`：契约测试扩展 usage-summary，`CHANNEL_PERIOD_CONTRACT_OUTPUT` 同时输出到本任务 `research/channel-budget-usage-summary.json`。
- `dto/channel_period_policy.go`：新增 `ChannelBudgetUsageSummaryView` / `ChannelBudgetUsageSummaryItem` / `ChannelBudgetUsageTopUser`，JSON 键见父 design §2。

## 前端（web/src/features/channels）

- `period-types.ts`：summary 类型；`period-api.ts`：`getChannelBudgetUsageSummary` 与 `channelPeriodErrorMessage`；`lib/channel-budget-status.ts`：状态推导 `deriveChannelBudgetStatus`、计数身份分组 `groupChannelBudgetCounters`、生效上限 `channelBudgetEffectiveLimit`（父 design §1 规则，纯函数，`lib/__tests__/channel-budget-status.test.ts` 表驱动测试）。
- `lib/channel-schedule-state.ts`：`resolveChannelScheduleStates` 解析每个时段是否进行中、下次开始与是否已结束——优先取预览行 `source.start_at/end_at`（服务器时区的当前或下次出现），无引用时指定日期按起止推算、每周时段按 `view.timezone` 用 Intl 推算；停用时段 `active=false`。行状态与时段列/时段列表都由它驱动。
- `lib/channel-policy-error-rows.ts`：`matchChannelPolicyErrorRows` 按后端 400 文案「…：<行名>」「<行名> 的…」「A / B」精确匹配出错行，`channelPolicyErrorDetail` 去掉哨兵前缀；后端契约不变。
- `components/dialogs/budget/`：`budget-table.tsx`（含摘要条与筛选）、`budget-row-state.ts`、`budget-status-badge.tsx`、`budget-editor-sheet.tsx`、`schedule-list.tsx`、`policy-diff.ts`、`policy-save-bar.tsx`、`policy-preview-dialog.tsx`、`budget-usage-sheet.tsx`、`budget-usage-tab.tsx`、`budget-override-sheet.tsx`。`channel-period-policy-panel.tsx` 只保留数据加载、RHF 表单与组合；`channel-budget-progress.tsx` 删除；`channel-budget-usage.tsx` 保留 `ChannelBudgetUsagePanel` 供用量抽屉复用，`channel-period-override-editor.tsx` 保留并新增 `defaultBudgetId` 与撤销确认，供提额抽屉复用。
- `channel-user-limits-dialog.tsx`：页签顺序 预算 `budgets` / 时段 `schedules` / 用量 `budget-usage` / 个人覆盖 `overrides` / 并发 `concurrency`，默认 `budgets`；策略面板只挂载一次（`role=tabpanel` 容器按页签 `hidden`），以 `section` 切换预算表与时段列表，两者共用同一份 RHF 草稿与保存栏；关闭前脏草稿确认；个人覆盖页签两个区块；并发覆盖为独立 `Sheet`，撤销走 `ConfirmDialog`。
- 后端 400：面板顶部 Alert 显示原文；命中的行加 `data-error` 高亮并在名称下显示具体原因，打开该行编辑抽屉时顶部同样显示；预览失败不锁定草稿，保存失败沿用锁定。
- 抽屉用 `ui/sheet` 与 `visible-scrollbar` 滚动容器；编辑表单沿用 RHF `register` + 行内 `role="alert"` 提示，范围/周期/模型用按钮组（`aria-pressed`）而非 MultiSelect；确认用 `components/confirm-dialog`；加载用 `loading-state`。
- i18n：新增文案全部走 `t()`，`bun run i18n:sync` 后七语言逐字节检查与缺键扫描。

## 测试

- `__tests__/channel-period-policy-panel.test.tsx`、`__tests__/channel-user-limits-dialog.test.tsx` 改写为新 DOM；新增：保存栏出现与计数、diff 内容与 PUT body 一致、脏草稿关闭确认、summary 只请求一次、状态徽标推导、共用计数说明、本渠道写 0、页签顺序与时段页签草稿保留、并发覆盖抽屉、后端 400 行定位、时段下次开始。
- `lib/__tests__/channel-budget-status.test.ts`：状态推导、计数身份分组与生效上限表驱动；`lib/__tests__/channel-schedule-state.test.ts`：指定日期/预览来源/每周推算/停用/时区回退；`lib/__tests__/channel-policy-error-rows.test.ts`：三种后端文案格式与前缀剥离。
