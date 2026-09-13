# Implement — new-api 渠道限额界面改版

## 步骤

1. [x] 复核本渠道换模型归一化；后端 DTO + `GetChannelBudgetUsageSummary` + 路由 + 权限测试 + 契约测试与 fixture 输出。
2. [x] `period-types.ts` / `period-api.ts` / `lib/channel-period-label.ts`：summary 类型与 API、状态推导、计数身份分组及表驱动测试。
3. [x] `budget/` 子组件：表、摘要筛选、时段折叠列表、编辑 Sheet（分组表单 + 行内校验 + 模型按钮组）；面板改为组合层，删除旧 progress 逐行请求。
4. [x] 保存动线：`policy-save-bar.tsx` + `policy-preview-dialog.tsx`（diff + 生效结果）；脏草稿确认（关闭 / 重新加载 / 放弃）；页签常驻挂载。
5. [x] 用量 Sheet 与提额 Sheet；个人覆盖页签拆分；撤销 / 调整确认；后端 message 透传与行高亮。
6. [x] 测试改写与新增；i18n 同步与缺键扫描；`typecheck / lint / test / build`。
7. [x] 本地 new-api 实机走 AC1–AC5、AC7，截图记入 `research/`。
8. [x] Check-All 修复：CHK-001 时段独立页签且页签顺序 预算/时段/用量/个人覆盖/并发（面板单次挂载按 `section` 切换）；CHK-002 并发覆盖改 Sheet + 撤销 ConfirmDialog；CHK-003 后端 400 按行名定位（行高亮、行下原因、编辑抽屉原因）；CHK-004 补脏草稿关闭确认、共用计数说明、本渠道写 0 三项用例；CHK-005 时段列与时段列表显示下次开始时间（`lib/channel-schedule-state.ts`）；CHK-006 编辑抽屉 `set` 改用 `Path`/`PathValue` 约束。

## 验证

```bash
go build ./... && go vet ./... && go test ./controller ./service ./router -count=1
CHANNEL_PERIOD_CONTRACT_OUTPUT=.trellis/tasks/09-13-channel-budget-ux-newapi/research/channel-budget-usage-summary.json go test ./controller -run 'TestChannelPeriodPolicyManagementContract$' -count=1
cd web && bun run typecheck && bun run lint && bun test src/features/channels && bun run build && bun run i18n:sync
cd relaykit && GOWORK=off go build ./...
```

## 风险文件 / 回滚点

- `channel-user-limits-dialog.tsx`、`channel-period-policy-panel.tsx`：整体重写，回滚为 revert 本任务提交。
- `router/channel-router.go`：只新增一条只读路由。
