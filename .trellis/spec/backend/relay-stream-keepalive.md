# Relay 流式 SSE 响应保活契约

> 记录 Relay 流式请求在上游响应头等待阶段和上游流正文读取阶段的 SSE comment 保活、idle timeout、请求体读取边界和日志分类约束。

## 场景：流式 SSE 两阶段保活

### 1. Scope / Trigger

- Trigger：修改 `controller.Relay` 请求生命周期、`relay/channel.DoRequest` 上游 HTTP 请求、`relay/helper.StreamScannerHandler` 流正文扫描、SSE ping、stream end reason、渠道重试或流式日志分类。
- 适用范围：已完成请求体读取和请求校验，并且 `RelayInfo.UsesUpstreamStream()` 返回 true 的流式 HTTP relay 请求。
- 目标：等待上游响应头和读取上游流正文时向下游发送协议合法的 SSE comment keepalive，降低代理链路因响应阶段长时间无字节而超时的概率。
- 非目标：不覆盖客户端上传请求体阶段，不修改 Cloudflare/Nginx/隧道配置，不改变非流式 JSON 空白保活，不把 Realtime/WebSocket、文件下载、音频二进制或未知响应改成 SSE。

### 2. Signatures

```go
type GeneralSetting struct {
	PingIntervalEnabled       bool `json:"ping_interval_enabled"`
	NonStreamKeepAliveEnabled bool `json:"non_stream_keepalive_enabled"`
	PingIntervalSeconds       int  `json:"ping_interval_seconds"`
}

func (info *RelayInfo) UsesUpstreamStream() bool

func doRequest(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo, logRequestError bool) (*http.Response, error)

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult))

func PingData(c *gin.Context) error
```

请求体读取失败分类：

```go
func initialRelayRequestError(err error) *types.NewAPIError
```

### 3. Contracts

#### 生命周期边界

- `controller.Relay` 必须先完成 `helper.GetAndValidateRequest`，再生成 `RelayInfo`；请求体未读完或校验失败前不得启动响应保活，也不得提交响应头。
- `io.ErrUnexpectedEOF` 和 request body too large 属于请求体读取阶段错误，应映射为 `read_request_body_failed` 并设置 `skipRetry`；它们不能被记录为流式 idle timeout。
- 流式响应头等待阶段由 `doRequest` 覆盖：`info.UsesUpstreamStream()` 为 true 时先设置 SSE header，再在 `relayClient.Do(req)` 返回前按 `ping_interval_seconds` 发送 `: PING\n\n`。
- 流正文读取阶段由 `StreamScannerHandler` 覆盖：进入 scanner 后继续按同一 ping 配置发送 SSE comment，并与业务数据写入共享写锁。

#### 协议与配置

- SSE keepalive payload 固定使用 `helper.PingData` 写出的 `: PING\n\n`；该 payload 是 comment，不代表业务 chunk、终态、usage 或计费信息。
- `ping_interval_enabled=false` 或 `RelayInfo.DisablePing=true` 时不得发送流式 ping。
- `ping_interval_seconds <= 0` 时回退到 `relay/helper.DefaultPingInterval`。
- 非流式 JSON 响应继续走 `StartNonStreamKeepAlive` 的 `\n` 前导空白契约；不得复用 SSE comment。
- `UsesUpstreamStream()` 必须排除历史 Responses Compact unary 上游桥接，避免等待 unary 响应时提前按真实上游流处理。

#### 上游有效数据与 idle timeout

- `constant.StreamingTimeout` 判断真实上游有效数据静默时间；本地下游 keepalive 不得重置该 timeout。
- scanner 只在非空 `data:` payload 且 payload 不是 `[DONE]` 时刷新 idle timeout、设置首包时间、增加 `ReceivedResponseCount` 并交给 `dataHandler`。
- 上游空行、SSE comment、非 `data:` 行和空 `data:` payload 必须被过滤，且不能刷新 idle timeout。
- `data: [DONE]` 和裸 `[DONE]` 都应结束扫描并设置 `StreamEndReasonDone`。
- 客户端断开时应设置 `StreamEndReasonClientGone`，关闭上游 `resp.Body`，避免继续消耗上游 token。

#### 日志与错误

- 等待上游响应头阶段的网络错误日志应区分为 stream upstream request failed before response headers，并对错误文本做本地预览和敏感信息脱敏。
- 请求体读取失败日志应明确标注 before response keep-alive stage。
- 日志不得包含请求体、响应体、API Key、完整敏感 URL query 或用户对话内容。
- ping 写失败应停止对应保活 goroutine；正文阶段应记录 `StreamEndReasonPingFail`。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 请求体读取返回 `io.ErrUnexpectedEOF` | `400 read_request_body_failed`，`skipRetry`，不启动保活，不记录流式 timeout |
| 请求体超过大小限制 | `413 read_request_body_failed`，`skipRetry`，不启动保活 |
| `UsesUpstreamStream()==true` 且等待上游响应头超过 ping 间隔 | 下游收到 `Content-Type: text/event-stream` 和 `: PING\n\n` |
| 上游响应头已返回但业务 data 短暂静默 | 流正文阶段继续发送 `: PING\n\n` |
| 上游只发送 comment、空行或空 `data:` | 下游可收到本地 ping，但 idle timeout 不刷新 |
| 长期无有效业务 data | `StreamEndReasonTimeout`，收到的业务响应计数不增加 |
| 上游返回 `data: [DONE]` 或裸 `[DONE]` | `StreamEndReasonDone`，停止 scanner |
| `ping_interval_enabled=false` 或 `DisablePing=true` | 不发送本地 SSE ping，业务流转发保持原行为 |
| 历史 Responses Compact unary bridge | 不按 upstream stream TTFB ping 处理，继续使用 Compact bridge 自身契约 |

### 5. Good / Base / Bad Cases

- Good：流式请求上游首包耗时超过代理空闲阈值，Relay 在响应头等待期间先发送 SSE comment，后续上游成功后继续返回真实 data。
- Good：上游流正文每 30 秒返回一次真实 `data:` chunk，本地 ping 每 10 秒保活下游；idle timeout 只按真实 chunk 刷新。
- Good：上游只发 `: keep-alive` comment 但长期没有业务 data，Relay 最终按 `StreamEndReasonTimeout` 结束，避免无限占用连接。
- Base：管理员关闭 SSE ping 后，流式请求不写本地 comment，仍按真实上游数据转发和 timeout。
- Base：客户端上传请求体中途断开，Relay 返回请求体读取错误；这是响应保活无法覆盖的阶段。
- Bad：在 `GetAndValidateRequest` 之前提交 SSE header，会把非流式或无效请求错误体强行变成流式响应。
- Bad：每次 scanner 读到空行、comment 或本地 ping 就重置 timeout，会掩盖真实上游业务数据长期缺失。
- Bad：把非流式 JSON 的保活 payload 改成 `: PING\n\n`，会破坏 JSON 语法。

### 6. Tests Required

- TTFB keepalive：真实 `httptest` 下游在上游响应头阻塞时先读到 `: PING\n\n`，并断言 SSE header。
- 流正文 keepalive：慢速上游业务 data 之间能收到本地 `: PING`，业务 chunk 计数仍正确。
- idle timeout：上游 comment、空行、本地 ping 或空 payload 不刷新 timeout，最终 `StreamEndReasonTimeout`。
- 终态：`data: [DONE]` 和裸 `[DONE]` 都应设置 `StreamEndReasonDone`。
- 请求体读取阶段：`io.ErrUnexpectedEOF` 和 oversized body 分别映射到 `400/413 read_request_body_failed` 且 `skipRetry`。
- 非流式边界：保留 `relay-nonstream-keepalive.md` 的允许列表、空白 payload、状态码提交和最终 JSON 可解析测试。
- 回归命令：
  - `go test ./relay/helper ./relay/channel ./controller`
  - `go test -race ./relay/helper -run 'StreamScannerHandler_(PingSentDuringSlowUpstream|UpstreamCommentsDoNotResetIdleTimeout|PingDoesNotResetIdleTimeout)' -count=1`
  - `go test -race ./relay/channel -run 'TestDoRequestSendsStreamPingWhileWaitingForUpstreamHeaders' -count=1`
  - `go vet ./relay/... ./controller/... ./service/...`
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong

```go
for scanner.Scan() {
	ticker.Reset(streamingTimeout)
	line := scanner.Text()
	if !strings.HasPrefix(line, "data:") {
		continue
	}
	handle(line)
}
```

问题：上游 comment、空行或其它非业务行也会刷新 idle timeout，代理链路虽然保活了，但真实上游长期没有业务数据的问题被掩盖。

#### Correct

```go
for scanner.Scan() {
	line := scanner.Text()
	if !strings.HasPrefix(line, "data:") {
		continue
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" {
		continue
	}
	if data == "[DONE]" {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
		return
	}
	ticker.Reset(streamingTimeout)
	dataHandler(data, sr)
}
```

要求：下游 keepalive 与上游有效数据判断分离；只有真实业务 `data:` 刷新 idle timeout。

## 场景：Responses SSE 业务终态与传输结束分离

### 1. Scope / Trigger

- Trigger：修改 OaiResponsesStreamHandler 的终态观察、StreamStatus 错误记录或日志中的 stream_status。
- 避免上游已失败、取消或中断，但传输随后发送 [DONE] 而被展示为正常业务完成。

### 2. Signatures

    type responsesStreamStatusObserver struct {
        completed bool
        failed bool
    }
    func (o *responsesStreamStatusObserver) observe(status *relaycommon.StreamStatus, event dto.ResponsesStreamResponse)
    func (o *responsesStreamStatusObserver) finalize(c *gin.Context, info *relaycommon.RelayInfo)
    func (s *StreamStatus) RecordError(msg string)

现有日志字段为 other.stream_status.status、end_reason、error_count、errors；不新增 schema 或前端字段。

### 3. Contracts

- response.failed、response.incomplete、response.cancelled、response.canceled、response.error 和顶层 error 记录一次稳定业务错误，后续 [DONE]/EOF 不消除错误。
- response.completed 或兼容的 response.done 且没有显式失败状态表示业务完成；完成事件携带 failed/incomplete/cancelled/canceled 状态时仍记异常。
- 正常传输结束而缺少业务完成事件时，在扫描回调全部退出后补记缺少完成事件；已有客户端取消、超时、读取或 JSON 解码错误保留原始归因。
- end_reason 继续表达传输收尾原因；最终 status 由 IsNormalEnd 和 HasErrors 共同决定，done/error 是有效组合。
- 扫描器可能先于回调读到 [DONE]，而 SetEndReason 使用 sync.Once。Responses 观察器必须复用受锁保护的 RecordError，不争抢或重写公共结束原因。
- 观察器状态仅由串行回调更新，最终检查在 StreamScannerHandler 等待其 goroutine 完成后执行。
- 沿用原始事件转发、usage 和结算路径，不新增向外层返回的 NewAPIError、错误帧或流开始后的透明重试。
- 预热、合法零 usage、工具调用及搜索不按输出 Token 数判错；新增诊断只保留固定分类和事件类型，不拼接原始上游 message 或正文。

### 4. Validation & Error Matrix

| 条件 | 最终日志行为 |
| --- | --- |
| failed/incomplete/error 等异常事件后接 [DONE] 或 EOF | status=error，errors 保留业务事件类型 |
| 正常 [DONE]/EOF，但没有 completed/done | status=error，errors 说明缺少业务完成事件 |
| 已有 client_gone、timeout、读取或解析错误 | 保留已有错误，不追加缺少完成事件覆盖归因 |
| completed/done，包含合法非零或零 usage | 保持正常，原有 usage 不变 |
| 未知事件后正常完成 | 未知事件继续转发，保持正常 |

### 5. Scenarios and Examples

- 正常：response.completed 携带 input_tokens=2、output_tokens=0，作为成功预热按现有规则处理。
- 边界：上游只发送 response.created 后结束，记录缺少业务完成事件；客户端主动取消则保留取消原因。
- 错误用法：仅因收到 [DONE] 就把业务结果判断为成功，或由 Responses 回调全局改写 SetEndReason 优先级。
- 正确处理：保留传输结束标记，并独立记录业务异常，使现有日志状态和错误列表展示真实结果。

### 6. Tests Required

- TestResponsesStreamStatusProtocolTerminals：使用真实处理器和日志生成入口，覆盖异常+DONE/EOF、缺少终态、正常完成、预热/零 usage、工具调用及未知事件；断言状态、原因、原始事件和 usage。
- TestResponsesStreamStatusPreservesTransportErrors：读取错误、客户端取消、超时和 JSON 解码错误保留原始归因。
- 回归：go test ./relay/channel/openai ./relay/helper ./relay/common ./service -count=1。
- 时序：go test -race ./relay/channel/openai -run 'Responses.*Stream|Responses.*Billing' -count=1。
