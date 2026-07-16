# OpenAI Compact V1/V2 实施计划

## 1. 协议检测与共享状态

- [ ] 在 `relay/constant` 定义 Compact 模式枚举，并在 `constant/context_key.go` 增加请求级 context key。
- [ ] 在 `relay/helper` 实现 HTTP/WS 共用 detector：path、transport、stream、`compaction_trigger`、beta feature 精确匹配。
- [ ] 为 detector 增加表驱动测试：多 header、多 feature、错误 JSON、非数组 input、普通 Responses、V1 path、V1 body bridge、V2 HTTP、V2 WS。
- [ ] 在 `RelayInfo` 增加 Compact 模式与客户端/上游流式语义字段，并提供清晰领域方法，避免散布枚举判断。

验证：

```bash
go test ./relay/helper ./relay/constant ./relay/common
```

回滚点：此阶段不改路由和上游行为，普通请求应完全不变。

## 2. HTTP 分发、模型映射与计费标记

- [ ] 在 `middleware.getModelRequest` 中调用 detector，并为所有 Compact 模式追加本地价格后缀。
- [ ] 保证 token 模型限制、渠道能力、渠道亲和性和 retry model 使用相同的 Compact 选择模型。
- [ ] 修改 `GenRelayInfoResponses`/`GenRelayInfoResponsesCompaction` 读取 Compact 模式。
- [ ] 修改 `ModelMappedHelper`：所有 Compact 模式都先去除本地后缀再映射，但 V2 保持普通 Responses URL。
- [ ] 补充模型映射、循环映射、上游模型计费开关、Compact wildcard price 和后缀不泄漏测试。

验证：

```bash
go test ./middleware ./relay/helper ./relay/common ./setting/ratio_setting
```

## 3. V1 canonical 请求与安全请求头

- [ ] 按锁定 OpenAI Codex commit 校准 V1 canonical allowlist。
- [ ] 为 Codex、OpenAI-compatible、Azure 分别确认 `previous_response_id`、cache options/retention 的兼容策略并固化测试。
- [ ] 增加 Codex 安全元数据 header allowlist；保留 Header Override 的最终优先级，禁止客户端认证透传。
- [ ] 保持 V1 unary JSON handler、usage/cache usage 和文本计费不变。
- [ ] 补齐 OpenAI/Codex/Azure URL、body、header、响应和计费回归。

验证：

```bash
go test ./dto ./relay ./relay/channel/openai ./relay/channel/codex ./relay/helper
```

## 4. V2 原生 HTTP/SSE

- [ ] 让裸 `/v1/responses` V2 保持原始 path、`stream:true` 和 Responses 字段。
- [ ] 默认转发 `x-codex-beta-features` 和确认过的 Codex turn/session 元数据。
- [ ] 在 Responses SSE handler 中增加 V2 终态观测，不重新 marshal 原始 item。
- [ ] 确认 `encrypted_content`、未知 compaction 字段和 `response.completed` 原样写回。
- [ ] V2 使用 Compact 价格模型结算，普通 Responses 仍使用基础模型价格。
- [ ] 覆盖客户端取消、上游 EOF、无 completed、零/多个 compaction item 和已写流后不重试。

验证：

```bash
go test ./controller ./middleware ./relay ./relay/channel/openai ./service
```

## 5. 历史 body-signal V1 bridge

- [ ] 在普通 `/responses` 入口将非 V2 `compaction_trigger` 标记为 V1 body bridge，并在上游前切换 Compact path/body。
- [ ] 实现请求级 Compact SSE bridge writer，独立于 JSON 非流式保活。
- [ ] 实现注释心跳、停止同步、业务响应提交标记和心跳字节排除。
- [ ] 将 unary JSON output 合成为 `response.output_item.done` + `response.completed`。
- [ ] 心跳提交后把失败转换为 `response.failed`；提交前保留真实 HTTP 错误。
- [ ] 覆盖快速成功、慢成功、快速失败、心跳后失败、非法 JSON、缺失 id/usage、取消、并发写和 race。

验证：

```bash
go test ./controller ./relay ./relay/helper ./service
go test -race ./relay ./relay/helper
```

回滚点：bridge 使用独立 marker 和 writer，不改变原生 V1/V2 handler。

## 6. Responses WebSocket 路由与首帧分发

- [ ] 注册 `GET /v1/responses`，复用认证、性能检查和请求限流，但不使用 Upgrade 前的 `Distribute`。
- [ ] 新增 `RelayResponsesWebSocket` 控制器：Upgrade 校验、读限制、首帧超时、JSON/type/model 校验。
- [ ] 从 `middleware.Distribute` 抽取可复用的 token 模型权限和首次渠道选择能力，HTTP 与 WS 共用。
- [ ] 首帧识别 V2 Compact、追加选择/计费后缀、初始化 Channel context 和 RelayInfo。
- [ ] Upgrade 前后的错误分别使用 OpenAI HTTP JSON 与 WS error/close code。

验证：

```bash
go test ./router ./controller ./middleware
```

## 7. new-api 到 sub2api 的上游 WebSocket

- [ ] 为 Responses WS 构造 OpenAI-compatible、Codex 和显式 Advanced Custom 上游 URL，安全转换 `http/https` scheme。
- [ ] 使用 Channel API Key 替换客户端认证。
- [ ] 添加 `OpenAI-Beta: responses_websockets=2026-02-06`，透传 beta/turn/session 元数据，并保持 Header Override 优先级。
- [ ] 使用本地 `httptest` + WebSocket server 模拟 sub2api，验证 path、query、认证、beta feature、originator、session 和首帧 body。
- [ ] 握手或首帧发送失败且尚未写下游业务帧时进入现有渠道 failover；之后禁止切换。

验证：

```bash
go test ./relay/channel ./relay/channel/openai ./relay/channel/codex ./controller
```

## 8. 双向转发、多轮状态与计费

- [ ] 实现一客户端连接对一上游连接的双向代理和幂等关闭。
- [ ] 保持 text/binary payload、Ping/Pong、close code 和 context 取消语义。
- [ ] 连接内只允许一个 active turn；终态后允许下一条 `response.create`。
- [ ] 每轮重新解析 model、Compact 模式、映射和价格；禁止状态串轮。
- [ ] 每轮请求发上游前 Reserve；`response.completed` 按 `dto.Usage` Settle；failed/cancel/disconnect Refund。
- [ ] 普通 Responses turn 与 V2 Compact turn 在同一连接交替时使用各自价格。
- [ ] 消费日志逐 turn 记录，连接关闭本身不生成重复消费日志。
- [ ] 覆盖多轮成功、普通/Compact 交替、第二轮模型变化、重复 response.create、cancel、上游 error、断连、额度不足和 quota clamp。

验证：

```bash
go test ./controller ./relay ./service
go test -race ./controller ./relay ./service
```

## 9. 全链路回归与规范更新

- [ ] 增加真实 HTTP server 场景：V1 JSON、V2 SSE、V1 body bridge SSE、Responses WS 到模拟 sub2api。
- [ ] 验证普通 `/v1/responses`、Chat Completions via Responses、视觉辅助、非流式 JSON 保活和 Realtime WS 不回归。
- [ ] 更新 `.trellis/spec/backend/relay-alpha-search-compact.md`，扩展 V1/V2/WS 契约；必要时拆出专门 Responses WS spec 并更新 index。
- [ ] 执行实现现实检查，验证 OpenAI commit、sub2api path/header、Gorilla writer 生命周期和历史记录兼容假设。

最终验证：

```bash
gofmt -w <本任务修改的 Go 文件>
go test ./controller ./dto ./middleware ./relay ./router ./service ./setting/ratio_setting
go test -race ./controller ./middleware ./relay ./service
go test ./...
go vet ./...
git diff --check
```

## 10. Review Gates

- [ ] Gate A：detector、后缀、模型映射和普通 Responses 隔离通过后，才进入响应 bridge。
- [ ] Gate B：bridge writer 的取消/并发/race 通过后，才进入 WS 实现。
- [ ] Gate C：WS 首帧分发和上游 sub2api 握手通过后，才接入多轮计费。
- [ ] Gate D：最终 Check-All 重点复核协议原样性、failover 提交边界、预扣/退款和日志敏感信息。
