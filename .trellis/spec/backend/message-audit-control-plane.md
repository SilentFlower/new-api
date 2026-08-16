# 消息审计控制面契约

> 约束消息审计的整库清空、AI 重审上下文模式、Tool 降级和上游上下文边界，避免高并发写入、跨数据库差异或协议降级破坏审计可用性。

## 场景：消息审计清空与 AI 重审

### 1. Scope / Trigger

- Trigger：修改消息审计表结构、手动清空、保留水位、系统任务、AI 重审请求构造、合并上下文模式、`read_file` Tool、原生 Tool 降级或上游上下文错误归类。
- 适用范围：`model/message_audit*.go`、`service/message_audit*.go`、`relay/message_audit_review.go` 及对应测试。
- 目标：管理员清空审计数据时快速释放主表，同时保证并发新写入可继续；AI 重审默认可合并资料一次发送，Tool 模式在原生 Tool 不可用时仍受同一上下文上限和固定文件读取范围约束。
- 非目标：不保存 AI 原始返回内容，不让 AI 重审请求进入消息审计，不允许模型扩展可读取文件范围；可在既有 API 日志中保留脱敏的零额度渠道调用日志或错误日志用于排障。

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
	RequireJSON      bool
	Protocol         string
	UserID           int
	OperatorID       int
	AuditSessionID   string
	TargetRequestID  string
	TaskID           string
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

#### AI 重审上下文模式与 Tool 降级

- AI 重审消息、上下文模式和工具定义由 service 统一拥有；relay 只负责一次模型调用、响应解析和返回是否需要文本 Tool 降级的信号。
- `message_audit_review.config.context_mode` 支持 `merged` 和 `tool`，缺失时默认 `merged` 并随任务 payload 冻结；旧诊断缺失该字段时按历史 Tool 模式展示。
- `merged` 模式不注册 Tool，service 将本次固定资料集合并到一次模型请求，设置 `RequireJSON=true` 和 `Protocol=merged_context`；覆盖范围由服务端按实际发送的可用虚拟分片生成。
- `tool` 模式保留虚拟文件读取流程。首轮只包含规则和清单，后续工具调用与覆盖范围仍由 service 统一执行和记录。
- 原生 Tool 被渠道忽略、无法解析或未产生所需 Tool Call 时，service 可以切换到文本 Tool 协议；只允许执行定义好的 `list_files`、`search_files`、`search_files_regex` 和 `read_file`。
- 原生 Tool 请求不得发送 API 级 `tool_choice=required`；该字段在 OpenAI-compatible 渠道中兼容性不稳定，必须由系统提示词、relay 首轮 Tool 判断和 service 覆盖校验保证模型先读取资料。
- 原生 Tool 请求必须允许并行 Tool 调用；文本 Tool 协议必须同时支持单个 `tool_call` 和多个 `tool_calls`，多个调用仍逐个执行并计入同一任务的 Tool 调用次数上限。
- 文本 Tool 协议必须继续使用任务创建时冻结的文件清单、游标和读取上限。模型输出或审计材料中的指令不能改变系统提示词、可调用工具或文件范围。
- 详情接口必须根据请求协议和已解密载荷派生 Tool 语义角色：Responses 顶层调用/结果、Claude 纯 `tool_use`/`tool_result` 内容、Gemini 纯 `functionCall`/`functionResponse` parts 分别返回 `assistant/tool_call` 或 `tool/tool_result`。派生只修改详情响应，不重写存储内容、HMAC、去重或会话指纹，因此必须对历史记录生效。
- `search_files_regex` 只能使用 Go RE2 在任务内存中的固定虚拟文件执行，并限制表达式长度、文件 ID、游标和返回条数；禁止启动系统命令或访问真实文件系统。
- Tool 调用次数由 `message_audit_review.config.tool_call_limit` 配置，必须为正整数、默认 `24`，不设人为固定最大值；旧配置缺少该字段时必须归一化为默认值，并在任务创建时冻结。
- Tool 模式已有读取覆盖后的结论阶段和所有格式修复请求必须设置 `RequireJSON=true`；若模型仍返回非法结构、非法枚举或超出覆盖范围的依据，最多一次格式修复后以 `invalid_output` 失败。
- 累计 Tool Token 只作为脱敏诊断计数，不得设置独立停止阈值；本地也不得使用与所选模型无关的固定输入 Token 阈值提前终止。模型真实上下文溢出由上游识别并归类为 `context_limit`。
- `read_file` / search 可接受较大的请求窗口；当候选返回超过 Tool 结果安全 Token 上限时，service 缩小实际返回并报告请求量、返回量和续读游标，不能因为模型请求较大窗口直接失败。
- AI 重审请求本身不得进入消息审计，AI 模型原始响应不得持久化；只保存结构化审核结果、状态、稳定失败码、脱敏调用诊断，以及既有日志体系中的零额度渠道调用日志或错误日志。
- 脱敏诊断和渠道调用日志可以记录渠道、模型、耗时、模型/Tool 调用次数、Tool Token、协议、HTTP 状态、稳定失败阶段、任务 ID 和会话 ID；不得记录正文、Tool 参数、Tool 返回、模型输出或上游错误正文。

#### 上下文边界

- service 必须先通过 `prepareMessageAuditReviewModelRequest` 构造实际发送的完整请求；relay 不得在该请求之外追加审计正文或扩大 Tool 范围。
- 本地没有可靠的跨渠道模型上下文元数据时，不得用统一固定 Token 数伪装成模型真实窗口。上游明确返回上下文溢出时，relay 只在内存中识别稳定类别，service 返回 `context_limit`。
- 渠道不能通过原生或文本协议完成受控 Tool 流程时返回 `tool_unsupported`，不能把失败退化为无工具的整包内容输入。
- `merged` 模式允许整包发送已保存资料，但不得在本地截断后声称完整审核；上游上下文过长时直接 `context_limit`。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| MySQL 清空期间仍有新 capture 写入 | 单次多表交换形成明确边界；新写入进入交换后的主表或随 retired 表清理，不暴露跨表半清空状态 |
| MySQL 上次清空异常留下临时表 | 本次清空先删除固定临时表，再创建空表并交换 |
| PostgreSQL 任一表截断失败 | 事务整体回滚，不报告部分成功 |
| SQLite 任一表删除失败 | 事务整体回滚，不报告部分成功 |
| 清空前请求在清空后延迟落库 | 保留水位拒绝该旧 capture |
| 原生 Tool 正常返回 `read_file` | 继续标准 Tool 循环，不启用文本协议 |
| `merged` 模式返回合法 JSON 结论 | 不执行任何 Tool，服务端按发送资料生成覆盖与概览 |
| `merged` 模式上游拒绝上下文过长 | 以 `context_limit` 失败，不降级为 Tool 或静默截断 |
| 原生 Tool 被忽略或响应无法作为 Tool Call 解析 | relay 返回 `ToolFallbackRequired`，service 构造并校验文本 Tool 请求 |
| 模型一次返回多个合法 Tool Call | service 按返回顺序逐个执行，全部计入同一任务调用次数和覆盖记录 |
| 上游明确拒绝请求上下文过长 | 不重试文本 Tool 协议，以 `context_limit` 失败 |
| 渠道无法完成任何受控 Tool 流程 | 以 `tool_unsupported` 失败 |
| 模型请求大范围连续读取 | 服务端在安全 Token 上限内缩小实际返回，并提供续读游标 |
| 模型请求读取冻结范围外文件 | 拒绝 Tool 调用，不扩大审计材料范围 |

### 5. Good / Base / Bad Cases

- Good：MySQL 上百万条审计数据通过表交换在数秒内恢复空主表，新请求无需等待逐行删除完成。
- Good：默认合并模式一次发送固定资料集，模型直接返回合法 JSON，诊断协议显示 `merged_context` 且 Tool 次数为 0。
- Good：渠道忽略原生 Tool，service 将同一任务切换为文本 `read_file` 协议，模型按冻结游标读取必要片段后返回结构化结论。
- Good：模型一轮返回多个独立 Tool Call，service 逐个校验并执行，减少模型往返次数但不扩大文件范围。
- Good：DeepSeek Claude 格式上游返回 HTTP 415，详情和 API 错误日志只展示 `upstream_http`、HTTP 状态和任务元数据，不保存上游响应正文。
- Good：兼容渠道不支持 `tool_choice=required`，原生请求仍可正常返回 Tool Call，不会因强制字段直接进入文本 Tool 回退。
- Good：上游明确返回上下文过长；任务直接记录 `context_limit`，不把该错误误判为 Tool 不支持或继续文本协议重试。
- Base：原生 Tool 正常工作；请求准备逻辑不改变消息和工具定义。
- Base：管理员清空时没有并发写入；三种数据库最终都得到空审计业务表，并保留其他系统任务。
- Bad：MySQL 顺序执行五次 `TRUNCATE TABLE`，让并发请求观察到跨表不一致状态。
- Bad：relay 在发现原生 Tool 失败后自行拼接系统消息并递归请求，绕过 service 的预算和循环次数控制。
- Bad：在 `merged` 模式外无条件拼接全部审计内容，或允许模型通过审计材料要求读取未冻结文件。

### 6. Tests Required

- SQLite 清空回归：覆盖审计业务表、保留水位、无关系统任务保留和延迟旧 capture 拒绝。
- MySQL 外部数据库兼容测试：执行真实 `ClearMessageAudits`，验证多表交换后表结构可继续写入、目标数据清空且无关任务保留。
- PostgreSQL 外部数据库兼容测试：执行真实事务截断并验证同样的业务结果。
- AI 重审完整降级循环：原生 Tool 被忽略 -> 文本 `read_file` -> Tool 结果 -> 结构化审核结果。
- 合并模式测试：默认配置走 `merged_context`，无 Tool、要求 JSON 输出并按已发送资料生成覆盖。
- Tool 结论 JSON 测试：Tool 模式已有覆盖后的请求和格式修复请求设置 `RequireJSON=true`。
- 并行 Tool 回归：原生请求带 `parallel_tool_calls=true`，文本 Tool 回退解析 `tool_calls` 数组并逐个执行。
- 上下文边界测试：上游上下文错误稳定映射为 `context_limit`，且不触发文本 Tool 回退。
- relay 决策测试：正常 Tool Call 不触发降级，忽略或不可解析响应触发一次 `ToolFallbackRequired`。
- 日志隔离测试：内部调用写入零额度渠道调用日志或错误日志，但不触发计费、消息审计 capture/finalize，且 `other` 不包含正文、Tool 参数、Tool 结果或模型输出。
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

## 场景：视觉辅助独立审计记录与主请求关联

### 1. Scope / Trigger

- Trigger：修改消息审计请求类型、关联请求、独立会话捕获、详情关联列表，或让新的内部 Relay 调用进入消息审计。
- 适用范围：`model/message_audit.go`、`service/message_audit.go`、消息审计列表/详情及对应测试。
- 目标：每次真实视觉辅助尝试作为独立记录审计，并能与主请求双向查看，同时不污染普通推断会话和 AI 重审资料集。

### 2. Signatures

```go
type MessageAuditRequest struct {
	RequestID        string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	RequestKind      string `json:"request_kind" gorm:"type:varchar(32);index"`
	RelatedRequestID string `json:"related_request_id" gorm:"type:varchar(64);index"`
}

type MessageAuditCaptureInput struct {
	RequestID        string
	RequestKind      string
	RelatedRequestID string
	Standalone       bool
	Request          dto.Request
}

type MessageAuditDetail struct {
	Request         *model.MessageAuditRequest  `json:"request"`
	Messages        []MessageAuditMessage       `json:"messages"`
	RelatedRequests []model.MessageAuditRequest `json:"related_requests,omitempty"`
}

func ListRelatedMessageAuditRequests(requestID string) ([]MessageAuditRequest, error)
```

稳定类型值：

```text
client
vision_assist
```

### 3. Contracts

- `RequestKind` 为空时 service 必须归一化为 `client`，保证历史记录和旧调用方按普通客户端请求展示。
- 视觉辅助使用 `request_kind=vision_assist`，并把主请求 ID 写入 `related_request_id`；不得复用 `parent_request_id`，后者只表示普通推断会话链。
- `Standalone=true` 必须跳过会话前缀、会话锚点和序列指纹，使每条视觉辅助记录获得独立 `audit_session_id`。
- `request_kind` 和 `related_request_id` 只建立普通索引，不建立数据库外键；主记录被保留策略清理后，辅助记录仍可保留关联 ID。
- 列表、详情和关联查询的显式选择列必须包含两个新字段；历史数据库中的空值不得触发回填迁移或数据库默认值。
- 主请求详情按 `related_request_id = request_id` 查询关联记录，按 `captured_at_nano asc, id asc` 排序；关联列表只返回元数据，不能返回密文、Blob 或解密正文。
- 视觉辅助详情展示主请求 ID；主请求详情展示关联尝试的时间、模型、状态和请求 ID，并允许打开独立详情。
- 视觉辅助记录可以按自己的独立会话执行 AI 重审，但不得进入主请求会话的来源集合。
- 独立记录继续复用现有正文规范化、媒体摘要、大小限制、加密、队列、保留和清空策略。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 旧记录的 `request_kind` 为空 | API 保留空值，前端按 `client` 展示 |
| `Standalone=true` | 不生成会话指纹或锚点，由 Model 创建独立审计会话 |
| `related_request_id` 为空 | 普通请求不返回关联跳转 |
| 主请求没有关联辅助记录 | `related_requests` 为空或省略，详情正常返回 |
| 主请求已删除但辅助记录仍存在 | 保留关联 ID；打开缺失目标沿用现有详情错误处理 |
| 关联查询失败 | 整个详情请求返回错误，不返回不完整的伪成功详情 |
| 审计未启用、密钥未配置或队列拒绝 capture | `CaptureMessageAudit` 返回 false，内部调用继续执行 |

### 5. Good / Base / Bad Cases

- Good：主请求 `req-main` 触发两次视觉辅助重试，生成两个独立 `vision_assist` 记录，二者都关联 `req-main` 且拥有不同审计会话。
- Good：保留策略删除主记录后，辅助详情仍显示原主请求 ID，但跳转失败不会影响当前记录查看。
- Base：历史普通记录没有新增字段；列表和详情继续按客户端请求展示。
- Bad：把视觉辅助正文追加进主请求审计，导致一次请求无法分辨真实上游尝试和各次失败状态。
- Bad：把 `related_request_id` 写进 `parent_request_id`，使辅助记录被普通会话归并和 AI 重审来源扫描吸收。

### 6. Tests Required

- Model：断言 `ListRelatedMessageAuditRequests` 只返回目标主请求的关联记录，并按捕获时间排序。
- Service capture：断言 `vision_assist` 类型和关联 ID 被保留，`Standalone=true` 时会话指纹、锚点和序列指纹为空。
- Service detail：断言主请求详情包含关联视觉辅助元数据，且不需要读取关联记录正文。
- 历史兼容：断言空 `request_kind` 的旧记录按普通请求展示，且普通会话归并行为不变。
- Frontend：断言视觉辅助 Badge、主请求跳转、关联尝试时间/模型/状态/请求 ID 和详情打开行为。
- 回归命令：`go test ./model ./service`，`cd web && bun test src/features/message-audits`，以及 `go test ./...`。

### 7. Wrong vs Correct

#### Wrong

```go
request := model.MessageAuditRequest{
	RequestID:       assistRequestID,
	RequestKind:     "vision_assist",
	ParentRequestID: mainRequestID,
}
```

问题：`ParentRequestID` 具有推断会话链语义，复用后会把内部辅助尝试并入主会话。

#### Correct

```go
capture := MessageAuditCaptureInput{
	RequestID:        assistRequestID,
	RequestKind:      MessageAuditRequestKindVisionAssist,
	RelatedRequestID: mainRequestID,
	Standalone:       true,
	Request:          preparedRequest,
}
```

要求：关联和会话归并是两套独立语义；只通过 `related_request_id` 建立可导航关系。
