# Implement — 预算 v2 号池限制管理 UI/UX 优化（父任务）

父任务不直接实现，按顺序推进两个子任务并做集成验收。

## 步骤

1. [ ] `channel-budget-ux-newapi`：聚合接口 → 前端结构拆分 → 保存动线 → 行上抽屉 → 个人覆盖拆分 → 测试与 i18n → 生成 usage-summary fixture。
2. [ ] `channel-budget-ux-aifund`（在 ai-fund 仓执行）：BFF 透传与回退 → 管理弹窗重写 → 本人面板卡片 → 测试与构建 → 规范同步。
3. [ ] 集成验收：本地 new-api（`/tmp/new-api-dev`，端口 3999）+ ai-fund `wrangler dev` 指向本地，走通 AC1–AC7；两仓同版本发布。

## 验证

```bash
# new-api
go build ./... && go vet ./... && go test ./controller ./service ./router -count=1
cd web && bun run typecheck && bun run lint && bun test src/features/channels && bun run build && bun run i18n:sync
# ai-fund
cd /root/project/ai-fund/worker && node --test src/*.test.js && node --check src/index.js
cd /root/project/ai-fund/frontend && npm run test:components && npm run build
```

## 回滚点

- new-api：聚合接口与前端改动在同一提交序列，整体 revert；不含迁移。
- ai-fund：BFF 回退逻辑保证旧版 new-api 可用；界面改动整体 revert。
