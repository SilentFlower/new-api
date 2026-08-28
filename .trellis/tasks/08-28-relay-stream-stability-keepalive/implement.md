# 增强 Relay 流式稳定性与保活策略 — Implement

## Checklist

- [x] 复查任务上下文：`prd.md`、`design.md`、`implement.jsonl`、`check.jsonl`。
- [x] 复查规范：非流式 JSON 保活契约、Responses Compact 透传约束、流式计费/结束状态相关规范。
- [x] 梳理当前流式 TTFB keepalive：确认 `DoRequest` 在 `relayClient.Do(req)` 等待期间的 header、ping、取消和 goroutine 回收语义。
- [x] 梳理当前流正文 keepalive：确认 `StreamScannerHandler` 的 ping 写锁、timeout reset 点、`StreamStatus` end reason 和客户端断开处理。
- [x] 按 PRD 修正差距：优先收敛 timeout reset 和日志分类；只有必要时调整配置或公共接口。
- [x] 保持非流式 JSON 保活协议不变；如触及边界，补允许列表/真实 HTTP/错误体回归测试。
- [x] 补充或更新定向测试：TTFB keepalive、流正文 keepalive、keepalive 不重置 idle timeout、请求体 EOF 阶段分类、非流式边界不变。
- [x] 运行定向验证命令，并根据触及范围决定是否扩大测试。
- [x] 进入 Check-All 前记录实际 diff 和测试证据。

## 验证证据

- `go test ./relay/helper ./relay/channel ./controller` 通过。
- `go test ./relay/... ./controller/... ./service/... -count=1` 通过。
- `go test -race ./relay/helper -run 'StreamScannerHandler_(PingSentDuringSlowUpstream|UpstreamCommentsDoNotResetIdleTimeout|PingDoesNotResetIdleTimeout)' -count=1` 通过。
- `go test -race ./relay/channel -run 'TestDoRequestSendsStreamPingWhileWaitingForUpstreamHeaders' -count=1` 通过。
- `go vet ./relay/... ./controller/... ./service/...` 通过。
- `git diff --check` 通过。

## Validation Commands

- `go test ./relay/helper ./relay/channel ./controller`
- 如触及共享请求读取、`RelayInfo`、计费或最终日志生成：`go test ./relay/... ./controller/... ./service/...`
- 如触及 `relaykit/`：`cd relaykit && GOWORK=off go build ./...`

## Risky Files

- `controller/relay.go`
- `relay/channel/api_request.go`
- `relay/helper/stream_scanner.go`
- `relay/helper/non_stream_keepalive.go`
- `service/http.go`

## Guardrails

- 不在请求体读取完成前写响应。
- 不把非流式 JSON 心跳改成 SSE comment。
- 不新增裸 `encoding/json` marshal/unmarshal。
- 不改变 Responses Compact 原始透传和计费/退款语义。
- 不记录请求体、响应体、API Key 或用户对话内容。
- 不扩大后台默认开关行为，除非 PRD 明确更新并重新展示 Brief。
