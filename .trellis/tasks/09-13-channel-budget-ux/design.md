# Design — 预算 v2 号池限制管理 UI/UX 优化

原型是规格：`research/channel-budget-redesign.html`（new-api）、`research/ai-fund-redesign.html`（ai-fund）。本文只写原型之外必须约定的结构、数据流与边界。

## 1. 共享的展示模型（两端前端各自实现，规则一致）

行状态由数据推导，优先级从高到低：

| 状态 | 条件 |
| --- | --- |
| 已停用 | `enabled=false` |
| 未到时段 | 引用了时段且该时段当前不活跃（预览/状态接口的 `active=false`） |
| 已超限 | `limit>0` 且 `used >= 生效上限` |
| 接近上限 | `limit>0` 且 `used / 生效上限 >= 0.85` |
| 生效中 | 其余 |

- 生效上限：个人行取用量最高用户的 `effective_limit`（含提额），池子行取 `limit`；`limit=0` 显示「不限」且不画进度条。
- 共用计数：前端按计数身份分组，身份 = `window + (window==occurrence ? schedule_id : "") + 排序后的 models`；同身份多行时，除第一条（按创建时间最早）外其余显示「与「X」共用计数」。
- 摘要条计数与筛选沿用上述状态；「需关注」= 接近上限 + 已超限；「生效中」包含未到时段。
- 「下次时段切换」取预览接口的 `next_change_at` 与对应时段名。

## 2. 聚合用量接口（new-api 后端，唯一后端改动）

`GET /api/channel/:id/budgets/usage-summary`，权限同 `GET /budgets/:budget_id/usage`（`authz.ChannelRead`），响应：

```json
{
  "channel_id": 1, "revision": 3, "storage_mode": "redis", "now": 1789264809,
  "items": [
    { "budget_id": "…", "scope": "pool", "window_start": 0, "window_end": 0, "tracking_since": 0,
      "used_quota": 31250000 },
    { "budget_id": "…", "scope": "user", "window_start": 0, "window_end": 0, "tracking_since": 0,
      "used_quota": 4200000, "top_user": { "user_id": 1, "username": "root", "display_name": "Root User",
      "used_quota": 4200000, "effective_limit": 8000000, "override": true } }
  ]
}
```

- 实现放在 `controller/channel_budget.go` 旁新增 handler，复用现有按行用量服务（池子读 `__pool`，个人读第一页第一名并查其生效上限），一次遍历策略中全部已保存行；不改 revision，不写审计。
- 路由加入 `router/channel-router.go` 并补权限断言测试；契约测试与现有 `TestChannelPeriodPolicyManagementContract` 同风格，输出 fixture 供 ai-fund。
- 前端 `period-api.ts` 新增 `getChannelBudgetUsageSummary`，`channel-budget-progress.tsx` 改为从 summary 取值，不再自行请求；按行用量 Sheet 打开时仍调用原按行接口取用户分页。

## 3. new-api 前端（`web/src/features/channels`）

- `channel-user-limits-dialog.tsx`：默认页签 `policy`；页签常驻挂载策略面板（`hidden` 而非条件渲染）以保留草稿；`Dialog` 关闭前若 `formState.isDirty` 则走 `ConfirmDialog`。并发页签与个人覆盖页签结构保留，个人覆盖拆为两个 `Card` 区块，提额与并发覆盖各自独立 Sheet（不再共用 AlertDialog）。
- `channel-period-policy-panel.tsx` 拆为：`budget-table.tsx`（表、摘要条、筛选、工具栏）、`budget-editor-sheet.tsx`（分组表单，`Form/FormField/FormMessage` 渲染 zod 错误）、`schedule-list.tsx`（`Accordion` 折叠列表）、`policy-save-bar.tsx`（sticky footer）、`policy-preview-dialog.tsx`（diff 表 + 生效结果表）。RHF 表单仍是一份，`useWatch` 派生脏字段列表用于保存栏摘要与 diff。
- `channel-budget-usage.tsx`：改为 Sheet 内容组件，池子/个人两种布局；调整用量走 `ConfirmDialog`。
- `channel-period-override-editor.tsx`：改为 Sheet 内容组件，预算行由入口固定；撤销走 `ConfirmDialog`。
- 模型输入用 `components/multi-select` 候选 `selfTarget.models`；金额输入沿用 `channel-period-amount.tsx` 加 `$` 前缀与「0 = 不限」提示。
- 错误映射：`period-api.ts` 保留后端 `message`，面板顶部 `Alert` 显示原文，同时按 message 中的行名（后端错误格式「…：<行名>」）高亮对应行。
- 「本渠道」目标：编辑器写 `channel_id: 0`；读取时 `channel_id === 0 || channel_id === channelId` 都显示「本渠道」；以 `service/channel_budget_policy.go:channelBudgetSameChannelToZero` 的归一化为准。
- 测试：两份现有测试按新 DOM 改写选择器，新增保存栏/diff/确认/聚合请求次数用例；测试锚点优先 `aria-label` 与按钮文案。

## 4. ai-fund（`/root/project/ai-fund`）

- worker：`newapi_client.js` 新增 `getChannelBudgetUsageSummary`；`pool_period_limits.js` 新增 `usageSummaryToDisplay`（quota → `_display`）；`index.js` 路由 `GET /pools/:poolId/budgets/usage-summary`；fixture 追加 `usage_summary`。
- `PoolLimitAdminModal.vue`：页签 `period, schedules, usage, overrides, settings`；`mapping` 与 `limits` 合并为 `settings`；号池切换在有脏草稿时确认；成功提示改用共享 toast（新增 `ui/AppToast.vue`，全局挂载一次）。
- `PoolPeriodPolicyEditor.vue` 重写为列表 + 行内展开：`BudgetRow.vue`（行 + 展开区容器）、`BudgetRowEditor.vue`、`BudgetRowUsage.vue`（吸收 `PoolBudgetUsageEditor.vue`）、`BudgetRowOverride.vue`（吸收 `PoolPeriodOverrideEditor.vue` 的表单部分）、`PolicySaveBar.vue`、`PolicyPreviewDialog.vue`；`PoolBudgetActionEditor.vue` 复用于行级与默认动作。停用行保留在列表并弱化；草稿快照与 `expected_revision` 逻辑不变。
- `PoolPeriodMetrics.vue` 与 `PoolLimitPanel.vue` 的卡片渲染合并为 `BudgetMetricCard.vue`：两行标题（名称·范围·周期 / 模型·时段·提额标记）、大数字、进度、`pct · 余 X · 相对重置时间`、右上状态词；`PoolLimitPanel` 增加「下次时段切换」和已用尽卡的动作说明。
- 样式：沿用 `main.css` 变量与 `.input-field/.app-btn-*`；新增少量 scoped 类，不引入组件库。
- 测试：`pool-period-policy.spec.js`、`pool-budget-management.spec.js`、`pool-limit-panel.spec.js` 按新 DOM 改写，锚点用 `aria-label` 与按钮文案；worker 新增 usage-summary 契约用例。

## 5. 兼容与回滚

- 聚合接口为新增只读接口，旧接口全部保留；ai-fund 若访问到旧版 new-api（404），BFF 回退为逐行调用原接口并聚合，保证不同版本窗口内可用。
- 两端界面改动均可整体回退到当前提交；无数据迁移。
