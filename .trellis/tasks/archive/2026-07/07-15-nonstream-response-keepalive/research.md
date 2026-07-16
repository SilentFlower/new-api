# 非流式响应空白心跳调研

## 现有链路

- `relay/common/relay_info.go` 在 `genBaseRelayInfo` 中根据请求 DTO 和路径生成 `RelayInfo.IsStream`、`RelayMode`、`RelayFormat`，因此可在发起上游请求前判定客户端请求是否为流式及其逻辑响应协议。
- `relay/channel/api_request.go` 的现有 ping 仅在 `info.IsStream` 时启动；`relay/helper/stream_scanner.go` 在收到上游 SSE 响应后继续负责流式期间的 ping。
- 非流式 OpenAI 文本与图片路径分别在 `relay/channel/openai/relay-openai.go` 和 `relay/channel/openai/relay_image.go` 完整读取、解析上游 body 后，再通过 `service.IOCopyBytesGracefully` 写给下游。
- `service.IOCopyBytesGracefully` 当前会在最终写入前复制允许的上游响应头、设置固定 `Content-Length` 并写入上游状态码。若心跳已先提交响应，这三项都不能继续按原方式执行。
- `controller/relay.go` 的渠道重试循环在所有尝试结束后才通过 defer 写最终错误体。只写前导 JSON 空白不妨碍后续渠道成功，因此需求决定继续沿用现有重试规则。

## 路由与协议边界

- `relay/constant/relay_mode.go` 已提供稳定的 relay mode，可建立显式 JSON 允许列表。
- 图片生成和图片编辑分别映射到 `RelayModeImagesGenerations`、`RelayModeImagesEdits`，非流式 URL 与 `b64_json` 最终均为 JSON，是本功能核心范围。
- Claude Messages 的路径不会映射到专用 relay mode，需要结合 `RelayFormatClaude` 判断。
- 音频 speech、transcription、translation 可能返回音频、文本、字幕或 JSON，不能在等待上游响应头前统一判定，首期全部排除。
- Realtime/WebSocket、SSE、任务下载和其他二进制路径全部排除；新增 relay mode 不自动获得保活能力。

## HTTP 写入约束

- 标准 JSON 允许顶层值之前存在 `SP`、`HTAB`、`CR`、`LF`；心跳使用单个 ASCII 换行即可，不能复用 SSE 的 `: PING\n\n`。
- 第一次 `Write` + `Flush` 会提交 HTTP 200。之后的 `WriteHeader(4xx/5xx)` 不再改变实际状态码，但错误 JSON 仍可作为前导空白后的唯一顶层 JSON 值被解析。
- 请求级心跳必须跨越完整的上游等待、body 读取、解析和渠道重试周期；只在 `http.Client.Do` 周围启动会漏掉“已收到响应头但 body 长时间未完成”的情况。
- 最终响应写入散落在多个 adaptor/handler 中，逐个改写风险较高。更合适的边界是在 `controller.Relay` 为符合条件的请求包装 `gin.ResponseWriter`，由包装器统一串行化心跳与普通写入。
- 包装器的普通 `WriteHeader`、`Write`、`WriteString`、`Flush` 会永久停止后续心跳；心跳内部直接通过被包装的原始 writer 写入，避免被识别为最终业务响应。
- `main.go` 中 relay 路由没有启用 gzip 中间件，避免了应用内压缩缓冲；心跳首次写入前设置 `Content-Type: application/json` 和 `X-Accel-Buffering: no`，并显式 Flush。

## 配置与前端

- `setting/operation_setting/general_setting.go` 的 `GeneralSetting` 通过反射式全局配置管理器持久化，不需要数据库迁移；新增布尔字段的代码默认值即可保证历史配置关闭。
- `web/default/src/features/system-settings/models/global-settings-card.tsx` 已承载流式心跳开关和共享间隔，可在原位置增加独立开关及风险 `Alert`。
- 间隔输入的可编辑状态应直接派生为“流式开关或非流式开关已启用”，无需增加额外 React state 或 `useMemo`。
- `web/default/src/i18n/config.ts` 实际注册 `en`、`zhCN`、`fr`、`ru`、`ja`、`vi`、`zhTW` 七种资源。新增文案必须通过 i18n skill 规定的脚本写入并运行 `bun run i18n:sync`，不得手改 locale JSON。

## 验证重点

- 默认关闭和首次间隔前完成时必须做到原始字节、状态码、响应头及重试行为不变。
- 心跳后成功响应应能被标准 JSON 解析；图片 JSON 路径必须有显式回归测试。
- 心跳后重试成功与重试耗尽错误都要验证，后者实际 HTTP 状态为 200。
- 允许列表要用表驱动测试覆盖正反例，防止音频/二进制路径误入。
- 使用可控 tick/channel 测试心跳循环，避免依赖 sleep 的时序测试；请求取消和停止后不得继续写入。
