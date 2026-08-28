# 增强 Relay 流式稳定性与保活策略 — Design

## Architecture

本任务按请求生命周期拆成三个边界，避免把不同阶段的超时混在一起处理：

1. 请求体读取/校验阶段：`controller.Relay` 调用 `helper.GetAndValidateRequest`。此时尚未生成 `RelayInfo`，不能知道最终是否是流式响应，也不能安全提交响应头；因此不启动响应保活。
2. 上游响应头等待阶段：`relay/channel.DoRequest` 在 `info.UsesUpstreamStream()` 为 true 时设置 SSE header，并在 `relayClient.Do(req)` 阻塞期间发送 SSE comment keepalive。
3. 上游流正文读取阶段：各 channel handler 调用 `helper.StreamScannerHandler`。该阶段转发真实上游数据，同时在下游空闲时发送 SSE comment keepalive；真实上游 idle timeout 与本地 keepalive 解耦。

## Boundaries

- `controller/relay.go`：Relay 生命周期入口，请求体解析失败、`RelayInfo` 生成、非流式 keepalive 启停、渠道重试和最终错误写入都在这里串起来。
- `relay/channel/api_request.go`：上游 HTTP 请求执行入口，覆盖等待上游响应头阶段的 SSE ping。
- `relay/helper/stream_scanner.go`：流正文扫描、SSE ping、stream end reason 和 idle timeout 的核心位置。
- `relay/helper/non_stream_keepalive.go`：非流式 JSON 空白保活实现，本任务只允许守住边界或补测试，不把它改成 SSE。
- `service/http.go`：最终响应复制与 `NonStreamKeepAliveWritten()` 协作，只有触及非流式元数据行为时才修改。

## Data Flow

1. `controller.Relay` 读取并校验请求体；失败则按普通错误返回，不启动响应阶段 keepalive。
2. `GenRelayInfo` 根据请求格式、mode 和 stream 标志生成 `RelayInfo`。
3. 非流式 JSON 且命中允许列表时，`StartNonStreamKeepAlive` 安装 JSON 空白 writer；流式请求不走该 writer。
4. 流式上游请求进入 `DoRequest`；在等待上游响应头期间，按配置向下游写 `: PING\n\n` 并 flush。
5. 上游响应返回后，channel handler 进入 `StreamScannerHandler`；真实上游有效数据由 data handler 处理，本地 ping 只通过共享写锁写入下游，不参与业务数据、usage 或 timeout 成功判定。

## Contracts

- SSE keepalive payload 继续使用现有 `helper.PingData` 的 `: PING\n\n`，仅用于流式响应。
- 非流式 keepalive payload 继续使用 `\n`，仅用于 JSON 前导空白。
- 流正文 idle timeout 应围绕真实有效上游数据更新。实现时需要复查当前 scanner 是否在空行、comment、非 `data:` 行或本地 ping 上重置 timeout；如果会重置，需要调整。
- keepalive 写失败应停止对应 keepalive goroutine，并通过现有 stream status 或请求日志留下阶段信息。
- 不改变 Responses Compact V2 原始透传、错误事件写入、usage 提取和退款/结算策略。

## Compatibility

- 默认配置行为不扩大：后台开关仍由 `general_setting` 控制。
- `ping_interval_seconds <= 0` 继续回退到 `helper.DefaultPingInterval`。
- 已开启 `ping_interval_enabled` 的流式请求应获得增强行为；未开启时保持原行为。
- 已开启 `non_stream_keepalive_enabled` 的非流式 JSON 请求保持现有空白保活；未开启或历史缺字段保持关闭。

## Risks / Trade-offs

- 如果过早提交 SSE header，后续上游错误只能通过流式错误事件表达；因此只在已经确认请求为流式后启动。
- 如果把上游空行/comment 当作有效数据，可能掩盖真实业务内容长期不返回的问题；如果过于严格，少数 provider 只用 comment 表示存活时会更早触发 idle timeout。本任务按 cc2api 思路选择“真实业务数据优先”。
- 请求体上传阶段的 Cloudflare 取消无法由响应 keepalive 解决；任务完成后仍应保留运维侧 request buffering 建议。

## Rollback

- 变更应集中在 `relay/channel/api_request.go`、`relay/helper/stream_scanner.go` 和相关测试。出现兼容问题时，可回退流正文 idle 判断调整，保留不改变外部协议的日志增强。
