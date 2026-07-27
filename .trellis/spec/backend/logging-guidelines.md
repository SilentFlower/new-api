# 日志规范

> 本项目的日志库、日志级别、格式和敏感数据处理规范。

---

## 概述

项目使用 **Go 标准库**（`fmt.Fprintf` 到 `gin.DefaultWriter`/`gin.DefaultErrorWriter`），**不使用**第三方日志库（无 logrus、zap、slog）。

日志分为两层：

| 层级 | 包 | 用途 |
|------|-----|------|
| **系统日志** | `common/sys_log.go` | 低层系统事件，无上下文/请求 ID |
| **应用日志** | `logger/logger.go` | 上下文感知日志，包含请求 ID |

此外，项目还有**数据库审计日志**（`model/log.go`），持久化到 `logs` 表。

---

## 日志初始化

### 文件日志

通过 `--log-dir` CLI 参数控制（默认 `./logs`）：

- 日志文件命名：`oneapi-{timestamp}.log`（如 `oneapi-20260328150405.log`）
- 输出目标：同时写入 **stdout/stderr** 和**文件**（`io.MultiWriter`）
- 自动轮转：每 1,000,000 条日志自动创建新文件

### 调试模式

通过 `DEBUG=true` 环境变量启用，控制 `LogDebug` 是否输出。

---

## 日志级别

### 系统层（`common/sys_log.go`）

| 函数 | 标签 | 输出 | 用途 |
|------|------|------|------|
| `common.SysLog(s)` | `[SYS]` | stdout | 通用系统消息 |
| `common.SysError(s)` | `[SYS]` | stderr | 系统错误 |
| `common.FatalLog(v...)` | `[FATAL]` | stderr + `os.Exit(1)` | 致命错误，终止进程 |

### 应用层（`logger/logger.go`）

| 函数 | 标签 | 输出 | 条件 |
|------|------|------|------|
| `logger.LogInfo(ctx, msg)` | `[INFO]` | stdout | 始终 |
| `logger.LogWarn(ctx, msg)` | `[WARN]` | stderr | 始终 |
| `logger.LogError(ctx, msg)` | `[ERR]` | stderr | 始终 |
| `logger.LogDebug(ctx, msg, args...)` | `[DEBUG]` | stderr | 仅 `common.DebugEnabled == true` |
| `logger.LogJson(ctx, msg, obj)` | （通过 LogDebug） | stderr | 仅调试模式，用于测试 |

### 日志级别使用指南

| 级别 | 何时使用 | 示例 |
|------|---------|------|
| **FatalLog** | 无法恢复的启动错误 | 数据库连接失败、必要配置缺失 |
| **SysError** | 系统级错误但不致命 | Redis 连接断开、缓存初始化失败 |
| **SysLog** | 系统启动/关闭事件 | 数据库迁移完成、OAuth 提供商加载 |
| **LogError** | 请求级错误 | 上游 API 返回错误、流解析失败 |
| **LogWarn** | 值得注意但非错误 | 额度不足、渠道被自动禁用 |
| **LogInfo** | 正常业务事件 | 计费结算完成、任务状态变更 |
| **LogDebug** | 调试信息 | Redis 操作详情、请求/响应细节 |

---

## 日志格式

**非结构化纯文本格式**（不使用 JSON 格式）：

### 应用日志格式

```
[LEVEL] 2026/03/28 - 15:04:05 | REQUEST_ID | 消息内容
```

实现代码（`logger/logger.go:108`）：
```go
fmt.Fprintf(writer, "[%s] %v | %s | %s \n", level, now.Format("2006/01/02 - 15:04:05"), id, msg)
```

请求 ID 从 `ctx.Value(common.RequestIdKey)` 提取，无上下文时默认为 `"SYSTEM"`。

### 系统日志格式

```
[SYS] 2026/03/28 - 15:04:05 | 消息内容
```

### HTTP 请求日志格式

Gin 的 `LoggerWithFormatter` 自定义格式（`middleware/logger.go`）：

```
[GIN] 2026/03/28 - 15:04:05 | relay | req-id-xxx | 200 | 1.234s | 192.168.1.1 | POST /v1/chat/completions
```

包含字段：路由标签、请求 ID、HTTP 状态码、延迟、客户端 IP、方法和路径。

---

## 请求 ID

每个请求通过 `middleware/request-id.go` 生成唯一 ID：

- 格式：`timestamp + 8位随机字符`
- 存储在 `gin.Context` 中，Key 为 `X-Oneapi-Request-Id`
- 注入到 `context.Context` 通过 `context.WithValue`
- 作为响应头返回客户端
- 所有日志消息自动包含请求 ID

---

## 数据库审计日志

除控制台日志外，关键业务事件持久化到 `logs` 表（通过 `LOG_DB`）：

### 日志类型

| 类型 | 常量 | 说明 |
|------|------|------|
| 充值日志 | `LogTypeTopup = 1` | 充值记录 |
| 消费日志 | `LogTypeConsume = 2` | API 调用消费 |
| 管理日志 | `LogTypeManage = 3` | 管理操作 |
| 系统日志 | `LogTypeSystem = 4` | 系统事件 |
| 错误日志 | `LogTypeError = 5` | API 调用错误 |
| 退款日志 | `LogTypeRefund = 6` | 退款记录 |

### 审计日志字段

消费/错误日志记录：用户 ID、用户名、模型名、Token 数量、额度消耗、渠道信息、请求 ID、分组、使用时间、流式标志。

### 可配置开关

| 配置 | 默认值 | 说明 |
|------|--------|------|
| `LogConsumeEnabled` | `true` | 是否记录消费日志到数据库（管理 UI 可切换） |
| `ErrorLogEnabled` | `false` | 是否记录错误日志到数据库（`ERROR_LOG_ENABLED` 环境变量） |
| `DebugEnabled` | `false` | 是否输出 DEBUG 级别日志（`DEBUG=true` 环境变量） |

---

## 应该记录的内容

| 类别 | 内容 |
|------|------|
| **渠道错误** | 渠道 ID、错误状态码、错误类型、自动封禁决策 |
| **计费结算** | 预消费额度 vs 实际额度、调整差额 |
| **任务生命周期** | 任务状态变更、完成/失败/超时事件 |
| **OAuth 事件** | Token 交换结果、认证失败（带供应商前缀标签如 `[OAuth-GitHub]`） |
| **系统启动** | 数据库迁移、缓存初始化、网络 IP |
| **流式传输** | 流解析错误（所有供应商适配器） |

---

## 禁止记录的内容

| 类别 | 说明 |
|------|------|
| **请求/响应体** | 普通系统日志、应用日志和 `logs` 表不得记录 AI 对话内容；显式启用的独立加密消息审计仅按下述受控场景保存入站内容 |
| **API 密钥/Token** | 不包含在日志消息中 |
| **密码** | 不记录 |
| **OAuth Access Token** | 仅在调试模式截断显示前 10 字符 |
| **会话密钥** | 不记录 |

---

## 敏感数据处理

### IP 地址可选记录

IP 地址仅在用户**明确启用** `RecordIpLog` 设置时才记录到数据库审计日志，否则存储为空字符串。

### 错误消息脱敏

持久化到数据库的错误消息经过 `MaskSensitiveErrorWithStatusCode()` 处理，调用 `common.MaskSensitiveInfo()` 自动脱敏 URL、IP 地址和主机名。

### 用户可见日志清洗

非管理员用户查看日志时（`formatUserLogs()`）：
- `ChannelName` 置空
- `admin_info` 和 `reject_reason` 字段从 `Other` JSON 中剥离

---

## 使用指南

### 系统日志 vs 应用日志选择

```go
// 系统级事件（无请求上下文）
common.SysLog("数据库迁移完成")
common.SysError("Redis 连接失败: " + err.Error())

// 请求级事件（有 gin.Context 或 context.Context）
logger.LogInfo(c.Request.Context(), "计费结算完成: "+model)
logger.LogError(c.Request.Context(), "上游返回错误: "+err.Error())

// 调试信息（仅 DEBUG=true 时输出）
logger.LogDebug(ctx, fmt.Sprintf("Redis GET: key=%s, value=%s", key, value))
```

### Panic 恢复日志

`middleware/recover.go` 捕获 panic 并记录 panic 值和完整堆栈跟踪。

---

## 常见错误

1. **在有请求上下文时使用 SysLog**：应使用 `logger.LogInfo/LogError`，以便包含请求 ID
2. **记录敏感数据**：永远不要在日志中包含 API 密钥、用户密码或 AI 对话内容
3. **过度使用 FatalLog**：`FatalLog` 会调用 `os.Exit(1)` 终止进程，仅用于启动阶段的致命错误
4. **忘记区分日志数据库**：日志模型操作应使用 `LOG_DB`，支持独立日志数据库
5. **调试信息不加条件**：高频调试信息应使用 `LogDebug`（受 `DebugEnabled` 控制），避免生产环境产生大量无用日志

## 场景：受控 AI 入站消息持久化审计

### 1. Scope / Trigger

- Trigger：修改 AI 入站消息采集、`MESSAGE_AUDIT_SECRET`、`MessageAuditEnabled`、`MessageAuditRetentionDays`、消息审计表、推断会话、审计管理 API、异步清理任务或 Default 消息审计页面。
- 本场景是“普通日志不得记录 AI 对话内容”的唯一受控例外。正文只能进入主关系数据库中的独立加密审计表，不能写入控制台/文件日志、`logs.content`、`logs.other`、ClickHouse 日志或管理操作审计。
- 第一阶段只审计经过验证的客户端入站可见内容，不保存当前请求产生的响应正文、流式增量、隐藏思考、请求头、凭证或媒体二进制。

### 2. Signatures

后端入口与查询：

```go
func CaptureMessageAudit(input MessageAuditCaptureInput) bool
func FinalizeMessageAudit(input MessageAuditFinalizeInput)
func ValidateMessageAuditConfiguration() error
func ListMessageAudits(filter model.MessageAuditListFilter) ([]model.MessageAuditRequest, int64, error)
func GetMessageAuditDetail(requestID string) (*MessageAuditDetail, error)
func GetMessageAuditStatus() MessageAuditStatus
func StartMessageAuditCleanupTask(targetTimestamp int64) (*model.SystemTask, bool, error)
```

root-only 管理 API：

```text
GET  /api/message-audit/
GET  /api/message-audit/status
GET  /api/message-audit/:request_id
POST /api/system-task/message-audit-cleanup
```

主数据库固定包含四张表：

```text
message_audit_requests
message_audit_blobs
message_audit_items
message_audit_states
```

### 3. Contracts

- `MESSAGE_AUDIT_SECRET` 必须至少 32 字节；所有节点必须使用完全相同的值。数据库只保存不可逆密钥指纹，不保存原始密钥。
- `MessageAuditEnabled` 默认 `false`；开启时必须先通过密钥校验。`MessageAuditRetentionDays` 默认 `7`，合法范围为 `1-30`。
- controller/relay 只传递验证后的 DTO、请求 ID、用户、令牌、模型、路径、协议、流式标志和时间；协议规范化、过滤、HMAC、加密、去重、会话推断和数据库操作分别归 service/model。
- capture/finalize 均非阻塞入队。队列容量为 1024 条、总字节上限为 64 MiB，单请求快照上限为 1 MiB；队列满、快照超限或持久化失败不得使 relay 或计费失败。
- 正文使用 AES-256-GCM 加密；去重指纹使用独立密钥的 HMAC-SHA256，并包含用户 ID 和 schema version。去重只允许发生在同一用户内；每次请求仍保留独立元数据和有序引用。
- 支持 OpenAI Chat、OpenAI Responses、Claude Messages 和 Gemini GenerateContent 的入站可见内容；Responses Compact、Realtime、Alpha Search、Embedding、Rerank、图片/音频任务及异步任务正文不进入审计。
- 媒体只记录类型、MIME、大小、来源类别和摘要；Authorization、API Key、Cookie、密码、OAuth/Webhook 密钥、Base64/文件二进制和隐藏 reasoning/thinking/signature 必须过滤。
- 推断会话仅在同一用户、同一协议内工作。唯一最长完整前缀标记 `prefix`，完全一致标记 `exact`；前缀失败时只允许使用非公共会话内容 HMAC 的高覆盖严格有序子序列标记 `compressed`；无匹配、低覆盖或多候选歧义必须标记 `new`。
- 默认列表先应用筛选，再按 `audit_session_id` 聚合并返回每个会话的最新请求；`audit_session_id` 查询返回该会话的单次请求且最新在前。历史空会话 ID 和 metadata-only 请求必须单独展示。
- 列表和状态接口不得返回密文、nonce 或正文。详情接口按单个 `request_id` 解密有序消息，每次成功或失败的查看尝试都写不含正文的管理审计日志。
- 状态接口返回 `storage_bytes`、`storage_estimated`、`request_count`、`blob_count` 和 `item_count`。优先统计四张审计表及其索引的实际分配空间，数据库能力不足时回退为密文与 nonce 的逻辑字节并标记估算。
- 一键清空使用异步系统任务和持久化纳秒清理水位；清理截止时间之后的新请求继续保留，截止时间之前尚在队列中的旧 capture 不能在任务结束后重新出现。
- Default 会话历史查询只能在同一 `audit_session_id` 翻页时复用上一页占位数据；切换 session 时必须立即清空旧数据，避免在新会话标识下展示上一会话记录。

### 4. Validation & Error Matrix

| 条件 | 行为 |
|------|------|
| 密钥缺失或不足 32 字节 | 拒绝把 `MessageAuditEnabled` 设置为 `true`，不影响普通 relay |
| 多节点密钥不一致或旧正文指纹不匹配 | 详情返回明确错误并记录查看失败，不尝试错误密钥、不输出密文 |
| 队列条目或字节预算耗尽 | 非阻塞丢弃本次审计并增加 `dropped`，relay 继续 |
| 快照超过 1 MiB | 只保留安全元数据并标记正文不可用，不把超大正文放入队列 |
| 后台写入失败 | 最多重试 3 次，最终增加 `failed` 并记录不含正文的请求关联告警 |
| 完整前缀落入多个会话 | 创建新的 `audit_session_id`，`session_match=new` |
| 压缩子序列候选相同或差距不足 | 不强行归并，创建新会话 |
| 物理表空间查询不可用 | 返回逻辑字节，`storage_estimated=true` |
| 历史记录没有会话 ID | 按单次请求独立显示，不与其他空值记录合并 |
| 前端从会话 A 切换到会话 B | B 加载期间不得把 A 的请求作为 placeholder 展示 |

### 5. Good / Base / Bad Cases

- Good：同一用户的长会话后续请求复用已有加密消息块，只新增变化内容和有序引用；列表聚合为一个推断会话，详情仍能还原每次请求。
- Good：客户端压缩上下文后保留足够多且顺序一致的旧消息 HMAC，系统标记 `compressed`，不解密正文做匹配。
- Base：功能关闭或请求协议不支持正文审计，普通消费日志、计费和转发行为保持不变。
- Base：完全摘要化且没有足够原始锚点时创建新会话，这是保守边界而不是错误。
- Bad：把请求 JSON、提示词或响应正文写入 `logger.LogDebug`、`logs.content` 或 `logs.other`。
- Bad：使用普通 SHA 哈希去重、跨用户共享消息块或把客户端 session/thread 头作为唯一归属依据。
- Bad：为了保证审计必达而同步等待数据库写入，导致 relay 延迟或失败。

### 6. Tests Required

- service 规范化测试必须覆盖四种支持协议、隐藏思考/签名过滤、媒体二进制剥离、超大快照降级和 Responses Compact 排除。
- 加密与去重测试必须断言相同用户复用消息块、不同用户不共享、密文不能直接还原正文、工具定义不改变会话前缀。
- model 测试必须覆盖 SQLite 写入/查询/清理、共享块回收、纳秒水位、历史秒级水位、精确/前缀/压缩/新建归属，以及前缀和压缩多候选歧义拒绝。
- 外部数据库测试通过隔离 schema 的 `MESSAGE_AUDIT_MYSQL_DSN`、`MESSAGE_AUDIT_POSTGRES_DSN` 验证迁移、事务、详情和清理；未提供 DSN 时必须明确记录为未执行，不能声称三库已实测。
- controller/router 测试必须断言非 root 拒绝、列表不泄露正文、详情查看产生管理审计、`audit_session_id` 参数原样传递。
- 前端测试必须断言会话查询参数、角色/内容类型组合过滤保持原顺序，以及跨 session 切换不复用旧 placeholder；变更文件还必须通过 typecheck、lint、format 和 build。
- 回归命令：
  - `go test ./... -count=1`
  - `go test -race ./model ./service -run 'MessageAudit' -count=1`
  - `go vet ./model ./service ./controller ./router`
  - `cd web/default && bun test src/features/message-audits && bun run typecheck && bun run build`
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong

```go
// 错误：在请求主链路同步落库，并把原始正文写进普通日志。
logger.LogInfo(c.Request.Context(), fmt.Sprintf("audit body=%+v", relayInfo.Request))
if err := model.SaveRawMessageAudit(relayInfo.Request); err != nil {
	return err
}
```

#### Correct

```go
// 正确：控制器只向非阻塞审计入口传递验证后的最小上下文。
auditCaptured := service.CaptureMessageAudit(service.MessageAuditCaptureInput{
	RequestID: relayInfo.RequestId,
	UserID:    relayInfo.UserId,
	Protocol:  relayInfo.RelayFormat,
	Request:   request,
})
```

```typescript
// 正确：仅同一推断会话翻页时复用旧页，切换会话立即清空。
placeholderData: (previousData, previousQuery) =>
  keepMessageAuditSessionPlaceholder(
    previousData,
    previousQuery?.queryKey[1],
    sessionId
  )
```

## 场景：API 请求原始 User-Agent 审计

### 1. Scope / Trigger

- Trigger：修改 API 消费日志、错误日志、`Log.Other` 管理员字段、日志脱敏或 Default/Classic 日志详情展示。
- 仅记录应用从 Go `net/http` 请求对象读取到的入站 `User-Agent`，用于管理员排查调用来源；该值可伪造，不能作为身份或安全判定依据。
- 登录日志的 `other.user_agent`、管理操作审计和异步任务后续结算日志不属于本合同。

### 2. Signatures

```go
func appendRequestUserAgent(c *gin.Context, other map[string]interface{}) map[string]interface{}

func RecordErrorLog(
	c *gin.Context,
	userId int,
	channelId int,
	modelName string,
	tokenName string,
	content string,
	tokenId int,
	useTimeSeconds int,
	isStream bool,
	group string,
	other map[string]interface{},
)

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams)
func formatUserLogs(logs []*Log, startIdx int)
```

管理员日志 JSON 与前端类型：

```json
{
  "other": {
    "admin_info": {
      "user_agent": "SourceSDK/7.3 (linux; x86_64)"
    }
  }
}
```

```typescript
interface LogOtherData {
  admin_info?: {
    user_agent?: string
  }
}
```

### 3. Contracts

- `RecordConsumeLog` 和 `RecordErrorLog` 必须在 `common.MapToJsonStr` 序列化 `Other` 之前调用 `appendRequestUserAgent`。
- 唯一可信写入来源是 `c.Request.Header.Get("User-Agent")`；不得使用调用方预置的 `admin_info.user_agent` 代替入站请求头。
- 应用层不得解析、trim、截断、改变大小写或标准化 UA。HTTP 协议解析器在业务代码之前执行的规范化不在本合同控制范围内。
- 非空 UA 写入 `other.admin_info.user_agent`；`Other` 或 `admin_info` 缺失时按需创建，已有管理员字段必须保留。
- UA 为空、上下文为空或请求对象为空时，辅助函数不写该字段，并移除调用方预置的同名值。
- 管理员日志接口保留 `admin_info`；普通用户和公共 Token 日志必须继续通过既有清洗逻辑删除整个 `admin_info`。
- Default 与 Classic 只在管理员上下文且字段为非空字符串时展示，标签使用各自的 `User Agent` i18n 文案；旧日志缺少字段时不显示空行。
- 存储继续复用 `Log.Other` JSON 字符串，不新增数据库列或迁移，保持 SQLite、MySQL、PostgreSQL 和独立日志库兼容。

### 4. Validation & Error Matrix

| 条件 | 写入与展示行为 |
|------|----------------|
| 请求头为非空字符串 | 按应用收到的字符串原值写入 `admin_info.user_agent` |
| 已有 `admin_info` 包含其他字段 | 保留其他字段，仅覆盖 `user_agent` 为请求头值 |
| 请求头为空或缺失 | 不写 `user_agent`，并移除调用方预置值 |
| 辅助函数收到空上下文或空请求对象 | 不 panic，不保留调用方预置 UA |
| `Other` 或 `admin_info` 为空 | 按需创建 map 后写入 |
| 管理员查询消费/错误日志 | API 保留字段，Default 与 Classic 展示 UA |
| 普通用户或公共 Token 查询 | 删除整个 `admin_info`，不得返回 UA |
| 历史日志没有该字段 | 前端不展示 UA，其他详情正常显示 |

### 5. Good / Base / Bad Cases

- Good：客户端发送 `SourceSDK/7.3 (linux; x86_64)`，数据库 `Other` 中保存完全相同的字符串，管理员两套 UI 均可见。
- Good：计费逻辑已写入 `admin_info.quota_saturation`，追加 UA 后该审计字段仍保留。
- Base：请求未携带 UA，日志照常写入，详情中没有 User Agent 行。
- Base：旧日志没有 `admin_info.user_agent`，Default 与 Classic 均正常渲染其余详情。
- Bad：解析 UA 后只保存浏览器或 SDK 名称，导致排查信息丢失。
- Bad：把 UA 写到 `other.user_agent`，从而混淆登录日志合同或绕开管理员字段隔离。
- Bad：在普通用户日志接口单独保留 `admin_info.user_agent`，泄露客户端指纹信息。

### 6. Tests Required

- 使用真实 `RecordConsumeLog` 和 `RecordErrorLog` 写入 SQLite 测试库，断言持久化后的 `admin_info.user_agent` 与请求头完全一致。
- 预置不同的 `admin_info.user_agent` 和其他管理员字段，断言请求头值覆盖预置 UA，其他字段保持不变。
- 覆盖空 UA、空上下文和空请求对象，断言不保留伪造 UA；空 UA 场景还必须断言日志仍成功写入。
- 调用 `formatUserLogs`，断言普通用户响应删除整个 `admin_info`，非管理员字段继续存在。
- 前端至少执行 Default 类型检查、两套 UI 的涉及文件 lint/格式检查、两套构建和 i18n 同步。
- 回归命令：
  - `go test ./model -run 'UserAgent' -count=1`
  - `go test ./... -count=1`
  - `go vet ./model ./controller ./service`
  - `cd web/default && bun run typecheck && bun run build && bun run i18n:sync`
  - `cd web/classic && bun run build && bun run i18n:sync`
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong

```go
// 错误：改变了原值，而且字段不在管理员专属命名空间内。
other["user_agent"] = strings.TrimSpace(parseUserAgent(c.Request.UserAgent()).Name)
otherStr := common.MapToJsonStr(other)
```

#### Correct

```go
// 正确：在序列化前由独立模块合并应用实际收到的请求头原值。
params.Other = appendRequestUserAgent(c, params.Other)
otherStr := common.MapToJsonStr(params.Other)
```

普通用户返回前：

```go
delete(otherMap, "admin_info")
```

## 场景：Anthropic Reasoning Effort 消费日志

### 1. Scope / Trigger

- Trigger：修改 Anthropic/Claude 最终请求体、`output_config.effort`、参数覆盖、请求体透传、`RelayInfo.ReasoningEffort` 或消费日志 `other.reasoning_effort`。
- 目标是让消费日志记录实际发送给 Anthropic 上游的明确 effort 字符串，同时避免记录请求体、对话或凭证。

### 2. Signatures

```go
type RelayInfo struct {
	ReasoningEffort string
}

func syncAnthropicReasoningEffort(info *relaycommon.RelayInfo, outputConfig []byte)
func syncAnthropicReasoningEffortFromRequestBody(info *relaycommon.RelayInfo, requestBody []byte)
```

请求与日志字段：

```json
{
  "output_config": {
    "effort": "xhigh"
  }
}
```

```json
{
  "reasoning_effort": "xhigh"
}
```

### 3. Contracts

- 仅 `constant.ChannelTypeAnthropic` 同步该字段；其他渠道的 `RelayInfo.ReasoningEffort` 不得被 Anthropic 逻辑修改。
- 非透传请求必须在 `RemoveDisabledFields` 和 `ApplyParamOverrideWithRelayInfo` 之后，从最终上游 JSON 的 `output_config.effort` 同步日志字段。
- 请求体透传会跳过最终 JSON 重建和参数覆盖；此时从已解析的 `dto.ClaudeRequest.OutputConfig` 提取 effort，不得为了日志再次读取或复制完整请求体。
- 只接受 JSON string。字段缺失、空值或非字符串时清空 Anthropic 渠道的旧值，使消费日志省略 `reasoning_effort`，避免跨重试残留。
- 不根据 `thinking.budget_tokens` 反推 low、medium 或 high；没有明确 effort 就不记录。
- 日志生成继续由 `GenerateTextOtherInfo` 把非空 `RelayInfo.ReasoningEffort` 写入 `other.reasoning_effort`；前端只消费该既有字段。

### 4. Validation & Error Matrix

| 条件 | 日志行为 |
|------|----------|
| Anthropic `output_config.effort` 是非空字符串 | 写入相同字符串 |
| 参数覆盖把 `max` 改为 `xhigh` | 写入覆盖后的 `xhigh` |
| Anthropic 请求体透传且 DTO 中存在 effort | 写入 DTO 中的字符串 |
| effort 缺失、空值或非字符串 | 清空旧值，不写 `other.reasoning_effort` |
| 仅存在 `thinking.budget_tokens` | 不推断，不写日志字段 |
| 非 Anthropic 渠道出现同名 JSON 字段 | 不修改该渠道的日志上下文 |

### 5. Good / Base / Bad Cases

- Good：最终 Anthropic 请求经参数覆盖变为 `output_config.effort=xhigh`，消费日志显示 `xhigh`。
- Good：渠道开启请求体透传，已解析 Claude DTO 中 effort 为 `high`，消费日志显示 `high`，且没有额外读取完整 body。
- Base：请求没有 `output_config.effort`，日志详情不展示 Reasoning Effort。
- Bad：在参数覆盖前保存 `max`，导致上游实际使用 `xhigh` 而日志仍显示 `max`。
- Bad：从 `thinking.budget_tokens=4096` 猜测 `high`，把预算值误当成明确的上游 effort。
- Bad：为提取 effort 把完整请求体写入数据库日志或普通应用日志。

### 6. Tests Required

- 表驱动测试覆盖 Anthropic 字符串、字段缺失清空旧值、非 Anthropic 隔离。
- 使用真实 `ApplyParamOverrideWithRelayInfo` 覆盖 `max -> xhigh`，断言同步的是修改后的请求体结果。
- 透传路径必须复用同一 output config 提取函数，避免透传与非透传语义漂移。
- 回归命令：
  - `go test ./relay/... -count=1`
  - `go test ./service -count=1`
  - `go test -race ./relay -run '^TestSyncAnthropicReasoningEffort' -count=1`
  - `go vet ./relay ./relay/channel/claude ./service`
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong

```go
// 错误：参数覆盖随后可能改变 effort，日志会记录旧值。
info.ReasoningEffort = request.GetEfforts()
jsonData, _ = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
```

#### Correct

```go
jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
if err != nil {
	return newAPIErrorFromParamOverride(err)
}
syncAnthropicReasoningEffortFromRequestBody(info, jsonData)
```

请求体透传时：

```go
// 透传不执行参数覆盖，已解析 DTO 就是安全且足够的字段来源。
syncAnthropicReasoningEffort(info, request.OutputConfig)
```

## 场景：AI 入站消息持久化审计

### 1. Scope / Trigger

- Trigger：修改消息审计开关、`MESSAGE_AUDIT_SECRET`、Relay capture/finalize 钩子、协议规范化、审计表、root 管理 API、保留清理、一键清空任务或消息审计管理页面。
- 普通应用日志、消费日志和错误日志继续禁止保存 AI 对话正文；本场景仅允许独立的消息审计表在默认关闭、root-only、加密和限期清理条件下保存过滤后的入站可见内容。
- 第一阶段按单次请求审计，不读取或推断 session、thread、conversation，也不保存当前模型响应正文。

### 2. Signatures

Relay 薄层入口：

```go
type MessageAuditCaptureInput struct {
	RequestID string
	UserID    int
	Request   dto.Request
}

type MessageAuditFinalizeInput struct {
	RequestID  string
	Status     string
	ErrorCode  string
	HTTPStatus int
	Duration   time.Duration
}

func CaptureMessageAudit(input MessageAuditCaptureInput) bool
func FinalizeMessageAudit(input MessageAuditFinalizeInput)
```

持久化与清理：

```go
type MessageAuditRequest struct {
	RequestID      string
	CapturedAt     int64
	CapturedAtNano int64
}

type MessageAuditState struct {
	PurgeBefore     int64
	PurgeBeforeNano int64
}

func CreateMessageAuditCapture(record *MessageAuditCaptureRecord) (bool, error)
func AdvanceMessageAuditPurgeBefore(cutoff int64) (int64, error)
func DeleteMessageAuditsBeforeBatch(ctx context.Context, cutoff int64, batchSize int) (int64, error)
func DeleteOrphanMessageAuditBlobsBatch(ctx context.Context, batchSize int) (int64, error)
```

管理 API：

```text
GET  /api/message-audit/
GET  /api/message-audit/status
GET  /api/message-audit/:request_id
POST /api/system-task/message-audit-cleanup
GET  /api/system-task/current?type=message_audit_cleanup
GET  /api/system-task/:task_id
```

环境与配置：

```text
MESSAGE_AUDIT_SECRET          必填于启用阶段，所有节点必须一致，至少 32 字节
MessageAuditEnabled           默认 false
MessageAuditRetentionDays     默认 7，范围 1..30
```

### 3. Contracts

- 只支持 OpenAI Chat Completions、OpenAI Responses（不含 Compact）、Claude Messages 和 Gemini GenerateContent 的已验证入站 DTO。控制器只组装最小上下文并调用 service，不实现协议解析、过滤、加密、去重或数据库访问。
- 可见 system/developer/user/assistant/tool 消息、工具定义、工具调用和工具结果可以进入快照；reasoning、thinking、signature、encrypted content、认证头和渠道密钥必须排除。媒体只保存类型、MIME、大小、来源类别和 HMAC 摘要。
- 工具调用数量只统计实际调用，不统计工具定义或工具结果。OpenAI `tool_calls` 按数组元素计数，Responses/Claude/Gemini 的调用节点各计一次。
- 正文使用 `MESSAGE_AUDIT_SECRET` 派生的 AES-256-GCM 子密钥加密；去重使用独立 HMAC-SHA256 子密钥，并把用户 ID 与 schema version 纳入指纹。禁止跨用户复用消息块，禁止把密钥、nonce、密文或普通明文哈希返回列表 API。
- capture 在请求验证后生成不可变安全快照并非阻塞入队；finalize 在请求结束时非阻塞入队。队列满、字节预算不足、快照超限或持久化失败均 fail-open，不得改变 Relay 响应、重试、并发控制或计费。
- 列表只选择元数据列；详情仅在 root 打开单条记录时读取并解密。详情成功或失败尝试以及清空操作必须写 `LogTypeManage`，管理日志不得包含正文。
- 清理任务 payload 的 `target_timestamp` 使用 Unix 纳秒并在任务创建时固定。`CapturedAtNano <= PurgeBeforeNano` 的旧 capture 必须跳过；秒级 `CapturedAt/PurgeBefore` 只作为历史数据兼容回退。
- 清理消息块必须在事务中选择候选，并在实际删除语句中再次使用 `NOT EXISTS` 校验引用。MySQL/PostgreSQL 候选查询通过 `lockForUpdate` 锁定，SQLite 依赖写事务串行化；不得采用“先查 ID、事务外直接删除”的两阶段实现。
- 前端启动时通过 `/api/system-task/current?type=message_audit_cleanup` 恢复活动任务，轮询任务详情并持续展示进度、删除请求数、删除消息块数或失败信息。业务响应 `success=false` 必须进入错误态，不能伪装成空列表、Disabled 或 Missing。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 审计关闭 | 不创建正文审计记录，Relay 与现有日志行为不变 |
| 密钥缺失或不足 32 字节时尝试启用 | 管理 API 拒绝更新，不回显密钥 |
| 支持协议且队列有容量 | 非阻塞接收 capture，后台加密、去重并持久化 |
| 不支持协议、队列满或字节预算耗尽 | 跳过或丢弃审计，增加无正文指标/告警，业务请求继续 |
| 快照超过单请求上限 | 只保存 metadata-only 状态，不复制剩余正文 |
| 密钥指纹不匹配或密文被篡改 | 详情返回解密错误，并记录不含正文的管理审计 |
| 普通用户、普通管理员访问 | 后端 `RootAuth` 拒绝，前端不显示入口 |
| 清空后收到截止时间之前的排队 capture | 写入事务读取水位并跳过，旧数据不得重现 |
| 截止时间之后的新请求与清空发生在同一秒 | 依靠纳秒水位保留新请求 |
| 消息块仍被较新请求引用 | 孤立块清理不得删除该消息块 |
| 页面恢复时存在 pending/running 清理任务 | 恢复任务并继续轮询，禁用重复清空 |
| API HTTP 成功但 `success=false` | React Query 进入错误态并提供重试 |

### 5. Good / Base / Bad Cases

- Good：同一用户连续请求携带相同历史消息，只新增有序引用；详情仍按每次请求当时的顺序完整还原。
- Good：管理员点击清空后，同一秒稍晚到达的新请求因 `CapturedAtNano` 大于固定水位而保留。
- Good：清理候选消息块在删除前被新请求复用，删除语句再次校验引用并保留该块。
- Base：历史记录只有秒级时间字段，查询和清理按秒级字段兼容处理；新写入始终填充纳秒字段。
- Base：管理员刷新页面时存在活动清理任务，页面从 current 接口恢复进度，不要求重新点击清空。
- Bad：把正文写入 `logs.content`、`logs.other`、普通运行日志或 ClickHouse 日志表。
- Bad：使用 session ID 聚合请求，或要求客户端增加自定义会话请求头。
- Bad：把工具定义数组长度计为工具调用数，导致列表统计失真。
- Bad：使用秒级点击时间作为唯一清理水位，误删同一秒内稍后产生的新请求。
- Bad：先查询孤立块 ID，再无条件删除，导致并发写入产生悬空引用。

### 6. Tests Required

- 密钥测试：缺失、过短、随机 nonce、密文篡改、跨用户 HMAC 隔离和密钥指纹不匹配。
- 协议表驱动测试：四种协议的可见文本、工具定义、实际工具调用/结果、媒体过滤、隐藏思考过滤和 metadata-only 超限状态。
- 异步生命周期测试：禁用跳过、队列满不阻塞、capture/finalize 顺序、有限重试、字节预算释放、优雅关闭排空和 `go test -race`。
- 数据库测试：同用户去重、跨用户隔离、有序重复引用、列表不选择密文、详情解密、秒级历史字段兼容、同秒纳秒边界和孤立块删除前复查。
- 三库测试：SQLite 始终执行；临时 MySQL/PostgreSQL 实例验证迁移、去重、清理水位、共享块保留和孤立块回收。
- API/权限测试：RootAuth、筛选参数、统一响应、列表不泄密、详情查看管理审计和清空任务去重。
- 前端测试：业务响应解包、current 任务恢复、确认文本精确匹配、任务活动状态、进度边界、错误回退和结果展示。
- 回归命令：
  - `go test ./model ./service ./controller ./router -count=1`
  - `go test -race ./service -run 'MessageAudit' -count=1`
  - `go vet ./model ./service ./controller ./router`
  - `go test ./... -count=1`
  - `cd web/default && bun test src/features/message-audits`
  - `cd web/default && bun run typecheck && bun run build && bun run i18n:sync`
  - 对本任务前端文件运行 `oxlint` 和 `oxfmt --check`
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong：使用秒级水位清空

```go
cutoff := time.Now().Unix()
state.PurgeBefore = cutoff
db.Where("captured_at <= ?", cutoff).Delete(&MessageAuditRequest{})
```

问题：点击清空后同一秒稍晚产生的新请求仍满足 `captured_at <= cutoff`，会被误删。

#### Correct：固定纳秒水位并兼容历史数据

```go
cutoff := time.Now().UnixNano()
state.PurgeBeforeNano = cutoff
state.PurgeBefore = time.Unix(0, cutoff).Unix()

query.Where(
	"(captured_at_nano > 0 AND captured_at_nano <= ?) OR ((captured_at_nano IS NULL OR captured_at_nano = 0) AND captured_at <= ?)",
	cutoff,
	time.Unix(0, cutoff).Unix(),
)
```

#### Wrong：按过期候选 ID 无条件删除消息块

```go
var ids []int64
db.Where("NOT EXISTS (?)", itemQuery).Pluck("id", &ids)
db.Where("id IN ?", ids).Delete(&MessageAuditBlob{})
```

#### Correct：事务锁定候选，并在删除时再次校验

```go
err := db.Transaction(func(tx *gorm.DB) error {
	if err := lockForUpdate(tx).Model(&MessageAuditBlob{}).
		Where("NOT EXISTS (?)", orphanCheck).
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	return tx.Where("id IN ?", ids).
		Where("NOT EXISTS (?)", stillOrphan).
		Delete(&MessageAuditBlob{}).Error
})
```
