# 实施计划

## 1. Compact 降级准备

- [x] 为 `cloneRelayRequest` 增加 `OpenAIResponsesCompactionRequest` 深拷贝，防止目标预检修改正式请求。
- [x] 调整 `preflightChannelPeriodLimits`，让 Compact 使用原生透传准备完成能力、价格和周期预算预检，普通请求路径保持不变。
- [x] 移除 Compact 的无条件提前返回，仅允许目标解析为来源渠道的额度动作继续执行。
- [x] 目标上下文建立后，对 Compact 使用目标路由模型再次执行能力、价格和预算预检。
- [x] Compact 跳过普通请求转换预检，使用 `sjson.SetBytes` 只替换原始 body 的顶层 `model`。
- [x] 保持现有单跳、Token 指定渠道、上游已开始和计费已创建等副作用保护。

## 2. Compact 模型身份

- [x] 调整 `PrepareResponsesCompactPassthrough`，使用 `RoutingModel()` 作为本次基础模型。
- [x] 保留 `OriginModelName`，同步目标模型到 `UpstreamModelName` 和请求 DTO，继续禁用模型映射。
- [x] 核对 V1 path、历史 body bridge、V2 HTTP 的出站路径和 usage 结算均未改变。

## 3. 模型预算归属

- [x] 调整 `RecordRelayChannelUserQuotaUsage`，使用 `RoutingModel()` 记录模型预算。
- [x] 保持实际渠道、整体额度、个人日/周额度和无模型预算行的累计逻辑不变。
- [x] 确认普通模型映射仍按路由模型而非上游别名累计。

## 4. 回归测试

- [x] 重写 Compact 完整 Relay 场景：三种 HTTP 模式分别覆盖未超限原样透传和来源超限同渠道降级成功。
- [x] 断言降级后的上游 body 只改变顶层模型，未知字段、`0`、`false` 和加密内容保留。
- [x] 断言目标模型计费、唯一消费日志、完整 `channel_limit_fallback` 审计、用户和 Token 唯一扣款。
- [x] 增加 Compact 跨渠道配置仍 429、目标预算耗尽、能力关闭时零副作用的场景。
- [x] 在普通同渠道模型降级测试中断言来源模型计数不增加、目标模型按结算额度增加。
- [x] 增加无降级和普通模型映射的归属保护，避免把预算写到上游别名。
- [x] 更新现有边界测试中 `compact_no_fallback` 的含义，使其明确保护跨渠道不开放，而不是阻止全部 Compact 降级。

## 5. 验证命令

- [x] `gofmt -w controller/channel_limit_fallback.go controller/relay_attempt.go relay/responses_compact_passthrough.go service/channel_user_quota_usage.go service/channel_budget_usage.go controller/channel_limit_fallback_compact_test.go controller/channel_limit_fallback_model_test.go controller/channel_limit_fallback_boundary_test.go controller/channel_model_usage_test.go controller/channel_period_policy_test.go controller/relay_attempt_responses_test.go service/channel_model_usage_test.go`
- [x] `go test ./controller -run 'TestChannelLimitFallback(UsesSameChannelCompactTarget|PreservesCompactPassthrough|SameChannelModelBilling|FullRelayBoundaries|TargetUserBudgets)|TestResponsesCompactionCloneIsolatesModelAndRawFields' -count=1`
- [x] `go test ./service -run 'TestRecordRelayChannelUserQuotaUsageUsesRoutingModel' -count=1`
- [x] `go test ./controller ./relay ./service -count=1`
- [x] `go test ./... -count=1`
- [ ] 若 `relaykit/` 被意外影响，执行 `cd relaykit && GOWORK=off go build ./...`；正常文件范围不应触及该模块。

## 6. 检查与规格同步

- [x] 运行 `trellis-check-all`，复核协议、计费、预算累计、单跳和副作用边界。
- [ ] 更新 `.trellis/spec/backend/channel-period-budget-fallback.md`，把模型累计归属改为目标路由模型，并记录 Compact 同渠道例外。
- [ ] 更新 `.trellis/spec/backend/relay-alpha-search-compact.md`，明确未降级使用原始基础模型、同渠道降级使用目标路由模型。

## 7. 回滚点

- [ ] 如 Compact 路由回归，先回退降级入口和原始 body 模型替换，恢复 Compact 429 保护。
- [ ] 如预算归属回归，独立回退 `RecordRelayChannelUserQuotaUsage` 的模型名选择；不自动迁移已经写入的周期计数。
