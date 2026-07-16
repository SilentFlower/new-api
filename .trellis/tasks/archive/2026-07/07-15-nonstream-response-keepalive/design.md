# 非流式响应空白心跳技术设计

## 1. 设计目标

在不改变默认行为的前提下，为明确返回 JSON 的非流式 relay 请求提供应用层空白心跳。心跳覆盖上游等待、响应体读取与渠道重试全过程，并保证心跳和最终响应不会并发交错。

## 2. 配置合同

- 在 `operation_setting.GeneralSetting` 增加 `NonStreamKeepAliveEnabled bool`，JSON 键为 `non_stream_keepalive_enabled`，代码默认值为 `false`。
- 复用现有 `PingIntervalSeconds`；非法或非正值沿用现有默认间隔回退逻辑。
- 流式 `PingIntervalEnabled` 与新开关独立。任何一个开关启用时，前端允许编辑共享间隔。
- 配置继续通过 `general_setting.*` option 持久化，不新增表、列或迁移。

## 3. 适用性判断

在 relay 公共层增加可测试的领域判断，输入为 `RelayInfo`，仅在以下条件同时满足时返回 true：

1. 全局非流式保活开关开启；
2. `RelayInfo` 非空且 `IsStream=false`；
3. `RelayFormat`/`RelayMode` 位于显式 JSON 允许列表。

允许范围：

- OpenAI Chat Completions、Completions、Responses、Responses Compact；
- Claude Messages；
- Gemini generate 与 embedding；
- Embeddings、Moderations、Rerank；
- OpenAI Image Generations、Image Edits，以及兼容 `/v1/edits` 的图片 JSON 路径。

拒绝范围：

- 所有流式请求；
- Realtime/WebSocket；
- Audio Speech、Transcription、Translation；
- 下载、图片二进制、任务文件、SSE；
- 未显式列入的新 relay mode。

## 4. 响应写入包装器

### 4.1 生命周期

`controller.Relay` 在完成请求校验并生成 `RelayInfo` 后、进入渠道重试循环前，为符合条件的请求安装请求级 `gin.ResponseWriter` 包装器，并启动心跳循环。controller defer 必须停止循环并等待其退出，然后才允许 Gin 回收 context。

包装器贯穿全部渠道尝试，避免每次重试重新创建心跳以及重试间隙失去保活。

### 4.2 状态

包装器至少维护：

- 原始 `gin.ResponseWriter`；
- 串行化写入的 mutex；
- 是否已开始最终业务响应；
- 是否实际写出过空白心跳；
- 停止信号、完成信号和只执行一次的清理保护。

### 4.3 心跳写入

每次 tick：

1. 加锁并检查请求 context、停止状态和最终响应状态；
2. 首次心跳前设置 `Content-Type: application/json`、`X-Accel-Buffering: no`，移除 `Content-Length`；
3. 通过原始 writer 写入单个 `\n`；
4. 立即调用原始 writer 的 Flush；
5. 标记已写出心跳。

单次写入应延用现有流写入 deadline 的保护思路，避免慢客户端让清理永久阻塞。
Flush 只下发当前分块，不得关闭响应：HTTP/1.1 不能发送结束块，HTTP/2/3 不能发送 `END_STREAM`。只有最终 JSON 写完且 handler 返回后，下游读取才完成。

### 4.4 普通响应写入

包装器的 `WriteHeader`、`WriteHeaderNow`、`Write`、`WriteString` 和对外 `Flush` 在持有同一把锁时将状态切换为“最终响应已开始”，并停止未来心跳，然后委托原始 writer。

`Header`、`Status`、`Size`、`Written`、`Hijack`、`CloseNotify`、`Pusher` 等能力透明委托；额外提供 `Unwrap() http.ResponseWriter`，保证 `http.ResponseController` 能找到底层 writer。

心跳内部不得调用包装器的普通 `Write`/`Flush`，否则会错误终止自己。

## 5. 最终响应与错误

- 未写出心跳：保持现有路径，复制允许的上游响应头、设置准确 `Content-Length`、写真实状态码和响应体。
- 已写出心跳：不再设置固定 `Content-Length`，不复制后到的 provider 响应头，也不再次假设可以改变 HTTP 状态码；仅追加最终成功或错误 JSON。
- 上游 request ID 仍由 `doRequest` 写入 Gin context，并进入现有日志；New API request ID 在心跳前已由 `RequestId` middleware 设置。
- 渠道失败后继续按现有 `shouldRetry` 规则重试。重试成功时最终响应是“若干空白 + 成功 JSON”；全部失败时是“若干空白 + 对应 relay format 的标准错误 JSON”，实际状态码为 200。
- `service.IOCopyBytesGracefully` 需要识别 writer 已提交的情况，避免在心跳后设置 `Content-Length`、复制迟到 header 或重复写状态码；未提交时行为必须不变。

## 6. 前端交互

- 在现有保持连接心跳设置区域增加“非流式响应保活”Switch。
- 使用现有 React Hook Form + Zod 数据流，将 `general_setting.non_stream_keepalive_enabled` 纳入 schema、默认值、类型、flatten/save 和 section 注入。
- 共享间隔的 disabled 值直接由两个 `form.watch` 布尔值计算，不建立冗余 state，也不为简单布尔表达式增加 `useMemo`。
- 使用现有 `Alert` 展示风险：首次心跳后 HTTP 状态固定为 200，后到的 provider header 无法透传；不要新增确认弹窗或嵌套卡片。
- 新增文案通过 `useTranslation()` 的字面量键调用，并按脚本流程补齐七种实际注册语言。

## 7. 并发与清理

- 心跳和最终写入共用 mutex，保证字节序列只有“前导空白”与“一个完整 JSON”，不出现 JSON 中间插入心跳。
- 停止流程必须幂等；禁止在持锁状态等待 goroutine 完成，避免锁反转。
- 请求 context 取消、客户端断开、普通响应开始或 controller 退出都会停止心跳。
- controller defer 等待完成信号，保证 goroutine 不再引用可能被 Gin 复用的 context/writer。

## 8. 兼容性与回滚

- 默认关闭且历史配置缺字段时为 false，因此部署后无行为变化。
- relay 主路由当前未启用 gzip；仍需以测试确认 Flush 可见，并通过 `X-Accel-Buffering: no` 降低外部 Nginx 缓冲风险。
- 回滚只需关闭配置即可恢复请求行为；代码回滚不涉及数据库迁移或数据修复。
- Cloudflare 之外的代理若强制缓冲微小分块，本功能不承诺绕过其平台限制。
