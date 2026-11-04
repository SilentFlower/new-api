# 修复 Responses 首帧超时配置与异常流终态记录

## Goal

支持配置 Responses WebSocket 首帧等待时间，并让 Responses SSE 的业务失败和不完整响应在日志中准确显示，减少首帧超时与零输出记录带来的排障误判。

## Background

- 用户明确要求在一个任务内修复两项问题，并指定首帧等待默认值为 60 秒。
- 首帧期限目前固定为 30 秒，见 controller/responses_websocket.go:32、controller/responses_websocket.go:82；线上请求 202611040349540183910708268d9d68Ks8DjZI 在该阶段耗时 30.001578991s 后失败。
- SSE 请求 202611040325206263584648268d9d6SfYJevuV 的上游实际执行 req_01a1a4986baa71c0acd6f71195e23259 因连接异常断开而 incomplete，new-api 却记录 done/ok。业务事件处理见 relay/channel/openai/relay_responses.go:80，日志分类见 service/log_info_generate.go:184。
- 两项问题处于不同阶段，详细服务器证据与关联 ID 校正记录见 research/findings.md。

## Requirements

### R1：首帧等待配置

- 新增环境变量 RESPONSES_WEBSOCKET_FIRST_FRAME_TIMEOUT_SECONDS，单位秒，默认 60。
- 进程启动时读取；接受可安全转换为读取期限的正整数。未配置、空值、非法值、非正数及会导致时长溢出的数值均回退至 60 秒。
- 仅影响客户端 WebSocket 握手后等待首个完整业务消息的期限，保留后续首帧校验和读取期限清理行为。
- 提供环境变量示例和中文配置说明。

### R2：Responses SSE 业务终态记录

- response.failed、response.incomplete、response.cancelled、response.canceled、response.error 和顶层 error 必须让最终 stream_status.status 为 error，并记录稳定、可识别的异常原因。
- 仅收到 [DONE] 或自然 EOF、但未观察到 response.completed 或兼容的 response.done 时，若没有既有取消、超时或读取错误，应记录缺失业务完成事件。
- 传输结束标记不得掩盖业务失败；end_reason 继续表达传输结束原因，业务异常通过已有 status、error_count、errors 表达。
- 保留已有客户端取消、超时和解析错误，避免覆盖或重复归因。

### R3：转发与计费兼容

- 原始上游业务事件继续沿用既有转发行为，不新增错误帧、正文重写或流开始后的透明重试。
- 保留现有 usage 提取、工具计数、预扣、结算及退款行为。本轮不因日志分类变化调整费用或日志类型。
- 成功预热、合法零 usage、仅工具调用和按次搜索不因正文或输出 Token 为零被统一判错。
- 新增错误诊断不得包含原始请求、响应正文、上游原始 message、凭据或对话内容。

## Non-Goals

- 不修改 Codex Proxy、Nginx、客户端、其他协议的公共扫描器结束语义或连接心跳策略。
- 不新增前端页面、翻译键、数据库字段或迁移。
- 不修复上游请求 ID 关联策略，不回填历史日志，不重算历史账单。
- 不执行线上部署、重启或生产数据变更。

## Acceptance Criteria

- [x] AC1 / R1：未配置及无效配置均得到 60 秒；有效正整数配置生效，超大输入不能产生负时长。
- [x] AC2 / R1：真实 WebSocket 连接在配置的短期限内未发送首帧时触发现有错误封套；正常及时发送不受影响。
- [x] AC3 / R2：失败或不完整事件随后出现 [DONE]、EOF，最终日志仍为 status=error，且可见对应异常原因。
- [x] AC4 / R2：缺少业务完成事件的正常传输收尾被标记不完整；已有取消、超时、读取或解析错误保留原始归因。
- [x] AC5 / R3：completed/done 成功、合法零 usage、预热、工具调用的正常状态和既有 usage 保持一致；搜索契约保持。
- [x] AC6 / R3：下游收到的事件及现有计费结果保持；新增诊断不泄露原始正文和凭据。
- [x] AC7：相关 Go 测试、受影响包 race 检查、构建、vet 和 diff 检查通过，现有其他协议扫描器回归通过。
