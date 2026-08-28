# Brief — 增强 Relay 流式稳定性与保活策略

## Goal

- 将 Relay 流式长连接稳定性收敛为类似 cc2api 的两阶段保活策略：等待上游响应头期间和读取上游流正文期间都能向下游发送协议合法的 SSE keepalive，同时真实上游数据静默仍会触发 idle timeout。

## Scope

- 复核并增强流式请求等待上游响应头阶段的 SSE keepalive，覆盖 `relayClient.Do(req)` 阻塞期间的下游空闲。
- 复核并增强流式上游正文读取阶段的 SSE keepalive，确保 keepalive 只保持下游链路活跃，不伪造业务数据、完成事件、usage 或计费信息。
- 收敛上游流 idle timeout 判断，让 timeout 围绕真实上游有效数据更新；本地 keepalive、空行、comment 或被过滤的非业务行不能掩盖真实业务数据长期缺失。
- 保持非流式 JSON 空白保活契约不变：非流式 JSON 继续只写 `\n` 前导空白，不改为 SSE。
- 增强日志或结束状态分类，使请求体读取失败、上游响应头等待失败、上游流正文 idle timeout、keepalive 写失败和客户端主动断开可以定位。
- 补充定向测试，覆盖 TTFB keepalive、流正文 keepalive、idle timeout 不被 keepalive 掩盖、非流式边界不变。

## Non-Goals

- 不修改 Cloudflare Dashboard、Tunnel、DNS、Nginx 或外部代理配置。
- 不通过付费 Cloudflare Enterprise、DNS-only 子域、异步任务协议或新域名绕过代理。
- 不在请求体尚未完整读取/校验前向客户端提交响应。
- 不把所有非流式请求统一改成 SSE。
- 不重做 Relay 计费、渠道选择、审计或消息记录链路。

## Key Decisions

- 请求体读取/校验阶段不启动响应保活，因为此时还不能安全判定最终响应是否为流式，也不能提前提交响应头；线上 `unexpected EOF` 属于这个阶段，代码保活无法直接覆盖。
- 流式响应使用 SSE comment keepalive；非流式 JSON 继续使用 JSON 合法空白。两者协议边界保持独立。
- 沿用现有后台配置 `ping_interval_enabled`、`non_stream_keepalive_enabled`、`ping_interval_seconds` 和现有 streaming timeout；只有实现时证明现有配置无法表达需求，才新增配置并回到规划更新 Brief。
- 采用 cc2api 可借鉴的核心语义：下游 keepalive 只代表本服务仍在保持连接，不能代表上游已经产生真实业务数据。

## Key Context

- 当前任务目录：`.trellis/tasks/08-28-relay-stream-stability-keepalive`；任务状态为 `planning`；PR 目标基线已设置为 `build-bak`。
- 当前代码入口：`controller/relay.go` 中 `helper.GetAndValidateRequest` 成功后才生成 `RelayInfo` 并启动非流式 keepalive。
- 流式上游响应头等待阶段入口：`relay/channel/api_request.go` 的 `DoRequest` / `doRequest`。
- 流式正文读取和 ping 入口：`relay/helper/stream_scanner.go` 的 `StreamScannerHandler`。
- 非流式 JSON 保活入口：`relay/helper/non_stream_keepalive.go` 的 `StartNonStreamKeepAlive`。
- 需要遵守的规范：`.trellis/spec/backend/relay-nonstream-keepalive.md`、`.trellis/spec/backend/relay-alpha-search-compact.md`、`.trellis/spec/backend/relay-billing-model.md`。

## Risks / Deferred

- 如果过早提交 SSE header，后续上游错误只能通过流式错误事件表达；因此只能在确认流式响应后启动。
- 如果 provider 长时间只发送 comment/空行而不发送业务数据，本任务会按“真实业务数据优先”的策略更早暴露 idle timeout。
- 请求体上传阶段的 Cloudflare 取消仍需代理 request buffering、客户端上传稳定性或请求体大小/上传耗时治理配合。

## Acceptance

- 流式请求等待上游响应头超过 `ping_interval_seconds` 时，下游能收到协议合法的 SSE comment keepalive。
- 流式请求收到上游响应头后，在上游有效数据短暂静默期间，下游能继续收到 SSE comment keepalive。
- 本地 keepalive 不会重置真实上游 idle timeout；上游长期没有有效数据时仍按 `constant.StreamingTimeout` 结束并记录 timeout。
- 非流式 JSON 保活仍只输出 JSON 合法空白，且允许列表、状态码提交和最终 JSON 可解析行为不回退。
- 请求体读取/校验阶段的 `unexpected EOF` 不会被误报为流式 idle timeout，并有可定位的日志分类。
- 现有流式完成、客户端断开、上游错误、渠道重试、Responses Compact 透传和计费相关测试不回退。
- 新增或更新的测试覆盖 TTFB keepalive、流正文 keepalive、idle timeout 不被 keepalive 掩盖、非流式边界不变。
- 至少运行定向验证命令并记录结果：`go test ./relay/helper ./relay/channel ./controller`；如实现触及共享计费或请求读取链路，则扩大验证范围。

## Next Step

- 你确认 Brief 后，运行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/08-28-relay-stream-stability-keepalive`，再进入实现阶段复查规范和当前代码差距。
