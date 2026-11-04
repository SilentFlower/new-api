# 排障与设计依据

## 1. 证据范围

2026-11-04 在用户授权下，通过 SSH 对 main.flower-cli.com 做只读检查。未使用阿里云查询，未修改线上配置、数据库或容器，也未保存登录凭据。

- new-api 镜像：ghcr.io/silentflower/new-api:build-bak-latest，源码版本 5fff73bb1dbe00e081111ce621261499ff999923。
- Codex Proxy RS：3.38.1，源码版本 e0b7a841ca074f17e11dad73f39a159c75cb2fb1。
- new-api 多个 OpenAI 渠道指向 http://codex-proxy-rs:8081，方向为客户端 → new-api → Codex Proxy → 模型上游。
- Proxy 未配置总请求期限，未覆盖 ws_pool 默认配置；其上游连接池与流空闲默认 300 秒，属于不同于 new-api 客户端首帧等待的阶段。
- 所用 Proxy 版本源码在本地临时目录核验；任务实现不得依赖该临时目录。

## 2. 首帧超时

请求 202611040349540183910708268d9d68Ks8DjZI 的 Docker 日志：

    2026-11-04T03:50:24.020530457Z GET /v1/responses 30.001578991s

controller/responses_websocket.go:32 的首帧期限为 30 秒；第 82 行设置期限，第 86 行统一包装读取失败。失败发生在首帧解析、选渠、预扣和上游连接之前。

git blame 确认首帧常量与 GET /v1/responses 入口由 4f41400fd6 于 2026-07-16 的 build 定制提交加入。原版基线没有这个 Responses WebSocket 入口。

## 3. 异常 SSE 被记为正常

new-api 请求：202611040325206263584648268d9d6SfYJevuV，POST /v1/responses，渠道 132。

按实际时间、渠道和 Proxy Key 名称重新核验的实际执行：

- Proxy ID：req_01a1a4986baa71c0acd6f71195e23259。
- 开始：2026-11-04T03:26:54.890796Z；结束：03:27:21.700381Z。
- upstream.read.failed：reset_without_closing_handshake，最后事件 response.output_item.added，实际执行约 26.8 秒。
- Proxy outcome=incomplete、error_kind=upstream_unavailable。
- new-api 于 03:27:21.704Z 结束，stream_status 为 status=ok、end_reason=done，输入/输出/额度均为零。

这证明需要修复业务失败观测；现有证据不能确定模型上游异常断开的网络根因。

## 4. 零输出分类与关联校正

固定窗口为 UTC 03:04:22.769321–04:04:22.769321。new-api 的 Proxy 渠道共有 40 条输出 Token 为零的消费记录：

| 数量 | 证据分类 |
| --- | --- |
| 33 | context canceled / client_gone / scanner_error，对应连接取消 |
| 4 | /v1/alpha/search，按次搜索，零 Token 正常 |
| 2 | WebSocket 记录的输入为 2、输出为 0，时间与 Proxy prewarm 执行吻合 |
| 1 | 第 3 节的异常上游断开，被误标 done/ok |

Proxy 同完成时间窗口内成功 turn 1683 条、普通 inference 201 条、compaction 24 条，输出 Token 为零均为 0；另有 2 条成功 prewarm，输出均为 0。此统计不代表所有用户可见空白都已解释，也不能把连接取消直接归因于用户主动操作。

注意：直接拿 new-api 的 upstream_request_id 关联 Proxy 主键曾指向更早的成功请求，不能据此认定当前请求已经成功。时间和渠道复核后，一条实际为取消，另一条为第 3 节的 incomplete。现有 ID 关联策略另行处理，本任务不扩大修复范围。

## 5. 本地代码事实

- relay/channel/openai/relay_responses.go:95 起解码并转发事件；completed/done 提取 usage，failed/incomplete/cancelled/canceled 目前主要清理图像计数，未记录业务错误。
- relaykit/dto/openai_response.go:386 的 ResponsesStreamResponse 没有顶层 Error 字段；无需为记录事件类型而修改 DTO。
- relay/helper/stream_scanner.go:258、284 将 [DONE] 设为 Done，第 296 行将 EOF 设为 EOF。
- relay/common/stream_status.go:45 使用 sync.Once 保存首个结束原因；扫描 goroutine 可能先于回调观察到传输结束。
- 同文件第 55 行 RecordError 是现成的受锁保护错误记录入口，第 70 行 HasErrors 可供最终分类。
- service/log_info_generate.go:184 根据 IsNormalEnd 与 HasErrors 决定 stream_status.status，并输出现有 errors；无需增加日志字段。
- web/src/features/usage-logs/components/timing-metrics-cell.tsx:161、dialogs/details-dialog.tsx:1099 已在 status 非 ok 时显示异常及错误列表，无需前端改动。
- service/text_quota_stream_guard.go 以 ClientGone 和本地估算路径保护费用；不改变 EndReason 可以保留该保护。
- service/text_quota.go:75 允许 Token 为零但存在工具调用附加费；不能用零 Token 作为异常判据。

## 6. 方案取舍

采用 Responses 独立观察器 + 现有 RecordError，使业务失败与传输 [DONE]/EOF 并存。避免全局改变 SetEndReason、EOF 正常性或扫描器并发组织，降低对其他协议和上游同步的影响。

首帧默认值使用用户指定的 60 秒。配置读取沿用启动配置模式，新增正数与时长转换溢出保护。未知事件仍转发，合法零 usage 和工具调用不判错。

## 7. 参考

- .trellis/spec/guides/build-upstream-friendly-customization.md
- .trellis/spec/backend/relay-stream-keepalive.md
- .trellis/spec/backend/relay-alpha-search-compact.md
- Codex Proxy 3.38.1 连接预算：https://github.com/zyycn/codex-proxy-rs/blob/v3.38.1/backend/crates/gateway-api/src/openai/responses/websocket/connection/frame.rs
