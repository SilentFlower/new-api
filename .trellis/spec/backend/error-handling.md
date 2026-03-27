# 错误处理

> 本项目的错误类型定义、传播方式和 API 错误响应格式。

---

## 概述

项目采用**分层错误处理**策略：

- **Model 层**：返回标准 Go `error`
- **Service 层**：将 Go `error` 转换为 `*types.NewAPIError`（统一错误类型）
- **Controller 层**：根据场景选择响应格式并返回客户端
- **Middleware 层**：在请求链中提前中止并返回错误

两种完全不同的错误响应格式：
1. **管理 API**（`/api/*`）：`{success: false, message: "..."}`，HTTP 状态码始终 200
2. **转发 API**（`/v1/*`）：OpenAI/Claude/Gemini 兼容格式，HTTP 状态码反映实际错误

---

## 错误类型

### 核心类型：`types.NewAPIError`

定义在 `types/error.go`，是项目的统一内部错误类型：

```go
type NewAPIError struct {
    Err            error           // 底层 Go error
    RelayError     any             // 供应商错误（OpenAIError 或 ClaudeError）
    skipRetry      bool            // 是否跳过重试
    recordErrorLog *bool           // 是否记录错误日志
    errorType      ErrorType       // 错误来源分类
    errorCode      ErrorCode       // 机器可读错误码
    StatusCode     int             // HTTP 状态码
    Metadata       json.RawMessage // 元数据（如 OpenRouter 的附加信息）
}
```

实现了 `error` 接口和 `Unwrap()` 方法，支持 `errors.Is`/`errors.As`。

### ErrorType（错误来源）

| 常量 | 说明 |
|------|------|
| `ErrorTypeNewAPIError` | 本网关自身生成的错误 |
| `ErrorTypeOpenAIError` | OpenAI 格式供应商的错误 |
| `ErrorTypeClaudeError` | Claude/Anthropic 的错误 |
| `ErrorTypeMidjourneyError` | Midjourney 错误 |
| `ErrorTypeGeminiError` | Gemini 错误 |
| `ErrorTypeUpstreamError` | 通用上游错误 |

### ErrorCode（错误码）

按类别前缀组织（定义在 `types/error.go` 第 40-88 行）：

| 前缀 | 示例 | 说明 |
|------|------|------|
| `channel:` | `channel:no_available_key`, `channel:param_override_invalid` | 渠道相关错误 |
| （无前缀） | `read_request_body_failed`, `convert_request_failed` | 客户端请求错误 |
| （无前缀） | `bad_response_status_code`, `empty_response` | 上游响应错误 |
| （无前缀） | `insufficient_user_quota`, `pre_consume_token_quota_failed` | 额度相关错误 |
| （无前缀） | `query_data_error`, `update_data_error` | 数据库操作错误 |

### 其他错误类型

| 类型 | 文件 | 用途 |
|------|------|------|
| `dto.OpenAIErrorWithStatusCode` | `dto/error.go` | 解析上游 OpenAI 格式错误 |
| `dto.GeneralErrorResponse` | `dto/error.go` | 解析各种上游错误格式的通用结构 |
| `dto.TaskError` | `dto/task.go` | 异步任务错误 |
| `dto.ClaudeErrorWithStatusCode` | `dto/claude.go` | Claude 专属错误 |
| `types.ChannelError` | `types/channel_error.go` | 记录导致失败的渠道信息 |
| `oauth.OAuthError` | `oauth/types.go` | OAuth 流程错误 |

---

## 错误构造（函数式选项模式）

### 构造函数

```go
// 通用错误（检测是否已包装 NewAPIError，避免双重包装）
types.NewError(err, types.ErrorCodeConvertRequestFailed, options...)

// OpenAI 格式错误（带 HTTP 状态码）
types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, 500, options...)

// 带状态码的通用错误
types.NewErrorWithStatusCode(err, types.ErrorCodeInsufficientUserQuota, 403, options...)

// 从解析的上游 OpenAI 错误构造
types.WithOpenAIError(openAIError, statusCode, options...)

// 从解析的上游 Claude 错误构造
types.WithClaudeError(claudeError, statusCode, options...)
```

### 选项函数

```go
// 标记为不可重试（如客户端请求格式错误）
types.ErrOptionWithSkipRetry()

// 不记录到错误日志表
types.ErrOptionWithNoRecordErrorLog()

// 隐藏原始错误消息（调试日志保留原始信息）
types.ErrOptionWithHideErrMsg("替换显示的消息")
```

### 使用示例

```go
// service/billing_session.go
userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
if err != nil {
    return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
}
if userQuota <= 0 {
    return nil, types.NewErrorWithStatusCode(
        fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)),
        types.ErrorCodeInsufficientUserQuota,
        http.StatusForbidden,
        types.ErrOptionWithSkipRetry(),
    )
}
```

---

## 错误传播流程

```
[Model 层]   返回标准 error
     ↓
[Service 层] 转换为 *types.NewAPIError，附加 ErrorCode 和选项
     ↓
[Controller/Relay 层] 执行重试逻辑、渠道错误处理、计费回退
     ↓
[响应层]     根据 RelayFormat 转换为客户端格式并发送 JSON
```

### 重试逻辑

`controller/relay.go` 中的重试机制：
- 最多重试 `common.RetryTimes` 次
- 每次失败后选择新渠道
- 重试决策（`shouldRetry()`）：
  - `channel:` 前缀错误 → 始终重试
  - `skipRetry` 标记 → 永不重试
  - 2xx 状态码 → 永不重试
  - 其他 → 根据 `operation_setting.ShouldRetryByStatusCode()` 判断

### 渠道自动禁用

失败后 `processChannelError()` 评估是否自动禁用渠道：
- 检查错误类型和状态码
- 如果启用了自动封禁且满足条件，异步禁用渠道
- 错误日志记录到数据库（包含渠道信息、错误类型、错误码等）

---

## API 错误响应格式

### 管理 API（Dashboard）

所有管理 API 错误返回 **HTTP 200** + JSON body：

```json
{"success": false, "message": "错误描述"}
```

使用 `common/gin.go` 中的辅助函数：

```go
// 基础错误
common.ApiError(c, err)          // message = err.Error()
common.ApiErrorMsg(c, "消息")    // message = 字符串

// i18n 错误（根据用户语言设置翻译）
common.ApiErrorI18n(c, i18n.MsgUserPasswordLoginDisabled)

// 成功响应
common.ApiSuccess(c, data)       // {success: true, message: "", data: {...}}
common.ApiSuccessI18n(c, i18n.MsgSuccess, data)
```

### 转发 API（Relay）

根据 `RelayFormat` 返回不同格式：

**OpenAI 格式**（默认）：
```json
{
    "error": {
        "message": "错误信息 (request id: xxx)",
        "type": "new_api_error",
        "param": "",
        "code": "error_code"
    }
}
```

**Claude 格式**：
```json
{
    "type": "error",
    "error": {
        "type": "error_type",
        "message": "错误信息"
    }
}
```

**Midjourney 格式**：
```json
{
    "description": "错误信息",
    "type": "upstream_error",
    "code": 4
}
```

### 中间件错误

中间件通过 `c.JSON()` + `c.Abort()` 中止请求链：

```go
// middleware/utils.go
func abortWithOpenAiMessage(c *gin.Context, statusCode int, message string, code ...types.ErrorCode) {
    c.JSON(statusCode, gin.H{
        "error": gin.H{
            "message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
            "type":    "new_api_error",
            "code":    codeStr,
        },
    })
    c.Abort()
}
```

---

## 敏感信息脱敏

### 错误消息脱敏

`common.MaskSensitiveInfo()` 在返回客户端前自动脱敏：

- **URL**：域名、路径、查询参数替换为 `***`
- **IP 地址**：替换为 `***.***.***.***`
- **主机名**：仅保留 TLD

在 `ToOpenAIError()` 和 `ToClaudeError()` 转换方法中自动调用。

### 请求 ID 注入

所有错误消息附带请求 ID：

```go
// common/utils.go
func MessageWithRequestId(message string, id string) string {
    return fmt.Sprintf("%s (request id: %s)", message, id)
}
```

---

## 上游错误解析

`service.RelayErrorHandler()` 是解析上游错误的核心函数：

1. 读取完整响应体
2. 尝试解析为 `dto.GeneralErrorResponse`（通用错误结构，兼容多种格式）
3. 从多个可能的字段提取错误信息：`error.message`, `msg`, `err`, `error_msg`, `detail`, `header.message`
4. 包装为 `*types.NewAPIError`

---

## i18n 错误消息

### 适用范围

| 场景 | 是否 i18n | 说明 |
|------|-----------|------|
| 管理 API 错误 | 是 | 使用 `common.ApiErrorI18n()` |
| 分发中间件错误 | 是 | 使用 `i18n.T()` |
| 转发 API 错误 | 否 | 使用英文错误码或上游原始消息 |
| 内部日志消息 | 否 | 中文硬编码 |

### 使用方式

```go
// 简单 i18n 错误
common.ApiErrorI18n(c, i18n.MsgUserPasswordLoginDisabled)

// 带模板变量的 i18n 错误
i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()})
```

语言检测优先级：用户设置 > 数据库用户语言 > `Accept-Language` 头 > 英文（默认）

---

## 常见错误

1. **双重包装 NewAPIError**：`NewError()` 已内置 `errors.As` 检测，避免重复包装，但需注意不要手动 `fmt.Errorf("wrap: %w", apiErr)`
2. **转发 API 用错响应格式**：Dashboard API 用 `ApiError`，Relay API 用 OpenAI/Claude 格式
3. **忘记脱敏**：错误消息返回客户端前必须经过 `MaskSensitiveInfo()`
4. **忘记标记 skipRetry**：客户端请求格式错误等不应重试的错误必须标记 `ErrOptionWithSkipRetry()`
5. **中间件忘记 c.Abort()**：`c.JSON()` 后必须调用 `c.Abort()` 阻止后续处理
