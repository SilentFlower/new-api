# Implement — 预算内核收敛

## 步骤

1. [x] `dto`：阶段一不新增指标 JSON 字段（契约逐字节不变）；`ChannelPeriodBlock` 增加内部 `row`（`json:"-"`）。
2. [x] `service/channel_budget_plan.go`：类型 + `buildChannelBudgetPlan` + 派生 id + 分组/身份/优先级解析；`channel_budget_plan_test.go` 覆盖旧 key 映射、同身份共用、implicit 行、优先级、模型精确匹配。
3. [x] `service/channel_budget_usage.go`：身份/hash/key 映射 + 逐条目 Lua + 记录 + 读取；内存/Redis 等价测试。
4. [x] `service/channel_budget_guard.go`：`evaluateChannelBudgets` 分组优先级、指标输出、错误码；旧 `channel_period_guard.go` / `channel_period_usage.go` 删除。
5. [x] `service/channel_period_rules.go`：`resolveChannelPeriodSources` 改为预算计划的 v1 投影（保留签名，供 v1 预览与旧覆盖校验），`PreviewChannelPeriodPolicy` 无需改动。
6. [x] 新增 `RecordChannelUserModelQuotaUsage`，旧签名保留并委托；`RecordRelayChannelUserQuotaUsage` 传 `OriginModelName`；上游 `service/task_billing.go` 仅改一行传 `task.Properties.OriginModelName`。
7. [x] 降级：`SelectChannelLimitFallback` + `controller/channel_limit_fallback.go` 改用行级选择。旧 relay 日/周独立检查与 `preflightChannelPeriodLimits` 的直接调用保留：目标渠道可能 revision=0，此时统一判定早返回，仍需旧检查兜底；删除推迟到阶段二迁移后（届时不再有 revision=0 渠道）。
8. [x] `ResolveChannelUserEffectiveLimits` / `ReplaceChannelUserLimitOverride` 通过 v1 投影取基线，无需改动。
9. [x] 全量验证：service/controller/model/relay 测试、web 渠道测试、gofmt、vet。

## 验证

```bash
gofmt -l service controller relay dto
go build ./... && go vet ./service/ ./controller/ ./relay/... ./dto/
go test ./service/ ./controller/ ./model/ -run 'ChannelPeriod|ChannelUser|ChannelLimitFallback|RelayAttempt|ChannelBudget'
go test ./... 2>&1 | grep -E "^(FAIL|ok)" | grep -v ok
cd web && bun test src/features/channels
cd /root/project/ai-fund/worker && node --test src/*.test.js && git diff --quiet -- src/fixtures/channel-period-contract.json
cd relaykit && GOWORK=off go build ./...
```

## 风险文件 / 回滚点

- `service/channel_budget_usage.go` Lua 脚本：先 miniredis 全绿再合入；回滚只需回退代码，key 未变。
- `controller/channel_limit_fallback.go`：全链路测试 `channel_limit_fallback*_test.go` 必须全绿。
