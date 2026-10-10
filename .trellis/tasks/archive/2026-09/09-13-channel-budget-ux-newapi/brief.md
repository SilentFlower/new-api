# Brief — new-api 渠道限额界面改版（预算表、行上抽屉、常驻保存栏）

## Goal

- 「用户限制状态」弹窗按已确认原型重做：一张预算表看懂状态与用量，常驻保存栏完成预览与保存，编辑 / 用量 / 提额从行上以 Sheet 打开；新增聚合用量接口消灭逐行请求。

## Scope

- 后端：新增只读接口 `GET /api/channel/:id/budgets/usage-summary`（DTO、handler、路由、权限测试、契约测试与 fixture 输出）。
- 前端 `web/src/features/channels`：默认页签改为预算，页签顺序 预算 / 时段 / 用量 / 个人覆盖 / 并发；预算表带状态徽标（生效中 / 接近上限 / 已超限 / 未到时段 / 已停用）、摘要条与筛选、共用计数文字说明；时段折叠列表；编辑 Sheet 分组表单与行内校验、模型多选；用量 Sheet 与提额 Sheet；底部保存栏与逐字段 diff 预览；脏草稿在关闭 / 重新加载 / 放弃时确认，切页签保留草稿；个人覆盖拆为预算提额与并发覆盖两个区块；撤销 / 调整确认；后端 message 透传。
- 「本渠道换模型」前端统一写 `0`。
- 两份现有测试改写并新增用例；六语种 i18n 同步与缺键扫描。

## Non-Goals

- ai-fund 任何改动。
- 预算 v2 契约语义、计数身份、降级链路、迁移与旧接口。
- 移动端专门布局（只保证不横向溢出页面）。

## Key Decisions

- 原型 `research/channel-budget-redesign.html` 即设计规格，文字与原型不一致时以原型为准。
- 行状态由前端按统一规则推导（停用 > 未到时段 > 已超限 > 接近上限 ≥ 85% > 生效中）；个人行上限与剩余按同一生效上限计算。
- 共用计数按计数身份（window + 整段时段 + models）在前端分组标注，如实反映日/周窗口不区分时段。
- 保存动线改为「预览并保存」一步，预览为逐字段 diff + 生效结果；预览与 PUT body 一致、revision CAS 与 409 处理不变。
- 唯一后端改动是新增只读聚合接口；旧接口全部保留。
- 后端已在保存时把本渠道 id 归一化为 `0`（`channelBudgetSameChannelToZero`），前端只做一致性修正。

## Key Context

- 入口：`components/data-table-row-actions.tsx` 行菜单「用户限制状态」→ `channel-user-limits-dialog.tsx`。
- 重写目标：`channel-period-policy-panel.tsx`、`channel-user-limits-dialog.tsx`；拆出 `components/dialogs/budget/` 子组件；`channel-budget-progress.tsx` 删除，`channel-budget-usage.tsx` 与 `channel-period-override-editor.tsx` 保留供抽屉复用。
- 类型与 API：`period-types.ts`、`period-api.ts`、`lib/channel-budget-status.ts`。
- 后端落点：`controller/channel_budget.go`、`router/channel-router.go`（同 `GET /:id/budgets/:budget_id/usage` 的 `authz.ChannelRead`）、`dto/channel_period_policy.go`、`controller/channel_period_policy_test.go`。
- 约束：遵循 build 分支上游同步友好定制指南；Base UI 组合规范；i18n 七语言门禁；现有测试锚点见 `research/newapi-ui-audit.md` §6。
- 本地实例：`/tmp/new-api-dev` 端口 3999，前端 `bun run dev` 端口 3001，已预置 8 行策略。

## Risks / Deferred

- 两份现有测试以按钮文案与 `querySelector` 定位，重写后需同步改写，注意不要丢失 revision CAS、409、新行不请求用量等既有契约。
- 聚合接口个人行只返回用量最高用户，行内详情仍走按行接口；128 行上限下需确认一次响应体积可接受。
- ai-fund 依赖本任务的接口与 fixture，需两仓同版本部署；BFF 回退逻辑在 ai-fund 子任务中实现。

## Acceptance

- AC1、AC2、AC4、AC7（父 PRD）在本地 new-api 上逐条走通。
- AC3：128 行策略打开预算页签时用量类请求恰好 1 次；契约测试断言 summary 覆盖全部已保存行。
- AC5：`go build ./... && go vet ./... && go test ./controller ./service ./router`、`bun run typecheck`、`bun run lint`、`bun test src/features/channels`、`bun run build`、i18n 同步与缺键扫描全部通过。
- fixture `research/channel-budget-usage-summary.json` 由契约测试实际响应生成。

## Next Step

- implement.md 步骤 1–8 已完成（含 Check-All CHK-001–006 修复）；重检通过后进入 `trellis-update-spec`（补记 usage-summary 接口与新页签结构），再由 `trellis-push` 生成提交计划。
