# Relay 上游 HTTP 传输层契约

## 场景：上游 HTTP/2 连接健康探测

### 1. Scope / Trigger

- 修改 `service/http_client.go` 的 `newRelayHTTPTransport`、`service/http_transport_policy.go`、`service/http_transport_sharded.go`，或新增、调整 `RELAY_*` 传输层环境变量时适用。
- 背景：2026-11-11/12 生产上，渠道 1/16/17/19 共用 `dashscope.aliyuncs.com` 的同一条 HTTP/2 连接。这条连接静默失效（对端不回 RST/GOAWAY）后，每次约 100 个请求（对端 `MAX_CONCURRENT_STREAMS: 100`）挂满 `RELAY_TIMEOUT=300`，报 `context deadline exceeded (Client.Timeout exceeded while awaiting headers)`。渠道 17 重试到渠道 19 时仍落在同一条连接上，总耗时 600s。

### 2. Signatures

- `func newRelayHTTPTransport() *http.Transport`：所有转发 transport 的唯一构造入口，覆盖直连、代理、连接分片三条路径。HTTP/2 参数只能在这里配置。
- `common.RelayHTTP2PingIdleTimeout int`、`common.RelayHTTP2PingTimeout int`：在 `common/init.go` 的 `InitEnv` 中从环境变量读取。
- `const maxTimeoutSeconds`：秒数转 `time.Duration` 前的上限，防止溢出。
- 实现移植自上游 PR #8059（2026-11-02 合并），变量名、默认值、代码与测试保持一致。上游 #6949（`RELAY_RESPONSE_HEADER_TIMEOUT`）尚未合入；同步上游时 `maxTimeoutSeconds` 只保留一份定义。

### 3. Contracts

| 环境变量 | 默认 | 语义 |
| --- | --- | --- |
| `RELAY_HTTP2_PING_IDLE_TIMEOUT` | `0` | 连接连续多少秒没收到任何帧就发送 PING，写入 `transport.HTTP2.SendPingTimeout`；`<=0` 时不设置 `transport.HTTP2`，保持原行为 |
| `RELAY_HTTP2_PING_TIMEOUT` | `15` | 等待 PING 应答的秒数，写入 `transport.HTTP2.PingTimeout`；`<=0` 时使用 Go 默认值 15s；超时即关闭该连接 |

- 统一通过标准库 `http.HTTP2Config`（Go ≥ 1.24）配置，与发给客户端的 SSE 心跳（`PingIntervalEnabled`）相互独立。
- 渠道设置 `http_protocol=http1` 时不走 HTTP/2，上述配置不生效。
- 代码默认关闭探测。生产要启用，必须在 `ai-newapi/new-api` Deployment 中把 `RELAY_HTTP2_PING_IDLE_TIMEOUT` 显式设为正数；只升级镜像不会改变行为。

### 4. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| `RELAY_HTTP2_PING_IDLE_TIMEOUT<=0` | 不发送 PING；静默失效连接上的请求一直等到调用方期限或 `RELAY_TIMEOUT` |
| 探测开启，对端正常应答 PING | 慢响应继续等待，不会被误杀 |
| 探测开启，对端不应答 PING | 约 `IDLE+TIMEOUT` 秒后关闭连接；进行中的请求报 `http2: client connection lost`，已收到的数据不会重放；下一个请求新建连接 |
| 配置值超过 `maxTimeoutSeconds` | 先截断再转换，不会溢出成极小超时、误伤所有请求 |

### 5. Scenarios and Examples

- Normal：生产设置 `RELAY_HTTP2_PING_IDLE_TIMEOUT=15`。静默失效的连接在几十秒内被关闭，其上请求快速失败，由渠道重试接管，不再挂满 300s。
- Boundary：不设置或设为 `0`，行为与改动前完全一致。
- Incorrect：想靠调大 `http2_connection_shards` 或调小 `RELAY_TIMEOUT` 解决卡死。
  - 分片按 origin 轮询，失效的那个分片仍会分到 1/N 的请求，并照样挂满 100 个流。
  - 全局调小 `RELAY_TIMEOUT` 会误杀生成时间长的非流式对话。
- Correct：开启 PING 探测；需要不发版立即止血时，对相关渠道设置 `http_protocol=http1`。
- Incorrect：在 `applyHTTPTransportPolicy` 或调用点单独设置 `HTTP2Config`，会漏掉代理或分片路径。
- Correct：只在 `newRelayHTTPTransport` 中设置。

### 6. Tests Required

`service/http_client_transport_test.go` 的 `TestRelayHTTP2HealthCheck` 必须保留以下用例和断言：

- `disabled`：读取响应体返回 `context.DeadlineExceeded`，且 PING 计数为 0。
- `healthy slow response`：对端应答 PING 时完整收到 `startend`，PING 计数 ≥ 2，始终只有 1 条连接。
- `unresponsive connection`：报错包含 `http2: client connection lost`，只收到 `start`（不重放）；下一个请求完整成功，连接数变为 2。
