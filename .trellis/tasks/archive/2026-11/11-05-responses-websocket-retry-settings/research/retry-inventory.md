# WebSocket 重试边界清单

以下为本次实现后的源码清单；首帧合同已由真实连接验证，其余分支维持既有策略并运行现有回归，不表示任意客户端已实现自动重连。

## Responses WebSocket

表内未写完整路径的行号均属于 `controller/responses_websocket.go`

| 触发条件 | 当前行为与依据 | 处理方向 |
| --- | --- | --- |
| upgrade 失败 | 记录并返回，未进入业务重试；`:75` | 保留入口失败边界 |
| 首帧超时或其他读取错误 | 超时返回 `408 responses_websocket_first_frame_timeout` 并关闭 `1013`；超限与关闭分别处理；`:82`、`:87`、`controller/responses_websocket_first_frame.go` | 已实现并通过真实连接验证，恢复需要重新建连并重发完整请求 |
| 首帧类型、JSON、type 或 model 非法 | `parseResponsesWebSocketTurn` 返回 400/SkipRetry；`:124` | 保留输入拒绝 |
| 请求验证、序列化或转换失败 | 首轮准备链按错误处理，部分显式 SkipRetry；`:259`、`:325`、`:329` | 区分客户端输入与渠道错误，不统一放开 |
| Token 模型权限、指定渠道无效或禁用 | 中间件返回不可重试错误；`middleware/responses_websocket_channel.go` | 不绕过权限和指定渠道 |
| 首次选渠无可用渠道 | 直接写选渠错误返回，未进入建连重试循环；`:96` | 区分无可选目标与上游调用失败，保留已有 503 语义 |
| 并发 guard 获取失败或租约失效 | 建连直接 break；运行中写错、退款并退出；`:189`、`:444` | 不通过换渠绕过限制 |
| 上游握手、首包写入、首个业务错误或未交付事件前上游关闭 | 已有重试入口，受次数和 `shouldRetry` 约束；`:158`、`:214`、`:535`、`:568` | 复用现有机制及资源生命周期 |
| 已交付任意上游事件 | `downstreamEventWritten=true`，后续失败不透明重试；`:539`、`:571`、`:596` | 包括 `response.created`，没有正文不等于没有交付 |
| 客户端断开、上下文取消或向客户端写入失败 | 退款并结束，不重放；`:449`、`:453`、`:589` | 保留取消和连接归属 |
| 正在执行时收到另一条 response.create | 409/SkipRetry 并退出；`:461` | 保留单活跃轮次限制 |
| 后续轮次准备失败 | 直接写错、退款并返回，未走首轮 prepare 的重试循环；`:495` | 先核定错误归属、原连接上下文和未发送边界，再判断是否优化 |
| 后续轮次写入旧上游失败 | 关旧连接、释放 guard，从重试索引 1 重新连接；`:506` | 已有恢复，检查次数耗尽与增量约束 |
| 非 response.create 控制消息写入失败 | 502 并退出，无控制消息重放；`:528` | 不把依赖原连接的控制操作作为新推理请求 |
| 原渠道失去资格且存在 previous_response_id | `400 previous_response_not_found + SkipRetry`；`controller/responses_websocket_recovery.go:28` | 调用方携完整上下文恢复，不跨渠道发送原增量 |
| response.incomplete / cancelled / canceled | 不在自动业务错误重试集合，终态转发并退款；`:665`、`:674` | 不把所有不完整或取消都重放 |
| completed/done 缺少 usage | 记录 `bad_response_body` 并退款，不重放已交付终态；`:641` | 保留账务边界 |
| 空闲期间上游关闭 | 无 currentTurn 时没有请求可重放，透传关闭并结束；`:535` | 区分连接恢复与请求重试 |
| Compact 渠道能力门禁拒绝 | 在 dial 和预扣前拒绝，不作为 channel error 进入 failover；`controller/responses_compact_passthrough_websocket.go` 及现有生命周期规范 | 不借重试绕过 Compact 能力声明 |

## 通用 shouldRetry 约束

依据：`controller/relay.go:360`、`setting/operation_setting/status_code_ranges.go`

- 会话亲和失败策略可要求立即停止重试
- `channel:*` 在 `shouldRetry` 中优先判断，不能把后续规则不加区分地套在所有错误上；最终次数仍受外层循环上限约束
- 普通错误显式 `SkipRetry`、剩余次数不足或指定 `specific_channel_id` 时不重试
- 2xx 不重试；其余按配置规则判断，当前默认范围不包含 400 和 408
- 504、524 为状态码硬排除，`bad_response_body` 为错误码硬排除
- `common.RetryTimes` 缺省为 0；本轮未读取线上重试次数设置，不推断部署实例实际值
- 流内错误状态推导检查 `response.error.status_code`、`error.status_code`、`status_code`，其他封套形态需另行核对

## Realtime 入口差异

- `router/relay-router.go:78` 将 `/v1/realtime` 交给通用 `Relay`，`/v1/responses` 使用独立 handler
- Realtime upgrade 失败直接返回；`controller/relay.go:81`
- `relay/websocket.go:15` 的 `WssHelper` 在 adaptor 不存在时显式 SkipRetry，其余上游/响应错误交回通用 Relay
- 通用 Relay 对准备、选渠、并发和预扣等部分失败直接 break，并对最终错误使用 `shouldRetry`；`controller/relay.go:200` 附近
- 本任务仅说明差异，不改变 Realtime 实时音视频、消息重放或计费；具体 adaptor 仍需按实际调用链核实

## 验证证据与限制

- 首帧恢复已由 `TestResponsesWebSocketFirstFrameTimeout`、`TestResponsesWebSocketFirstFrameFailureCategories`、`TestResponsesWebSocketFirstFrameSettingKeepsExistingDeadline` 和 `TestResponsesWebSocketFirstFrameReconnectRecovery` 验证错误封套、关闭码、存量期限和完整请求重连
- 每个拟放宽分支说明：完整请求是否具备、是否已发送、是否已交付、是否依赖原连接、预扣及租约能否正确清理
- 未验证的客户端自动重试、多实例传播及真实上游计费不得标为通过

## 本次实施结论

- 只修改首帧读取失败这一行：超时使用 `408 / responses_websocket_first_frame_timeout / 1013`，通过新连接恢复；读取上限、正常关闭与其他读取失败分别处理，不统一提示重试。
- 首帧读取成功后的输入校验、选渠、上游重试、流内失败、增量恢复和终态策略均维持现有边界，并运行对应已有回归。
- 采用动态设置不改变任何重试次数或上游状态码规则，不以 `408` 自动扩展 `shouldRetry`。
- 已执行全量 Go 回归和相关包竞态检查；实际命令、浏览器结果及未覆盖环境见 [验证记录](verification.md)。
