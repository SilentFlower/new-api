# Brief — 非流式响应空白心跳

## Goal

- 为明确返回 JSON 的非流式 relay 请求提供默认关闭的空白心跳，持续向 Cloudflare 等前置代理输出可 Flush 的响应数据，降低长耗时图片生成等请求触发 524 的概率。

## Scope

- 在 `general_setting` 增加独立的非流式响应保活开关，与流式 SSE 心跳共用现有 Ping 间隔；两个开关相互独立。
- 使用显式 JSON 允许列表覆盖 Chat/Completions、Responses、Claude、Gemini、Embeddings、Moderations、Rerank、图片生成和图片编辑，其中图片 JSON 是核心场景。
- 在 `controller.Relay` 安装请求级 `gin.ResponseWriter` 包装器，使单个 ASCII 换行心跳覆盖上游等待、body 读取、解析与全部渠道重试，并与最终 JSON 串行写入。
- 心跳后继续沿用现有渠道重试；成功时返回“前导空白 + 成功 JSON”，全部失败时返回实际 HTTP 200 + 对应协议的标准 JSON 错误体。
- 调整最终响应写入：仅在实际写出心跳后跳过固定 `Content-Length`、后到的 provider 响应头和重复状态码写入；首次间隔前完成的请求保持当前行为。
- 在 `web/default` 现有心跳区域增加 Switch 和内联风险 Alert，补齐表单数据流与七种运行时语言资源。

## Non-Goals

- 不调整 Cloudflare 账户侧超时，也不承诺绕过所有代理平台的强制缓冲。
- 不修改上游服务 keepalive，不把非流式协议改为 SSE。
- 不支持音频、下载、图片二进制、任务文件、WebSocket、SSE 或未知 relay mode。
- 不新增数据库迁移，不修改 classic 前端，不为二进制响应设计新封装协议。

## Key Context

- JSON 心跳只能使用标准允许的前导空白，不能发送 `: PING`、NUL 或 BOM。
- Flush 只发送当前分块，不结束响应；心跳后下游读取仍须等待，只有最终 JSON 写完且 handler 返回后才收到 EOF/`END_STREAM`。
- 首次心跳会提交 HTTP 200；此后无法改变实际状态码，也无法透传后到的 provider header。New API request ID 仍下发，上游 request ID 继续进入 context 和服务端日志。
- writer 必须完整代理 Gin 接口、用同一 mutex 串行化心跳与普通写入、幂等停止，并在 Gin 回收 context 前等待 goroutine 退出。
- relay 主路由当前未启用 gzip；首次心跳设置 `Content-Type: application/json`、`X-Accel-Buffering: no`，移除 `Content-Length` 并立即 Flush。
- 前端沿用 React Hook Form、Zod、现有 Switch/Alert 组合；共享间隔启用状态直接派生，不增加冗余 state 或简单 `useMemo`。
- locale 文件只能按 `i18n-translate` 规定通过脚本写入，再执行 `bun run i18n:sync`，禁止手工编辑 JSON。
- 详细证据与方案分别见 `research.md`、`design.md`、`implement.md`。

## Acceptance

- 默认关闭和历史缺字段时完全保持现有行为；首次间隔前完成时原始 body、状态码、header、`Content-Length` 和重试均不变。
- 启用后，允许列表内的慢请求收到已 Flush 的合法空白，最终成功/错误响应仍是单个可解析 JSON 值。
- 图片生成和图片编辑覆盖 URL、`b64_json`、流式排除与渠道重试回归；音频、二进制、WebSocket、SSE 和未知 mode 绝不启动空白心跳。
- 请求取消、最终写入和 controller 退出均能可靠停止心跳，无 goroutine 泄漏、Gin context 复用后写入或 JSON 中间交错。
- New API/upstream request ID 可追踪；心跳后不产生错误的固定 `Content-Length` 或迟到 header 写入。
- 后端相关测试、race 检查、`go test ./...`，以及前端 i18n、typecheck、lint、format check、生产构建全部通过。

## Next Step

- 用户确认规划文件和本 brief 后，运行 `task.py start` 激活任务，再通过 `trellis-route(implement)` 进入实现；当前阶段不修改业务代码。
