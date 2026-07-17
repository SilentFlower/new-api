# 规范化现有 Build 特有 Feature：首批实施计划

## 1. 固化行为基线

- [ ] 新增 `middleware/distributor_http_contract_test.go`，以 Gin HTTP 行为断言 Token 模型拒绝、错误 code、Compact 基础模型和 detector context。
- [ ] 将现有通用 helper 测试拆成 Responses WebSocket 领域行为测试，保留指定渠道、模型权限、亲和性和当前渠道能力断言。
- [ ] 先运行新增测试与现有 Compact/Alpha/WS 测试，确认结构迁移前全部通过。

验证：

```bash
go test ./middleware ./controller ./relay ./router ./service -count=1
```

回滚点：仅新增测试，不修改生产行为。

## 2. 隔离 Responses WebSocket 渠道选择

- [ ] 新建 `middleware/responses_websocket_channel.go`，复制当前 WS 所需的模型权限、指定渠道、亲和性、随机选择和 context 初始化逻辑。
- [ ] 使用 Responses WebSocket 领域函数名，不暴露通用插件或策略接口。
- [ ] 修改 `controller/responses_websocket.go`，仅替换三处渠道选择/校验调用。
- [ ] 保持基础模型用于 Token 权限、亲和性、首次选择、后续 turn 校验和 failover。
- [ ] 保持 Advanced Custom `/v1/responses` 原生 route 约束。

验证：

```bash
go test ./middleware ./controller -run 'ResponsesWebSocket|Compact|Distributor' -count=1
```

## 3. 恢复 HTTP Distribute 上游友好结构

- [ ] 将 `middleware.Distribute` 恢复为首批功能接入前的顺序式控制流。
- [ ] 内联保留当前 Token 权限、指定渠道、错误 code、亲和性、auto group、随机选渠和 context 初始化行为。
- [ ] 保留 `getModelRequest` 中 Compact detector 的单一窄接入，所有 Compact 模式继续返回基础模型。
- [ ] 删除不再被 HTTP/WS 共用的 `SelectAndSetupChannel`、`ValidateTokenModelAccess`、`ChannelSupportsRequest` 和 `abortWithDistributorError`。
- [ ] 用 HTTP 契约测试和 WS 专用测试证明行为不变。

验证：

```bash
go test ./middleware ./controller -count=1
git diff --stat -- middleware/distributor.go middleware/responses_websocket_channel.go
```

审查门：`middleware/distributor.go` 不得保留因 WS 共享抽取产生的大块移动；每个剩余新增区块必须能用一句话说明必要性。

## 4. 移出 Relay attempt 与 Alpha Search 计费实现

- [ ] 新建 `controller/relay_attempt.go`，原样迁移请求快照、attempt 重置、普通计费准备和最终失败退款函数。
- [ ] 新建 `controller/relay_alpha_search.go`，原样迁移 Alpha Search 冻结工具计费函数。
- [ ] 保持函数签名、调用顺序、错误类型、skipRetry/noRecordErrorLog、quota clamp 和 BillingSession 行为不变。
- [ ] `controller/relay.go` 只删除迁移后的函数体和无用 import，不重排主循环。
- [ ] 现有 `controller/relay_retry_test.go` 继续保护 Alpha Search 预扣、饱和拒绝、最终失败退款和 Compact retry 模型重置。

验证：

```bash
go test ./controller ./service -run 'AlphaSearch|FinalizeMainRelayBilling|ResponsesCompactV2Retry|BillingSession' -count=1
git diff --stat -- controller/relay.go controller/relay_attempt.go controller/relay_alpha_search.go
```

## 5. 首批完整回归

- [ ] 运行首批相关包回归，与规划阶段基线比较。
- [ ] 运行 Compact/Alpha/WS 定向 race。
- [ ] 运行全仓测试和 vet。
- [ ] 执行 `git diff --check`。
- [ ] 检查 `git diff --stat`，确认上游核心文件冲突面下降且没有无关格式化、重命名或注释整理。
- [ ] 逐项核对 HTTP JSON、SSE、WS 原始 payload、计费、退款、错误日志和渠道重试契约。

最终验证：

```bash
go test ./controller ./dto ./middleware ./relay ./relay/channel/openai ./relay/channel/codex ./router ./service -count=1
go test -race ./relay -run 'AlphaSearch|ResponsesCompact|ResponsesWebSocket' -count=1
go test -race ./controller -run 'AlphaSearch|ResponsesCompact|ResponsesWebSocket' -count=1
go test -race ./middleware -run 'Distributor|ResponsesCompact|ResponsesWebSocket' -count=1
go test ./... -count=1
go vet ./...
git diff --check
```

真实 OpenAI/sub2api 联调为非阻塞补充项；未执行时必须在交付说明中明确记录。

## 6. Review Gates

- [ ] Gate A：新增行为契约测试在结构调整前通过。
- [ ] Gate B：WS 独立选择实现与恢复后的 HTTP `Distribute` 同时通过模型权限和亲和性测试。
- [ ] Gate C：Relay helper 迁移后，Alpha/Compact 计费、退款和重试测试结果不变。
- [ ] Gate D：最终 diff 中原有上游文件只保留必要窄接入，无行为修复混入。

## 7. 后续批次

首批完成后，仅更新全量 Feature 清单和优先级，不在同一实现提交继续治理视觉辅助、Claude WebSearch、Dashboard、Token 迁移或其他领域。
