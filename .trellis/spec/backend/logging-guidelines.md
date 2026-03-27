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
