# Implement — 渠道限额统一为预算列表（父任务执行图）

父任务不直接实现；按子任务顺序推进，每个子任务在其目录内维护自己的 `implement.md`。

## 0. 前置

- [ ] 提交第零步（int64 上界）改动：new-api 与 ai-fund 各一提交（需用户确认后走 `trellis-push`）。
- [ ] 丢弃已被替代的 `09-11-channel-pool-model-budget-override`：`python3 ./.trellis/scripts/task_intent.py discard --task .trellis/tasks/09-11-channel-pool-model-budget-override`（需用户确认）。

## 1. budget-core（R1、R2；AC1、AC3、AC7）

见 `.trellis/tasks/09-12-channel-budget-core/implement.md`。完成标准：`go build ./... && go vet ./service/ ./controller/ ./relay/... && go test ./service/ ./controller/ ./model/ -run 'ChannelPeriod|ChannelUser|ChannelLimitFallback|RelayAttempt'`，`cd web && bun test src/features/channels`，`cd /root/project/ai-fund/worker && node --test src/*.test.js` 全绿且 fixture 未改。

## 2. budget-v2-contract（R3、R4、R6；AC2、AC4、AC5、AC6、AC8、AC10）

见 `.trellis/tasks/09-12-channel-budget-v2-contract/implement.md`。完成标准：后端全量 `go test ./...`（sqlite）+ 专用 MySQL/PostgreSQL DSN 跑 `model/` 迁移测试；`cd web && bun run typecheck && bun run lint && bun test src/features/channels && bun run i18n:sync`；`cd relaykit && GOWORK=off go build ./...`。

## 3. budget-ai-fund（R5；AC9）

见 `.trellis/tasks/09-12-channel-budget-ai-fund/implement.md`。完成标准：`cd worker && node --test src/*.test.js && node --check src/index.js`，`cd frontend && npm run build`，fixture 与 new-api 响应一致。

## 4. 集成验收（父任务）

- [ ] 用 new-api sqlite 环境导入含 v1 策略、渠道列、两张覆盖表数据的快照，启动后核对 AC5；重复启动 revision 不变。
- [ ] 按 AC2、AC4 手工走通 `gpt-6-astra` → `gpt-6-mini` 同渠道降级。
- [ ] 按 AC10 演练回滚。
- [ ] 规范更新：`channel-period-budget-fallback.md` 重写为预算契约；`channel-user-daily-quota.md`、`channel-user-weekly-quota-and-overrides.md` 收缩为并发/废弃说明；ai-fund `newapi-pool-limits.md` 同步；索引表更新。

## 回滚点

- 阶段一：回退代码即可。
- 阶段二：回退代码 + 恢复三表备份（design.md §8）。
