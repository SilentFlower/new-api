# 实施计划

## 1. 开工门禁与范围

- [x] 创建 planning 任务，并记录用户指定的首帧默认值 60 秒。
- [x] 查证线上超时、异常流、零输出分类和错误 ID 关联风险，写入 research/findings.md。
- [x] 读取相关 DTO、StreamResult、StreamStatus、扫描器、日志生成与客户端断开费用保护。
- [x] 用 trellis-task-brief 完整展示 Brief，取得本任务规划确认。
- [x] 确认后执行 task.py start，再进入 trellis-route(target=implement) 和 trellis-before-dev。
- [x] 实施前按 before-dev 补齐包级规范，读取 code-reuse-thinking-guide；保留任务外已有脏文件。

本任务为后端修复，不包含线上部署、数据库操作、前端修改或费用政策调整。执行模式由确认后的 trellis-route 决定，不预先派发实现。

## 2. 首帧配置

- [x] 新建 constant/responses_websocket_env.go，声明 60 秒初始默认配置。
- [x] 新建 common/responses_websocket_env.go，解析 RESPONSES_WEBSOCKET_FIRST_FRAME_TIMEOUT_SECONDS，并处理无效、非正和时长溢出输入。
- [x] common/init.go 仅增加一次启动初始化调用。
- [x] controller/responses_websocket.go 仅替换首帧期限来源，保留读取后清零和既有错误封套。
- [x] 更新 .env.example 与 README.zh_CN.md。
- [x] 新增确定性配置表测，并以短配置期限测试真实 WebSocket 空首帧连接；不等待默认 60 秒，不用 sleep 模拟业务。

## 3. Responses SSE 状态修复

- [x] 新建 relay/channel/openai/relay_responses_stream_status.go，实现协议终态观察和扫描后检查。
- [x] 在 OaiResponsesStreamHandler 加入最薄观察调用，原有转发、usage 和工具计数流程保持。
- [x] 显式业务失败通过 RecordError 标记；不修改 SetEndReason 的首写语义。
- [x] 正常传输收尾而缺少业务完成事件时补异常；已有取消、超时和解析错误保持原始归因。
- [x] 新增诊断使用固定安全文案，不记录原始 message、响应正文或凭据。
- [x] 不新增返回给外层的 NewAPIError，不新增重试、退款或费用路径。

## 4. 验证场景

新增测试集中放在独立文件，沿用 require 做前置/致命断言、assert 做值断言。

| 场景 | 关键断言 |
| --- | --- |
| 环境变量未配置、空值、非法、0、负数、超大值 | 回退 60，时长不溢出 |
| 有效配置与真实 WebSocket 首帧等待 | 配置影响实际读取期限，正常首帧仍通过 |
| failed/incomplete/error + [DONE] 或 EOF | 最终日志 status=error，包含稳定原因；事件保持 |
| DONE/EOF 前没有完成事件 | 标记不完整，不能表现为正常业务成功 |
| completed/done + 有效 usage | 正常状态及 usage 保持 |
| 预热零输出、显式零 usage、仅工具调用 | 不因无正文/零输出被判错 |
| 已有 client_gone/timeout/解析错误 | 保留既有错误，费用保护不变 |
| 真实扫描回调先后顺序 | DONE 提前被扫描也不能消除业务错误 |

验证优先覆盖公开行为：使用实际 OaiResponsesStreamHandler、日志生成入口和真实 WebSocket；不只断言观察器私有字段。

## 5. 验证命令

按改动一次执行以下验证，失败修复后仅重跑相关项：

    go test ./common ./controller ./relay/channel/openai ./relay/helper ./relay/common ./service -count=1
    go test -race ./controller -run 'ResponsesWebSocket' -count=1
    go test -race ./relay/channel/openai -run 'Responses.*Stream|Responses.*Billing' -count=1
    go vet ./common ./constant ./controller ./relay/channel/openai ./relay/helper ./relay/common ./service
    go build ./...
    git diff --check

如果实施影响 relaykit 公共 API，则额外执行独立模块 GOWORK=off go build ./...；当前方案不修改该模块。

## 6. 复核、规范与交付

- [x] Check-All 复核错误终态、正常零输出、原始事件、无重复重试/结算，以及扫描器时序假设。
- [x] trellis-update-spec 在既有 Responses 和 SSE 保活契约中记录新增配置、传输结束与业务结果分离。
- [x] 保留本任务和原有其他任务文件的边界，提交/推送按后续明确指令及精确范围确认流程执行。

上游冲突面限制为 common/init.go、controller/responses_websocket.go 和 relay/channel/openai/relay_responses.go 的薄接入。回滚删除独立模块、测试和说明并撤销薄接入，不改历史日志。

## 已执行验证（2026-11-04）

- 定向回归：go test ./common ./controller ./relay/channel/openai -run 'TestResponsesWebSocketFirstFrameTimeout|TestResponsesStreamStatus' -count=1，退出码 0。
- 六包完整测试：go test ./common ./controller ./relay/channel/openai ./relay/helper ./relay/common ./service -count=1，退出码 0。
- 根 Relay 与搜索/Responses 回归：go test ./relay -run 'AlphaSearch|Responses' -count=1，退出码 0。
- 合并执行两组 race 回归：go test -race ./controller ./relay/channel/openai -run 'ResponsesWebSocket|Responses.*Stream|Responses.*Billing' -count=1，退出码 0。
- 受影响包 go vet、go build ./...、全部 9 个变更 Go 文件 gofmt -l、git diff --check 均通过。
- Check-All 画像：interactive，requested=auto，effective=full，confidence=high；实现涉及首帧期限、SSE 协议终态和跨层日志状态。
- 三个检查维度通过；CHK=0、FBK=0。DOC-001 同步本计划的完成状态及 task.json 的过期规划说明；生产验收留待发布后执行。
- 规范更新结果为 written：仅修改 relay-alpha-search-compact.md 和 relay-stream-keepalive.md，规范 diff 检查与写入边界校验通过。

## 交付状态

- 用户已确认精确的业务与任务记录提交计划，业务修复已推送至 origin/build-bak。
- 任务记录在同一次确认范围内同步完成态并发布；本轮不执行服务器部署。
