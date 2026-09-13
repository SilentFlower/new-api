# new-api 渠道限额界面改版（预算表、行上抽屉、常驻保存栏）

父任务：`.trellis/tasks/09-13-channel-budget-ux`（需求 R1、R2；验收 AC1–AC5、AC7）。原型：父任务 `research/channel-budget-redesign.html`。

## Goal

「用户限制状态」弹窗按原型重做：预算表带状态徽标与摘要筛选，时段折叠列表，编辑 / 用量 / 提额从行上以 Sheet 打开，底部常驻保存栏与逐字段 diff 预览，个人覆盖拆成预算提额与并发覆盖；新增聚合用量接口消灭逐行请求。

## Requirements

- 父 PRD R1 全部、R2.1–R2.4；父 design §1–§3。
- 后端只新增 `GET /api/channel/:id/budgets/usage-summary` 及其路由、权限测试、契约测试与 fixture 输出；不改任何现有接口与语义。
- 前端改动限于 `web/src/features/channels`（新增子组件放 `components/dialogs/budget/`），i18n 七语言同步。
- 「本渠道换模型」后端保存时已把 `channel_id == 本渠道 id` 归一化为 `0`（`service/channel_budget_policy.go:313 channelBudgetSameChannelToZero`，已复核）；前端改为直接写 `0`，读取时 `0` 或本渠道 id 均显示「本渠道」。

## Acceptance Criteria

- [ ] AC1、AC2、AC4、AC7（父 PRD）在本地 new-api 上逐条走通。
- [ ] AC3：以 128 行策略打开预算页签，网络面板中用量类请求恰好 1 次；契约测试断言 summary 覆盖全部已保存行。
- [ ] AC5：`go build ./... && go vet ./... && go test ./controller ./service ./router`、`bun run typecheck`、`bun run lint`、`bun test src/features/channels`、`bun run build`、i18n 同步与缺键扫描全部通过。
- [ ] `research/channel-budget-usage-summary.json` fixture 由契约测试实际响应生成，供 ai-fund 子任务。

## Out of Scope

- ai-fund 任何改动；预算 v2 契约与计数语义；移动端专门布局。
