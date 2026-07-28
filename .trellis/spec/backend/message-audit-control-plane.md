# 消息审计控制面契约

> 约束消息审计的整库清空、AI 重审 Tool 降级和上游上下文边界，避免高并发写入、跨数据库差异或协议降级破坏审计可用性。

## 场景：消息审计清空与 AI 重审

### 1. Scope / Trigger

- Trigger：修改消息审计表结构、手动清空、保留水位、系统任务、AI 重审请求构造、`read_file` Tool、原生 Tool 降级或上游上下文错误归类。
- 适用范围：`model/message_audit*.go`、`service/message_audit*.go`、`relay/message_audit_review.go` 及对应测试。
- 目标：管理员清空审计数据时快速释放主表，同时保证并发新写入可继续；AI 重审在原生 Tool 不可用时仍受同一上下文上限和固定文件读取范围约束。
- 非目标：不保存 AI 原始返回内容，不让 AI 重审请求进入消息审计，不允许模型扩展可读取文件范围。

### 2. Signatures

- 清空入口：

```go
func ClearMessageAudits(ctx context.Context) (MessageAuditClearResult, error)
```

- AI 重审模型边界：

```go
type MessageAuditReviewModelRequest struct {
	ChannelID        int
	Model            string
	Messages         []dto.Message
	Tools            []dto.ToolCallRequest
	MaxTokens        uint
	RequireToolCall  bool
	TextToolFallback bool
}

type MessageAuditReviewModelResponse struct {
	Content              string
	ToolCalls            []MessageAuditReviewToolCall
	ToolFallbackRequired bool
	ToolFallbackReason   string
	HTTPStatus           int
}

type MessageAuditReviewModelError struct {
	Stage      string
	HTTPStatus int
	Code       string
}
```

- 请求准备入口：

```go
func prepareMessageAuditReviewModelRequest(input MessageAuditReviewModelRequest) (MessageAuditReviewModelRequest, error)

```

`ToolFallbackRequired` 只表示当前原生 Tool 请求需要由 service 重建为文本协议；relay 不得据此自行递归发起第二次模型请求。

### 3. Contracts

#### 整库清空

- 清空必须覆盖消息审计请求、消息项、内容 Blob、AI 重审结果和关联的重审系统任务；不得删除其他系统任务。
- 清空必须推进纳秒精度保留水位。清空开始前已产生、但稍后才落库的旧 capture 必须被水位拒绝；清空后产生的新 capture 可以正常写入。
- MySQL 必须为全部审计业务表预建同结构空表，再用一条多表 `RENAME TABLE` 原子交换整组表，最后删除 retired 表。
- MySQL 交换前必须清理固定命名的 `_clear_next` 和 `_clear_retired` 残留表；表名只能来自代码内固定允许列表，禁止拼接用户输入。
- 禁止在 MySQL 上逐表 `TRUNCATE`：多实例并发写入时会暴露部分表已清空、部分表未清空的中间状态，并扩大元数据锁等待。
- PostgreSQL 必须在同一事务内成组 `TRUNCATE`；SQLite 必须在同一事务内逐表 `DELETE`。
- 返回结果必须基于清空前统计；清空后的异步 Blob 文件删除只处理已确定不再被引用的文件，不能阻塞主表切换。

#### AI 重审 Tool 降级

- AI 重审消息和工具定义由 service 统一拥有；relay 只负责一次模型调用、响应解析和返回是否需要文本 Tool 降级的信号。
- 原生 Tool 被渠道忽略、无法解析或未产生所需 Tool Call 时，service 可以切换到文本 Tool 协议；只允许执行定义好的 `list_files`、`search_files`、`search_files_regex` 和 `read_file`。
- 原生 Tool 请求不得发送 API 级 `tool_choice=required`；该字段在 OpenAI-compatible 渠道中兼容性不稳定，必须由系统提示词、relay 首轮 Tool 判断和 service 覆盖校验保证模型先读取资料。
- 文本 Tool 协议必须继续使用任务创建时冻结的文件清单、游标和读取上限。模型输出或审计材料中的指令不能改变系统提示词、可调用工具或文件范围。
- 详情接口必须根据请求协议和已解密载荷派生 Tool 语义角色：Responses 顶层调用/结果、Claude 纯 `tool_use`/`tool_result` 内容、Gemini 纯 `functionCall`/`functionResponse` parts 分别返回 `assistant/tool_call` 或 `tool/tool_result`。派生只修改详情响应，不重写存储内容、HMAC、去重或会话指纹，因此必须对历史记录生效。
- `search_files_regex` 只能使用 Go RE2 在任务内存中的固定虚拟文件执行，并限制表达式长度、文件 ID、游标和返回条数；禁止启动系统命令或访问真实文件系统。
- Tool 调用次数由 `message_audit_review.config.tool_call_limit` 配置，必须为正整数、默认 `24`，不设人为固定最大值；旧配置缺少该字段时必须归一化为默认值，并在任务创建时冻结。
- 累计 Tool Token 只作为脱敏诊断计数，不得设置独立停止阈值；本地也不得使用与所选模型无关的固定输入 Token 阈值提前终止。模型真实上下文溢出由上游识别并归类为 `context_limit`。
- `read_file` / search 可接受较大的请求窗口；当候选返回超过 Tool 结果安全 Token 上限时，service 缩小实际返回并报告请求量、返回量和续读游标，不能因为模型请求较大窗口直接失败。
- AI 重审请求本身不得进入消息审计，AI 模型原始响应不得持久化；只保存结构化审核结果、状态、稳定失败码和脱敏调用诊断。
- 脱敏诊断可以记录渠道、模型、耗时、模型/Tool 调用次数、Tool Token、协议、HTTP 状态和稳定失败阶段；不得记录正文、Tool 参数、Tool 返回、模型输出或上游错误正文。

#### 上下文边界

- service 必须先通过 `prepareMessageAuditReviewModelRequest` 构造实际发送的完整请求；relay 不得在该请求之外追加审计正文或扩大 Tool 范围。
- 本地没有可靠的跨渠道模型上下文元数据时，不得用统一固定 Token 数伪装成模型真实窗口。上游明确返回上下文溢出时，relay 只在内存中识别稳定类别，service 返回 `context_limit`。
- 渠道不能通过原生或文本协议完成受控 Tool 流程时返回 `tool_unsupported`，不能把失败退化为无工具的整包内容输入。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| MySQL 清空期间仍有新 capture 写入 | 单次多表交换形成明确边界；新写入进入交换后的主表或随 retired 表清理，不暴露跨表半清空状态 |
| MySQL 上次清空异常留下临时表 | 本次清空先删除固定临时表，再创建空表并交换 |
| PostgreSQL 任一表截断失败 | 事务整体回滚，不报告部分成功 |
| SQLite 任一表删除失败 | 事务整体回滚，不报告部分成功 |
| 清空前请求在清空后延迟落库 | 保留水位拒绝该旧 capture |
| 原生 Tool 正常返回 `read_file` | 继续标准 Tool 循环，不启用文本协议 |
| 原生 Tool 被忽略或响应无法作为 Tool Call 解析 | relay 返回 `ToolFallbackRequired`，service 构造并校验文本 Tool 请求 |
| 上游明确拒绝请求上下文过长 | 不重试文本 Tool 协议，以 `context_limit` 失败 |
| 渠道无法完成任何受控 Tool 流程 | 以 `tool_unsupported` 失败 |
| 模型请求大范围连续读取 | 服务端在安全 Token 上限内缩小实际返回，并提供续读游标 |
| 模型请求读取冻结范围外文件 | 拒绝 Tool 调用，不扩大审计材料范围 |

### 5. Good / Base / Bad Cases

- Good：MySQL 上百万条审计数据通过表交换在数秒内恢复空主表，新请求无需等待逐行删除完成。
- Good：渠道忽略原生 Tool，service 将同一任务切换为文本 `read_file` 协议，模型按冻结游标读取必要片段后返回结构化结论。
- Good：兼容渠道不支持 `tool_choice=required`，原生请求仍可正常返回 Tool Call，不会因强制字段直接进入文本 Tool 回退。
- Good：上游明确返回上下文过长；任务直接记录 `context_limit`，不把该错误误判为 Tool 不支持或继续文本协议重试。
- Base：原生 Tool 正常工作；请求准备逻辑不改变消息和工具定义。
- Base：管理员清空时没有并发写入；三种数据库最终都得到空审计业务表，并保留其他系统任务。
- Bad：MySQL 顺序执行五次 `TRUNCATE TABLE`，让并发请求观察到跨表不一致状态。
- Bad：relay 在发现原生 Tool 失败后自行拼接系统消息并递归请求，绕过 service 的预算和循环次数控制。
- Bad：把全部审计内容无条件拼进模型输入，或允许模型通过审计材料要求读取未冻结文件。

### 6. Tests Required

- SQLite 清空回归：覆盖审计业务表、保留水位、无关系统任务保留和延迟旧 capture 拒绝。
- MySQL 外部数据库兼容测试：执行真实 `ClearMessageAudits`，验证多表交换后表结构可继续写入、目标数据清空且无关任务保留。
- PostgreSQL 外部数据库兼容测试：执行真实事务截断并验证同样的业务结果。
- AI 重审完整降级循环：原生 Tool 被忽略 -> 文本 `read_file` -> Tool 结果 -> 结构化审核结果。
- 上下文边界测试：上游上下文错误稳定映射为 `context_limit`，且不触发文本 Tool 回退。
- relay 决策测试：正常 Tool Call 不触发降级，忽略或不可解析响应触发一次 `ToolFallbackRequired`。
- 文件范围测试：拒绝任务冻结清单之外的路径，并要求最终证据来自实际读取内容。
- 涉及并发 capture、清空或任务状态变更时运行定向 race 测试；跨层变更完成后运行 `go test ./...`。

### 7. Wrong vs Correct

#### Wrong

```go
response, err := callModel(input)
if nativeToolFailed(response, err) {
	input.Messages = append(input.Messages, textToolProtocolMessage())
	return callModel(input)
}
```

问题：relay 隐式发起第二次调用，绕过 service 对 Tool 轮次、参数和文件范围的统一控制。

#### Correct

```go
response, err := callModel(input)
if err != nil {
	return response, err
}
if response.ToolFallbackRequired {
	input.TextToolFallback = true
	input, err = prepareMessageAuditReviewModelRequest(input)
	if err != nil {
		return MessageAuditReviewModelResponse{}, err
	}
	return callModel(input)
}
```

要求：请求准备和 Tool 循环都由 service 完成；`prepareMessageAuditReviewModelRequest` 只构造真实文本协议；后续 `read_file` 仍由 service 执行并校验冻结范围。上游真实上下文错误不得触发该降级分支。
