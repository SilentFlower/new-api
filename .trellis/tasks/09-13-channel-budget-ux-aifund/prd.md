# ai-fund 号池限制界面改版（跨仓跟踪）

父任务：`.trellis/tasks/09-13-channel-budget-ux`（需求 R1、R3；验收 AC6）。业务实现在 ai-fund 仓库执行：`/root/project/ai-fund/.trellis/tasks/09-13-channel-budget-ux`。本任务只保留父任务关联与集成跟踪，不在 new-api 仓做任何代码改动。

## Goal

ai-fund 管理弹窗与本人面板按父任务原型 `research/ai-fund-redesign.html` 工作，BFF 透传聚合用量并在旧版 new-api 下回退。

## Requirements

- 父 PRD R3.1–R3.4；父 design §1、§4、§5。
- 依赖 `channel-budget-ux-newapi` 产出的 usage-summary 接口与 fixture。

## Acceptance Criteria

- [ ] AC6（父 PRD）在 ai-fund 仓验证通过并提交。
- [ ] `.trellis/spec/backend/newapi-pool-limits.md`（ai-fund 仓）同步。

## Out of Scope

- new-api 侧任何改动。
