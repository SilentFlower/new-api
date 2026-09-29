# ai-fund 迁移到预算 v2 契约

父任务：`.trellis/tasks/09-12-channel-budget-unification`（需求 R5；验收 AC9）。依赖 `09-12-channel-budget-v2-contract` 契约冻结与其产出的 fixture。仓库：`/root/project/ai-fund`。

## Goal

ai-fund 管理端与本人面板按 v2 预算契约工作：策略编辑器为预算表 + 时段列表；用量调整与个人特批按行；本人面板指标标签含模型/时段，降级提示含触发行与目标。

## Requirements

- R5.1-R5.3（父 PRD）；父 design §9 的路由与文件落点。
- 删除对 new-api 已下线接口的调用：`user-daily-quota*`、`user-weekly-quota*`、`period-rules/*`、`PUT /api/channel/` 写日/周列。

## Acceptance Criteria

- [ ] AC9：`cd worker && node --test src/*.test.js && node --check src/index.js`、`cd frontend && npm run build` 通过；fixture 与 new-api v2 实际响应一致；Vue 真实挂载测试覆盖预算表增删改与按行用量/特批。
- [ ] 规范 `.trellis/spec/backend/newapi-pool-limits.md` 更新为 v2。

## Out of Scope

- new-api 侧任何改动。
