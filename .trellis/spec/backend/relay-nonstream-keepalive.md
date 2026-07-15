# Relay 非流式 JSON 响应保活契约

> 记录长耗时非流式 JSON relay 请求的空白心跳、响应提交、重试、错误体和并发写入约束。

## 场景：非流式 JSON 空白心跳

### 1. Scope / Trigger

- Trigger：修改 `controller.Relay` 生命周期、`gin.ResponseWriter` 包装、非流式响应写入、relay mode、渠道重试、图片 JSON 响应，或 `general_setting` 的心跳配置。
- 适用范围：在发起上游请求前即可确定最终响应是单个 JSON 值的非流式 relay 请求。
- 目标：等待上游期间定期写出 JSON 合法前导空白并 Flush，降低 Cloudflare 等反向代理因长时间收不到源站响应数据而超时的概率。
- 非目标：不调整代理平台超时，不改变上游 TCP keepalive，不把非流式响应改成 SSE，也不支持二进制响应。

### 2. Signatures

- 后端配置：

```go
type GeneralSetting struct {
	PingIntervalEnabled       bool `json:"ping_interval_enabled"`
	NonStreamKeepAliveEnabled bool `json:"non_stream_keepalive_enabled"`
	PingIntervalSeconds       int  `json:"ping_interval_seconds"`
}
```

- 请求级入口：

```go
func StartNonStreamKeepAlive(c *gin.Context, info *relaycommon.RelayInfo) func()
```

返回值是幂等停止函数；调用后必须等待心跳协程完全退出，才能允许 Gin 回收请求上下文。

- 最终响应写入协作接口：

```go
interface {
	BeginFinalResponse()
	NonStreamKeepAliveWritten() bool
}
```

`service.IOCopyBytesGracefully` 在复制上游响应头或写最终状态码前调用该接口。

### 3. Contracts

#### 配置与默认值

- `general_setting.non_stream_keepalive_enabled` 默认必须为 `false`；历史配置缺少该字段时保持关闭。
- 流式 SSE 心跳和非流式 JSON 心跳共用 `ping_interval_seconds`，但开关相互独立。
- 间隔小于等于零时回退到 `relay/helper` 的默认 Ping 间隔。

#### 协议允许列表

- 只有 `RelayInfo.IsStream == false` 且 format/mode 位于显式允许列表时才能启动。
- 允许：Chat Completions、Completions、Responses、Responses Compact、Claude Messages、Gemini、Embeddings、Moderations、Rerank、Image Generations、Image Edits 和兼容 `/v1/edits` 的图片 JSON。
- 拒绝：SSE、Realtime/WebSocket、Audio Speech/Transcription/Translation、文件下载、任务文件、二进制图片及未知 mode。
- 新增 relay mode 时默认不获得保活能力；必须先证明其响应在上游请求前即可确定为 JSON，再显式加入允许列表和测试。

#### 心跳与 writer 生命周期

- 心跳字节只能使用 JSON 允许的 ASCII 空白；当前固定为单个 `\n`，禁止复用 SSE 的 `: PING\n\n`。
- 首次心跳设置 `Content-Type: application/json`、`X-Accel-Buffering: no`，删除 `Content-Length`，延长写 deadline，然后 `Write` + `Flush`。
- Flush 只发送当前响应分块，不结束 HTTP 响应；最终 JSON 写完且 handler 返回后，下游才收到 EOF 或 `END_STREAM`。
- 心跳与业务侧 `Header`、`WriteHeader`、`WriteHeaderNow`、`Write`、`WriteString`、`Flush` 必须使用同一把 mutex 串行化。
- 业务代码访问包装器的 `Header()` 表示最终响应已经开始：包装器必须在返回底层 header 前停止后续心跳，避免并发修改 `http.Header` map。
- 心跳内部必须直接使用底层 writer，不能调用包装器的普通 `Write` 或 `Flush`，否则会把心跳误判为最终响应。
- 请求取消、业务响应开始或 controller 退出都必须停止心跳；controller 的停止 defer 必须等待协程退出。

#### 状态码、重试与响应头

- 首次心跳会提交下游 HTTP 200；之后任何 `WriteHeader(4xx/5xx)` 都不能改变实际状态码。
- 心跳不是业务响应数据，不能阻止现有渠道重试。后续渠道成功时返回“前导空白 + 成功 JSON”。
- 所有渠道失败时返回“前导空白 + 对应 relay format 的标准 JSON 错误体”，实际 HTTP 状态保持 200；原始错误码和错误信息仍进入错误体及服务端日志。
- 首次心跳前完成的请求必须保留原状态码、上游允许响应头、准确 `Content-Length` 和原始响应字节。
- 实际写出心跳后，`IOCopyBytesGracefully` 不得设置固定 `Content-Length`、复制后到的 provider header 或重复写状态码。
- 即使 provider header 无法下发，也必须继续调用 `ShouldCopyUpstreamHeader`，以便把上游请求 ID 写入 Gin context；本地请求 ID 在安装 writer 前已经写入响应头，必须保留。

#### 日志

- 启动和停止状态使用请求级 `LogDebug`；停止日志包含是否实际写出过心跳。
- 禁止每次 tick 输出普通日志，避免长请求产生高频日志。
- 日志不得包含请求体、响应体、API Key 或用户对话内容。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 新开关关闭或历史配置缺字段 | 不安装 writer，行为完全不变 |
| 请求为流式或 mode 不在允许列表 | 不启动非流式心跳 |
| 上游在首次 tick 前完成 | 不写前导空白，保留真实状态码、header 和 `Content-Length` |
| 心跳后渠道失败但仍可重试 | 按现有 `shouldRetry` 继续选择渠道 |
| 心跳后后续渠道成功 | HTTP 200，响应体是可解析的“空白 + 成功 JSON” |
| 心跳后所有渠道失败 | HTTP 200，响应体是可解析的“空白 + 标准错误 JSON” |
| 请求 Context 取消 | 心跳协程退出，不再访问 Gin context 或 writer |
| 最终响应开始时 tick 同时到达 | mutex 保证完整心跳只可能位于 JSON 之前，不能插入 JSON 中间 |
| 心跳写入或客户端连接失败 | 心跳协程退出，由现有请求错误和日志链路处理 |

### 5. Good / Base / Bad Cases

- Good：图片生成超过代理空闲超时前持续写出换行，最终返回 URL 或 `b64_json`；客户端标准 JSON 解析结果与关闭保活时一致。
- Good：第一个渠道超时后继续重试，第二个渠道成功；前导空白不影响成功 JSON。
- Good：所有渠道失败且已经发过心跳；客户端收到 HTTP 200，但通过标准 JSON 错误体识别失败。
- Base：请求在首次间隔前完成；原始 body、状态码和响应头完全不变。
- Base：管理员只启用 SSE Ping；非流式请求不写空白心跳。
- Bad：向非流式 JSON 写入 `: PING\n\n`，会破坏 JSON 语法。
- Bad：根据上游 `Content-Type` 才启动心跳，会漏掉等待上游响应头的阶段。
- Bad：心跳后继续设置固定 `Content-Length` 或转发迟到 header，会制造错误的 HTTP 元数据。
- Bad：业务代码先调用 `Header()` 设置最终响应头，再继续等待上游；该调用会结束心跳生命周期。

### 6. Tests Required

- 允许列表表驱动测试：覆盖全部允许 mode，以及流式、Audio、Realtime、未知 mode 等拒绝路径。
- 历史配置测试：缺少 `non_stream_keepalive_enabled` 时必须为 `false`。
- 可控 tick 测试：验证换行、Flush、Content-Type、`X-Accel-Buffering`、停止幂等和最终 JSON 不交错。
- 真实 HTTP 测试：客户端先读到心跳后仍不能收到 EOF，最终 JSON 写完后才结束。
- 取消测试：取消请求 Context 后直接等待心跳协程退出，并确认之后不再写入。
- 错误测试：OpenAI/Gemini 和 Claude 错误体在前导空白后仍是单个合法 JSON 值，实际状态为 200。
- 重试测试：已经写出空白时 `shouldRetry` 仍按原错误策略返回；后续成功响应保持可解析。
- 元数据测试：未发心跳时复制真实状态码、header 和 `Content-Length`；发过心跳时跳过迟到元数据，但仍捕获上游请求 ID。
- 图片回归：`/v1/images/generations` 和 `/v1/images/edits` 同时覆盖 URL、`b64_json`、流式排除和 HTTP 200 提交语义。
- 并发核心必须运行定向 race 测试；跨层变更完成后运行 `go test ./...`。

### 7. Wrong vs Correct

#### Wrong

```go
if !info.IsStream {
	c.Writer.Write([]byte(": PING\n\n"))
	c.Writer.Flush()
}
```

问题：SSE 注释不是 JSON 空白；同时没有协议允许列表、并发控制、取消清理或已提交状态处理。

#### Correct

```go
stopNonStreamKeepAlive := helper.StartNonStreamKeepAlive(c, relayInfo)
defer stopNonStreamKeepAlive()

// 最终响应写入边界先停止心跳，再根据是否实际写出过心跳决定元数据行为。
writer.BeginFinalResponse()
if !writer.NonStreamKeepAliveWritten() {
	c.Writer.Header().Set("Content-Length", contentLength)
	c.Writer.WriteHeader(statusCode)
}
```

要求：入口位于完整渠道重试循环之外；最终写入与心跳共享串行化边界；心跳后的错误只能通过 JSON body 表达。
