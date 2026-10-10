# 实机验证记录（2026-09-13）

## 环境

- 后端：`CGO_ENABLED=0 go build -o /tmp/new-api-dev .` 后 `/tmp/new-api-dev --port 3999`，SQLite `/tmp/newapi-dev-data/new-api.db`，内存计数。
- 前端：`cd web && VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3999 bun run dev` → http://localhost:3001 ，root / `Passw0rd!123`。
- 数据：渠道 #1 八行预算（含时段行、停用行、超限行、提额用户），由 `/tmp/newapi-dev-data` 下脚本通过管理 API 预置。
- 截图：`/tmp/newapi-dev-data/shots/new-01` 至 `new-11`（预算表、保存栏、编辑抽屉、预览 diff、用量抽屉、提额抽屉、用量页签、个人覆盖页签、关闭确认）。

## 已走通的验收点

- AC1：五种状态徽标（生效中 / 接近上限 / 已超限 / 未到时段 / 已禁用）；停用与未到时段行弱化；「周末池子每日」显示「与「池子每日总额」共用计数」而非进度条；个人行显示最高用量用户与提额后上限；摘要条与筛选计数正确。
- AC2：切换开关后底部出现「1 处未保存改动 · 周末池子每日 · 已启用」保存栏；「预览并保存」弹出逐字段 diff 与保存后生效结果；关闭弹窗触发放弃确认；重新加载在有草稿时确认。
- AC3：打开预算页签用量类请求为 1 次 `GET /budgets/usage-summary`（另有 1 次策略、1 次候选、1 次预览）；契约测试 `TestChannelBudgetUsageSummaryContract` 与 `TestChannelPeriodPolicyManagementContract` 断言摘要覆盖全部行。
- AC4：编辑抽屉名称为空时「完成」禁用并有行内提示；整段无时段、本渠道换模型无模型均有行内错误；撤销提额、调整用量、放弃草稿均有确认框；400/409 的后端 message 追加在错误提示后。
- AC5：`go build/vet/test`（controller、service、router、dto）、`bun run typecheck`、`bun run lint`（渠道目录 0 error）、`bun test src/features/channels`（45 通过）、`bun run build`、`bun run i18n:sync`（七语言 missing/extras/untranslated 均 0）、源码缺键扫描 0；`relaykit` 独立构建通过。
- AC7：编辑抽屉「本渠道（换模型）」写入 `channel_id=0`，表格与 diff 均显示「本渠道」。
- fixture：`research/channel-budget-usage-summary.json` 由契约测试实际响应生成，含 `usage_summary`。

## 顺手修复

- 两条上游遗留缺键（`Delete model "{{name}}"? …`、`Added {{count}} models from "{{name}}"`）补入七语言，使源码缺键扫描归零。

## Check-All 修复后复验（2026-09-13）

- 修复范围：CHK-001（时段独立页签、页签顺序）、CHK-002（并发覆盖 Sheet）、CHK-003（后端 400 行定位）、CHK-004（三项缺失用例）、CHK-005（时段下次开始时间）、CHK-006（`as never` 类型绕过）。后端无改动。
- 新增文件：`lib/channel-schedule-state.ts`、`lib/channel-policy-error-rows.ts` 及各自 `__tests__`；七语言新增 5 个键（`Next start {{time}}`、`Not started · {{time}}`、`Ended`、`Revoke this concurrency override?`、`{{user}} returns to the channel default concurrency immediately.`），`Schedules` 中文值改为「时段」。
- 验证：`bun run typecheck` 通过；`bun run lint` 0 error；`bun test src/features/channels` 61 通过 0 失败（原 45 + 新增 16）；`bun run build` 通过；`bun run i18n:sync` 七语言 missing/extras/untranslated 均 0，键数 5962 一致；渠道目录源码缺键扫描 0；`format:check` 通过。
