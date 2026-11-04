# 技术设计

## 1. 边界与数据流

本任务在 build-bak 分支完成两项 Responses 修复。首帧配置只作用于客户端 GET /v1/responses；终态观察只接入 OaiResponsesStreamHandler 已处理的 Responses SSE。既有 Compact 原始透传路径继续使用其独立观察器。

配置链路：环境变量 → common 的启动解析 → constant 中的配置值 → WebSocket 首帧 SetReadDeadline → 首帧读取完成后清除期限。

终态链路：上游 SSE → 现有 DTO 解码与转发 → 协议终态观察 → StreamStatus.RecordError → 现有日志生成器 → 现有日志列表异常图标和详情。

## 2. 首帧等待配置

- 环境变量：RESPONSES_WEBSOCKET_FIRST_FRAME_TIMEOUT_SECONDS。
- 默认值：60 秒；启动时解析，不新增动态后台配置。
- 在独立 constant 文件中提供初始默认值，避免未执行启动初始化的测试或嵌入入口得到零值。
- 在独立 common 文件中读取、规范化配置；common/init.go 的 initConstantEnv 只增加一次初始化调用。
- 复用 common.GetEnvOrDefault 解析整数，对非正数和超过 time.Duration 秒数可表示范围的值回退 60；不引入任意业务上限，不允许通过 0 意外禁用首帧期限。
- 控制器移除 30 秒固定常量，使用规范化后的配置设置读取期限。写超时、读取大小限制、首帧 JSON/type/model 校验、选渠和计费顺序保持。

## 3. Responses 业务终态观察

### 3.1 根因与最小修复

StreamScannerHandler 的扫描和回调运行在不同 goroutine。扫描器可以先读到 [DONE] 或 EOF，StreamStatus.SetEndReason 又使用 sync.Once，因此回调直接改结束原因既可能失效，也会影响其他协议的传输语义。

新增独立 Responses 观察器，利用已存在且带锁的 RecordError 记录业务异常。日志生成器已经通过 IsNormalEnd 与 HasErrors 计算 status；即便 end_reason 是 done，已有错误仍会使 status=error。无需修改共享扫描器、StreamStatus 类型、日志模型或前端。

### 3.2 观察规则

| 业务信号 | 记录方式 |
| --- | --- |
| response.completed 或兼容 response.done，且无显式失败状态 | 标记已完成，不按输出文本或 Token 数判错 |
| response.failed / response.incomplete / response.cancelled / response.canceled / response.error / error | 记录一次稳定协议异常，包含事件类型；后续 [DONE]/EOF 不消除错误 |
| 完成事件中出现显式 failed/incomplete/cancelled/canceled 状态 | 按异常终态记录，不能仅凭事件名认定成功 |
| 正常传输结束但没有业务完成事件，且没有已记录的协议或传输错误 | 收尾时记录缺少 Responses 完成事件 |
| 已有客户端取消、超时、读取错误或 JSON 解码错误 | 保留原始错误，不再追加“缺少完成事件”覆盖根因 |

观察器只在串行回调中更新状态，并在 StreamScannerHandler 清理、等待 goroutine 完成后做最终检查。明确异常在回调中立即 RecordError，使扫描器已有结束日志也能看到异常。仅缺少终态的情况在最终检查时补一条请求关联的安全诊断。

未知事件继续转发；不把工具调用、无正文输出或合法零 usage 当作错误。新增错误文案仅包含稳定事件类型和固定分类，不直接拼接上游原始 message 或整个 JSON。

### 3.3 对外行为与费用

- 沿用现有 sendResponsesStreamData，不改已有事件内容，不追加新错误封套，不把已提交的 HTTP 200 改成其他状态。
- 不向外层 controller 返回新增 NewAPIError，避免触发渠道重试、重复错误事件或改变结算路径。
- 保留当前 usage 提取、本地文本回退、工具计数、PostTextConsumeQuota 和客户端断开费用保护。
- end_reason 保留传输结束原因；最终 status 与 errors 表示业务失败。当前前端已按 status !== ok 显示异常，无需新增 UI 或 i18n。

## 4. 文件职责与上游同步边界

| 文件 | 计划职责或薄接入点 |
| --- | --- |
| 新建 constant/responses_websocket_env.go | 首帧配置及初始 60 秒默认值 |
| 新建 common/responses_websocket_env.go | 启动解析、正整数与时长溢出归一 |
| common/init.go | initConstantEnv 增加一次独立初始化调用 |
| controller/responses_websocket.go | 替换首帧读取期限来源，其他生命周期不动 |
| 新建 relay/channel/openai/relay_responses_stream_status.go | 完整承载 Responses 业务终态观察与最终检查 |
| relay/channel/openai/relay_responses.go | 建立观察器、观察解码事件、扫描结束后最终检查 |
| 新建 common/responses_websocket_env_test.go | 配置行为回归 |
| 新建 controller/responses_websocket_timeout_test.go | 真实连接首帧读取期限回归 |
| 新建 relay/channel/openai/relay_responses_stream_status_test.go | 协议事件、日志字段、转发及 usage 回归 |
| .env.example、README.zh_CN.md | 新配置说明及生效范围 |

不修改 relay/helper/stream_scanner.go、relay/common/stream_status.go、relaykit DTO、service 日志生成器和前端。业务逻辑置于独立文件，原有文件只保留初始化或观察接入点；复用现有配置解析、StreamStatus.RecordError、日志生成 API，不抽取上游流程。

## 5. 兼容、回滚与风险

- 无数据库迁移、无需历史数据兼容改造；历史 stream_status 不回填。
- 默认首帧期限从 30 改为用户指定的 60；可通过环境变量重新配置为 30。
- 上游同步时复核三处薄接入：启动环境解析、WebSocket 首帧读取、Responses SSE 回调与收尾；特别复核新增业务事件类型。
- 回滚为删除新增独立文件、撤回薄接入及对应文档；不修改线上配置或执行重启。
- 重点风险是误判预热/工具调用为失败，以及回调与 [DONE] 的时序竞争；使用协议终态而非 Token 数判定，并用真实扫描链路和 race 检查覆盖。
