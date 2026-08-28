# 增强 Relay 流式稳定性与保活策略

## Goal

将 Relay 流式长连接稳定性收敛为类似 cc2api 的两阶段保活策略：等待上游响应头期间和读取上游流正文期间都能向下游发送协议合法的 SSE keepalive，同时真实上游数据静默仍会触发 idle timeout。

本任务的直接价值是减少 Cloudflare 等代理在长耗时流式请求中因下游长时间无字节而报 524/超时的概率，并让日志能区分请求体读取失败、上游首包超时和上游流静默超时。

## Background / Confirmed Facts

- 线上排查到的 524 请求经过 Cloudflare Tunnel 直达 `localhost:3000`，不经过本机 Nginx；对应 new-api 日志在请求体读取/校验阶段返回 `unexpected EOF`，说明这类失败发生在 `GetAndValidateRequest` 之前，现有流式/非流式响应保活没有机会启动。
- 当前 `controller.Relay` 在 `helper.GetAndValidateRequest` 成功后才生成 `RelayInfo`，随后才调用 `helper.StartNonStreamKeepAlive`。
- 当前 `relay/channel/api_request.go` 会在 `info.UsesUpstreamStream()` 时设置 SSE header，并在等待 `relayClient.Do(req)` 返回期间按 `ping_interval_seconds` 发送 `: PING\n\n`。
- 当前 `relay/helper/stream_scanner.go` 会在读取上游流正文期间发送 SSE ping，并用 `constant.StreamingTimeout` 控制流式读取静默超时。
- 当前非流式 JSON 保活契约要求只写 JSON 合法 ASCII 空白，禁止把非流式响应改成 SSE comment。
- cc2api 的可借鉴点是将“下游 keepalive”和“真实上游流 idle 判断”拆开：keepalive 只保持客户端/代理链路活跃，不能把本地 keepalive 当作真实上游数据。

## Requirements

- R1. 流式请求在已完成请求体解析、已判定 `UsesUpstreamStream()` 后，等待上游响应头期间必须能够向下游发送 SSE comment keepalive。
- R2. 读取上游流正文期间必须继续发送 SSE comment keepalive，且 keepalive 不得伪造业务数据、完成事件、usage 或计费信息。
- R3. 上游流 idle timeout 必须以真实上游有效数据为判断依据；本地 keepalive 不得重置该 timeout，过滤掉的空行/comment 也不能掩盖真实业务数据长期缺失。
- R4. 保留现有非流式 JSON 空白保活契约：非流式 JSON 继续写 `\n` 前导空白，不改为 SSE，也不扩大到二进制、音频、Realtime/WebSocket 或未知 mode。
- R5. 日志和结束状态需要能区分请求体读取失败、等待上游响应头失败、上游流正文 idle timeout、keepalive 写失败和客户端主动断开；日志不得包含请求体、响应体、API Key 或用户内容。
- R6. 兼容现有后台配置：继续使用 `ping_interval_enabled`、`non_stream_keepalive_enabled`、`ping_interval_seconds` 和现有 streaming timeout；只有发现现有配置无法表达本需求时才新增配置。
- R7. 不改变渠道重试、错误体格式、状态码映射、Responses Compact 透传、usage 解析和计费结算语义。
- R8. 当前请求体读取阶段的 `unexpected EOF` 需要被明确标注为保活无法覆盖的阶段，不在请求体未读完前提交响应。

## Out Of Scope

- 不修改 Cloudflare Dashboard、Tunnel、DNS、Nginx 或外部代理配置。
- 不通过付费 Cloudflare Enterprise、DNS-only 子域、异步任务协议或新域名绕过代理。
- 不在请求体尚未完整读取/校验前向客户端提交响应。
- 不把所有非流式请求统一改成 SSE。
- 不重做 Relay 计费、渠道选择、审计或消息记录链路。

## Acceptance Criteria

- [ ] 流式请求等待上游响应头超过 `ping_interval_seconds` 时，下游能收到协议合法的 SSE comment keepalive。
- [ ] 流式请求收到上游响应头后，在上游有效数据短暂静默期间，下游能继续收到 SSE comment keepalive。
- [ ] 本地 keepalive 不会重置真实上游 idle timeout；上游长期没有有效数据时仍按 `constant.StreamingTimeout` 结束并记录 timeout。
- [ ] 非流式 JSON 保活仍只输出 JSON 合法空白，且允许列表、状态码提交和最终 JSON 可解析行为不回退。
- [ ] 请求体读取/校验阶段的 `unexpected EOF` 不会被误报为流式 idle timeout，并有可定位的日志分类。
- [ ] 现有流式完成、客户端断开、上游错误、渠道重试、Responses Compact 透传和计费相关测试不回退。
- [ ] 新增或更新的测试覆盖 TTFB keepalive、流正文 keepalive、idle timeout 不被 keepalive 掩盖、非流式边界不变。
- [ ] 至少运行定向验证命令并记录结果：`go test ./relay/helper ./relay/channel ./controller`；如实现触及共享计费或请求读取链路，则扩大验证范围。

## Notes

- 本任务只能降低“响应阶段下游空闲”导致的代理超时概率；对“客户端上传请求体阶段 Cloudflare 已取消连接”这类问题，代码层保活无法覆盖，仍需要代理 request buffering、客户端上传稳定性或请求体大小/上传耗时治理配合。
