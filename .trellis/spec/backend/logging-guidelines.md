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
| **请求/响应体** | AI 对话内容（提示词和回复）绝不记录，仅记录 Token 数量和模型名 |
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
