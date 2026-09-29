# Implement — 渠道按模型预算、按模型降级与池子已用金额调整

按顺序执行；每步后跑对应验证。两仓各自提交，new-api 在前。

## A. new-api 后端

1. `dto/channel_period_policy.go`：`ChannelPeriodModelBudget`（池子/个人四项额度 + `Fallback` + `CreatedAt`）、`Config.ModelBudgets`、`Metric.Model`。
2. `service/channel_period_rules.go`：`NormalizeChannelPeriodConfig` 归一化/校验 `model_budgets`（唯一、范围、`created_at` 沿用/重算、同渠道降级目标 ≠ 本模型、非 nil 输出）。
3. `service/channel_period_policy.go`：`GetChannelPeriodPolicy` 读出后 nil→空切片；`validateChannelPeriodStoredPolicy` Revision==0 约束；`SaveChannelPeriodPolicy` 模型级 `fallback.channel_id == self` 归一为 0、指定渠道存在性校验。
4. `service/channel_period_usage.go`：`channelPeriodModelCounters`；`recordChannelPeriodUsage` 增加 `modelName` 并追加模型 key（Redis+内存）；新增 `SetChannelPoolQuotaUsage` 与 Lua `channelPeriodUsageSetScript`。
5. `service/channel_user_quota_usage.go`：`RecordChannelUserQuotaUsage` / `RecordRelayChannelUserQuotaUsage` 增加 `modelName`；更新调用方 `relay`（`OriginModelName`）、`service/task_billing.go:267`（`task.OriginModelName`）、测试中的调用。
6. `service/channel_period_guard.go`：内部 `getChannelPeriodStatus(..., modelName)` 产出模型级池子/个人指标（上限 > 0 时）；`GetChannelPeriodStatusForModel`；`CheckChannelPeriodLimits(..., modelName)`；`CheckSelectedChannelPeriodLimits` 传 `ContextKeyOriginalModel`。
7. `service/channel_limit_fallback.go`：`SelectChannelLimitFallback(config, metric)`；`controller/channel_limit_fallback.go`：用其替换 `policy.Config.Fallback` 读取与 `Enabled` 门禁，`ChannelLimitFallbackInfo.Model` 赋值（`relay/common` 结构体加字段）。
8. `controller/channel_period_policy.go` + `router/channel-router.go`：两个 PUT handler、审计；`GetChannelPeriodPolicyTargets` 不再排除本渠道；`router/channel_router_test.go` 权限断言。
9. 测试（testify，表驱动）：
   - `service/channel_period_policy_test.go`：归一化（唯一/范围/created_at 沿用/同渠道目标 ≠ 本模型/自指归一为 0/指定渠道不存在，AC10）、旧存储无字段兼容、Revision==0 约束。
   - `service/channel_period_guard_test.go`（新）：AC1 整体+模型并行判定（Redis via miniredis + 内存）、AC2 tracking_since、请求前只产出命中模型指标（AC3）、AC8 个人按模型上限与渠道级个人限独立、`SetChannelPoolQuotaUsage` 清零/设满即时生效且不影响其他计数（AC4）、`SelectChannelLimitFallback` 优先级表（模型级/策略级/无）。
   - `controller/channel_period_policy_test.go`：PUT 用量 400 分支（AC5）、未知字段 400（AC7）、审计、targets 含本渠道。
   - `controller/channel_limit_fallback_boundary_test.go`：新增 AC9 场景（同渠道模型降级成功并按目标计费、整体耗尽走策略级、目标预算耗尽 429 不二跳、模型未配降级走策略级）；既有场景全绿。

验证：
```bash
gofmt -l dto service controller relay router && go build ./... && go vet ./service/ ./controller/ ./relay/ ./router/
go test ./service/ ./controller/ ./relay/... ./router/
cd relaykit && GOWORK=off go build ./... && cd ..
```

## B. new-api 前端

10. `period-types.ts`（预算行 schema 含个人上限与 `fallback`、metric.model）、`period-api.ts`、`channel-period-policy-panel.tsx`（模型预算行：四项额度 + 模型级降级选择器（目标渠道含 "This channel"）+ 用量调整表单；策略级选择器排除本渠道）、`channel-user-limits-dialog.tsx` 指标标签带模型。
11. i18n：`bun run i18n:sync`，补 zh 及其余语种。
12. 测试：`channel-period-policy-panel.test.tsx` 增加模型预算增删与用量调整提交断言。

验证：
```bash
cd web && bun run lint && bun run eslint && bun run i18n:lint && bun x vitest run src/features/channels && bun run build
```

## C. ai-fund worker

13. `newapi_period_policy.js`（`model_budgets` 全字段、metric.model、指标上限 388）、`newapi_client.js`、`pool_period_limits.js`（config/view 转换含个人上限与 fallback + `setPoolPeriodPoolUsage`；targets 保留本渠道）、`pool_limits.js`（降级 reasons 每条带 `model` 与 `target_model`，模型级优先策略级兜底）、`index.js` 路由。
14. fixture `channel-period-contract.json` 更新；`pool_period_limits.test.js` / `pool_limits.test.js` 增加模型预算往返、用量调整（校验、审计、错渠拒绝）。
15. `.trellis/spec/backend/newapi-pool-limits.md` 增加两个端点与字段。

验证：
```bash
cd /root/project/ai-fund/worker && node --test src/*.test.js
```

## D. ai-fund 前端

16. `api/index.js`、`PoolPeriodPolicyEditor.vue`（模型预算行：四项金额 + 降级开关/目标渠道（本渠道/其他）/目标模型）、新 `PoolPeriodUsageEditor.vue`、`PoolLimitAdminModal.vue` 挂载、`PoolLimitPanel.vue`（两组模型指标标签、按原因显示 `<model> → <target_model>`）。
17. 组件测试（vitest）：策略编辑器模型预算行、用量编辑器提交参数。

验证：
```bash
cd /root/project/ai-fund/frontend && bun run test:components && bun run build
```

## E. 收尾

18. new-api `.trellis/spec/backend/channel-period-budget-fallback.md` 补充模型预算与用量调整契约（trellis-update-spec）。
19. Check-All → 两仓分别提交（new-api message 带 `[build]`）。

## 风险与回滚点

- 步骤 5 改动函数签名跨 relay/service/controller，编译期即可发现遗漏调用。
- 步骤 4 Lua 脚本变更：先跑 miniredis 测试再部署；回滚只需回退代码，模型 key 自然过期。
- 两仓需同版本部署（design §7）。
