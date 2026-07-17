# Relay 视觉辅助契约

> 记录视觉辅助在 `service`、`dto`、`relay` 三层之间的可执行契约，避免辅助模型请求被错误转换、遗漏图片内容或把主请求残留字段带到上游。

## 场景：Gemini Native 视觉辅助请求构造

### 1. Scope / Trigger

- Trigger: 修改视觉辅助的图片抽取、缓存、辅助请求构造、端点模式选择、辅助渠道调用或 Gemini/Vertex 适配逻辑。
- 适用范围: 启用视觉辅助后，主请求可以来自 OpenAI Chat、Claude Messages 等入口，但辅助模型需要按配置端点模式转换为独立请求。
- 风险背景: 视觉辅助请求是跨层派生请求，不能直接复用主请求历史消息、角色、参数覆盖或非目标端点字段。Gemini Native 对请求体更严格，错误角色或空 `parts` 会导致上游 `400 INVALID_ARGUMENT` 或本地 `vision assist Gemini request has no content`。

### 2. Signatures

- Service 层构造 OpenAI 兼容的内部辅助请求：

```go
func buildVisionAssistRequest(setting dto.ChannelVisionAssistSettings, prompt string, images []VisionAssistImage) *dto.GeneralOpenAIRequest
```

- DTO 层写入和读取多模态内容必须走 `Message` 的媒体内容契约：

```go
func (m *Message) SetMediaContent(content []MediaContent)
func (m *Message) ParseContent() []MediaContent
```

- Relay 层选择并校验辅助端点模式：

```go
func resolveVisionAssistEndpointMode(configuredMode string, channelType int, modelName string) string
func validateVisionAssistEndpointMode(mode string, channelType int) error
func prepareVisionAssistRequest(c *gin.Context, parent *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, channelModel *model.Channel) (*visionAssistPreparedRequest, *types.NewAPIError)
```

- Gemini Native 辅助请求构造：

```go
func buildVisionAssistGeminiRequest(c *gin.Context, request *dto.GeneralOpenAIRequest) (*dto.GeminiChatRequest, error)
```

### 3. Contracts

- `buildVisionAssistRequest` 只能生成一条 `role = "user"` 的内部辅助消息。
- 内部辅助消息内容必须包含：
  - 第一段文本: 视觉辅助 prompt。
  - 每张图片前的文本标记: `图片 <index>：`。
  - 图片内容: `dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: *dto.MessageImageUrl}`。
- 写入 `dto.Message.Content` 时必须调用 `message.SetMediaContent(content)`；`ParseContent()` 必须能读回 `[]dto.MediaContent`。不要只把 `Content` 赋值成 `[]dto.MediaContent` 后依赖旧的 `[]any` 解析路径。
- `gemini_native` 模式只能用于 Gemini 或 Vertex AI 辅助渠道；其他渠道必须提前返回配置错误，不能静默降级或把 Gemini body 发给 OpenAI/Claude 兼容端点。
- `buildVisionAssistGeminiRequest` 必须输出干净的 Gemini Native body：
  - 只生成一个 `contents` 元素。
  - `contents[0].role` 固定为 `user`。
  - `contents[0].parts` 只包含非空 `text` 和图片 `inlineData`。
  - 图片必须通过 `service.GetBase64Data` 转成 `inlineData.mimeType` + `inlineData.data`。
  - 不能携带 OpenAI Chat、Responses、Claude Messages 的残留字段。
- 构造 Gemini Native parts 时，输入请求中非空且非 `user` 的消息必须跳过；视觉辅助不能把 `assistant` 历史消息传给 Gemini Native。

### 4. Validation & Error Matrix

| 条件 | 行为 |
|------|------|
| `buildVisionAssistGeminiRequest` 收到 `nil` request | 返回 `request is nil` |
| Gemini Native 转换后没有任何有效文本或图片 part | 返回 `vision assist Gemini request has no content` |
| 图片 URL/base64/data URL 无法取出 base64 数据 | 返回 `get file data from '<identifier>' failed: ...` |
| `gemini_native` 配置到非 Gemini/Vertex 辅助渠道 | 返回 `视觉辅助端点模式 gemini_native 需要 Gemini 或 Vertex AI 辅助渠道...`，并禁止重试 |
| 输入消息 `role` 为 `assistant`、`system`、`tool` 等非 `user` 角色 | Gemini Native 转换时跳过该消息 |
| 输入消息 `role` 为空 | 可按兼容内容处理，但输出 Gemini role 仍必须固定为 `user` |

### 5. Good/Base/Bad Cases

- Good: 主请求从 `/v1/messages` 进入并转发到 DeepSeek，DeepSeek 渠道启用 Gemini 视觉辅助；辅助请求先构造成单条 OpenAI 兼容 `user` 多模态消息，再转换成 Gemini Native `contents[0].role=user` 和 `parts[text|inlineData]`。
- Good: 图片源是远程 URL、裸 base64 或 `data:image/...;base64,...` 时，都通过 `types.NewFileSourceFromData` 和 `service.GetBase64Data` 归一化后发送给 Gemini。
- Base: 辅助渠道是 OpenAI Chat 兼容端点时，保持 `GeneralOpenAIRequest`，请求路径为 `/v1/chat/completions`。
- Base: 辅助渠道是 Anthropic Messages 端点时，先通过 `claude.RequestOpenAI2ClaudeMessage` 转换，再走 `/v1/messages`。
- Bad: 把主对话里的 `assistant` 历史消息、主模型参数或非 Gemini 字段直接塞进 Gemini Native body。
- Bad: `message.Content = []dto.MediaContent{...}` 后未保证 `ParseContent()` 可读，导致 Gemini parts 为空。

### 6. Tests Required

- DTO 测试：
  - `TestMessageParseContentSupportsTypedMediaContent`: 断言 `Content` 为 `[]dto.MediaContent` 时，`ParseContent()` 能返回文本和图片内容。
- Service 测试：
  - `TestBuildVisionAssistRequestKeepsTypedMediaContentParsable`: 断言 `buildVisionAssistRequest` 生成单条 `user` 消息，且 `ParseContent()` 能读到 prompt、图片标记和图片内容。
- Relay 测试：
  - `TestValidateVisionAssistEndpointModeRejectsGeminiNativeUnsupportedChannel`: 断言非 Gemini/Vertex 渠道使用 `gemini_native` 会返回配置错误。
  - `TestBuildVisionAssistGeminiRequestUsesCleanUserContent`: 断言 Gemini Native 输出只有 `user` role、有效 `parts`，并且跳过 `assistant` 内容。
- 回归验证：
  - 修改这些路径后至少运行相关包测试；跨层改动完成前应运行 `go test ./...`。

### 7. Wrong vs Correct

#### Wrong

```go
request := &dto.GeneralOpenAIRequest{
    Messages: []dto.Message{{
        Role:    "assistant",
        Content: []dto.MediaContent{imageContent},
    }},
}
```

问题：
- `assistant` role 会被 Gemini Native 上游拒绝。
- 直接赋值 `Content` 容易绕过 `Message` 的解析缓存契约。
- 辅助请求混入主对话角色，可能让上游认为这是多轮历史而不是图片识别任务。

#### Correct

```go
message := dto.Message{Role: "user"}
message.SetMediaContent([]dto.MediaContent{
    {Type: dto.ContentTypeText, Text: prompt},
    {Type: dto.ContentTypeImageURL, ImageUrl: imageURL},
})

request := &dto.GeneralOpenAIRequest{
    Model:    strings.TrimSpace(setting.AssistModel),
    Stream:   &stream,
    Messages: []dto.Message{message},
}
```

Gemini Native 转换时再输出：

```go
return &dto.GeminiChatRequest{
    Contents: []dto.GeminiChatContent{{
        Role:  "user",
        Parts: parts,
    }},
}, nil
```

## 场景：视觉辅助主链路薄层接入

### 1. Scope / Trigger

- Trigger: 修改 `controller.Relay` 的渠道重试准备、Claude/OpenAI Chat handler 的渠道初始化与模型映射、视觉辅助嵌套渠道上下文，或 Relay 错误日志写入。
- 适用范围: build 分支需要在主请求计费前完成映射和视觉辅助改写，同时保持上游热点文件只保留窄生命周期调用。
- 风险背景: 准备状态跨 Controller、Relay handler 和辅助渠道嵌套请求共享。状态未重置会让重试渠道复用旧映射，状态未恢复会让主请求继承辅助渠道；视觉辅助预处理失败若进入普通渠道错误处理，还可能错误封禁主渠道。

### 2. Signatures

- 主请求准备入口与状态边界：

```go
func PrepareRequestForSelectedChannel(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError
func ResetRequestPreparation(c *gin.Context)
func isRequestPreparationComplete(c *gin.Context) bool
func markRequestPreparationComplete(c *gin.Context)
```

- Relay 错误日志入口：

```go
func recordRelayErrorLog(c *gin.Context, err *types.NewAPIError)
func shouldRecordRelayErrorLog(c *gin.Context, err *types.NewAPIError) bool
```

### 3. Contracts

- 文件所有权：
  - `relay/vision_assist.go` 独占 `ContextKeyVisionAssistPrepared` 的运行时读写、渠道元信息初始化、模型映射和视觉辅助 Relay 实现。
  - `controller/relay_error_log.go` 独占 Relay 错误日志门禁、字段组装、上下文扩展合并和数据库写入。
  - `controller/relay.go` 只在每次渠道尝试开始时调用 `ResetRequestPreparation`，并在既有预处理失败和渠道失败位置调用 `recordRelayErrorLog`。
  - `relay/compatible_handler.go` 和 `relay/claude_handler.go` 只通过 `isRequestPreparationComplete` 判断是否需要执行原有 `InitChannelMeta` 与 `ModelMappedHelper`。
- 主请求准备顺序：
  - 每次渠道重试必须先克隆原请求、清理 `ChannelMeta` 和 attempt 字段，再重置准备状态。
  - 选定渠道后，`PrepareRequestForSelectedChannel` 必须先初始化渠道元信息和执行模型映射，再标记准备完成并调用 `service.ApplyVisionAssist`。
  - 准备失败必须发生在主请求计费前；不得对未完成视觉改写的请求预扣主渠道费用。
- handler 兼容：
  - 已准备请求必须跳过重复初始化和重复模型映射，直接深拷贝已经改写的 `RelayInfo.Request`。
  - 未经过 Controller 主循环的独立 handler 调用仍必须执行原有初始化和模型映射。
- 辅助渠道上下文：
  - 切换辅助渠道前必须快照主请求的准备状态；切换后重置状态，让辅助 handler 按辅助渠道重新初始化和映射。
  - 辅助调用结束后必须恢复主请求准备状态及其他既有 context key，不能把辅助渠道 ID、密钥、模型或日志上下文泄漏到主请求。
- 错误日志与封禁隔离：
  - `recordRelayErrorLog` 只在 `types.IsRecordErrorLog(err)` 成立，并且全局错误日志开启或存在非空 `vision_assist_failure_reason` 时写入。
  - 视觉辅助预处理失败直接记录日志后结束当前请求，不得调用 `processChannelError`，因此不得触发主渠道自动封禁。
  - 日志继续使用 `MaskSensitiveErrorWithStatusCode`，并合并 `ContextKeyLogOther`、亲和性 admin 信息和请求耗时；不得记录请求体、图片内容或凭证。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 新的主渠道重试开始 | 准备状态重置为 false，按新渠道重新初始化、映射和视觉改写 |
| `PrepareRequestForSelectedChannel` 完成映射 | 准备状态标记为 true，handler 不重复映射 |
| handler 未经过主 Controller 准备 | 准备状态为 false，执行原有初始化和模型映射 |
| 进入辅助渠道嵌套请求 | 重置准备状态，辅助 handler 使用辅助渠道元信息 |
| 辅助调用返回主请求 | 恢复快照中的主请求准备状态和渠道上下文 |
| 全局错误日志关闭且没有视觉失败原因 | 普通错误不写数据库错误日志 |
| 全局错误日志关闭但存在视觉失败原因 | 写入脱敏错误日志和视觉失败字段 |
| 视觉辅助预处理失败 | 记录日志并返回错误，不自动封禁主渠道 |

### 5. Good/Base/Bad Cases

- Good: 第一次主渠道视觉辅助完成后上游失败并重试；第二次尝试先重置状态，再按新渠道重新映射和执行该渠道的视觉配置。
- Good: 主请求切到 Anthropic 辅助渠道；辅助 Claude handler 看到未准备状态，初始化辅助渠道并映射辅助模型；返回后恢复主请求状态。
- Good: 全局错误日志关闭，但辅助调用失败写入 `vision_assist_failure_reason`；系统保存脱敏错误日志，同时不禁用主渠道。
- Base: 单元测试直接调用 `TextHelper`，上下文没有准备标记；handler 保持原有初始化和映射行为。
- Bad: Controller 直接写 `ContextKeyVisionAssistPrepared`，handler 也各自解释该 key，导致状态语义分散。
- Bad: 重试时不重置准备状态，导致第二个渠道复用第一个渠道的 `ChannelMeta`、模型映射或已改写请求。
- Bad: 视觉辅助预处理失败调用 `processChannelError`，把辅助渠道故障归因并封禁到主渠道。

### 6. Tests Required

- `TestRequestPreparationStateLifecycle`: 断言初始未准备、标记后已准备、重置后再次未准备。
- 视觉辅助 Relay 测试必须覆盖模型映射后的最终模型、辅助渠道元信息初始化、端点模式和上下文恢复。
- Controller 错误日志测试必须覆盖：
  - 全局错误日志关闭时，视觉失败字段仍写入数据库。
  - 普通错误尊重全局开关，不产生错误日志。
  - 日志保留请求路径、请求 ID、视觉失败字段和脱敏错误。
- 回归命令：
  - `go test ./controller ./relay ./service -count=1`
  - `go test -race ./controller ./relay ./service -run 'VisionAssist|RelayErrorLog|RequestPreparationState' -count=1`
  - `go vet ./controller ./relay ./service`
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong

```go
common.SetContextKey(c, constant.ContextKeyVisionAssistPrepared, false)

prepared := common.GetContextKeyBool(c, constant.ContextKeyVisionAssistPrepared)
if !prepared {
	info.InitChannelMeta(c)
}
```

问题：Controller 和 handler 直接共享底层 context key，未来调整状态语义时容易漏改调用方，也扩大 build 分支与上游热点文件的冲突面。

#### Correct

```go
// Controller 每次渠道尝试只保留生命周期调用。
relay.ResetRequestPreparation(c)

// Relay handler 只读取领域入口，不解释底层 context key。
prepared := isRequestPreparationComplete(c)
if !prepared {
	info.InitChannelMeta(c)
}
```

要求：
- 状态 helper 与 `PrepareRequestForSelectedChannel` 同属 `relay/vision_assist.go`。
- `controller/relay.go` 不承载视觉状态实现；Claude/OpenAI handler 不直接引用 `ContextKeyVisionAssistPrepared`。
- 错误日志函数体放在 `controller/relay_error_log.go`，原调用位置和行为保持不变。

## 场景：Responses 工具输出视觉辅助

### 1. Scope / Trigger

- Trigger: 修改 `/v1/responses` 视觉辅助的图片抽取、工具输出解析、识图文本写回或 `strip_image` 行为。
- 适用范围: Responses 普通消息 `content`，以及 `function_call_output` / `custom_tool_call_output` 的 `output` 数组。
- 风险背景: Codex `view_image` 不一定追加普通用户图片消息；直接工具调用会把图片放进 `function_call_output.output`，统一工具执行会放进 `custom_tool_call_output.output`。只扫描 `content` 会导致视觉辅助完全不触发。

### 2. Signatures

- Responses 图片抽取与请求改写：

```go
func extractOpenAIResponsesVisionAssistImages(request *dto.OpenAIResponsesRequest) []VisionAssistImage
func rewriteOpenAIResponsesVisionAssistRequest(request *dto.OpenAIResponsesRequest, results []VisionAssistResult, stripImage bool) error
```

- Responses 输入 DTO 保持原始 JSON 契约：

```go
type OpenAIResponsesRequest struct {
    Input json.RawMessage `json:"input,omitempty"`
}
```

- 需要识别的工具输出结构：

```json
{
  "type": "function_call_output | custom_tool_call_output",
  "call_id": "call_xxx",
  "output": [
    {"type": "input_text", "text": "可选的原工具文本"},
    {"type": "input_image", "image_url": "data:image/png;base64,...", "detail": "high"}
  ]
}
```

### 3. Contracts

- 普通 Responses 消息继续扫描 `content` 数组；`function_call_output` 和 `custom_tool_call_output` 必须扫描 `output` 数组。
- 触发依据是工具输出数组中存在有效的 `type=input_image`，不能依赖工具名、`call_id` 或相邻工具调用项。
- `image_url` 必须兼容字符串和对象 `{url, detail, mime_type}`；解析继续复用 Responses 现有图片字段规则。
- `VisionAssistImage.MessageIndex` 表示改写前的顶层输入项索引；插入新消息时不得改变后续结果与原输入项的对应关系。
- `function_call_output` 的识图文本必须在原 `output` 首张图片前写成 `input_text`。
- `custom_tool_call_output` 的图片按 `strip_image` 删除或保留，识图文本必须写进紧邻其后的普通 `type=message, role=user` 消息。部分目标转换器会过滤自定义工具项，只写回原 `output` 会让识图文本再次丢失。
- 所有非图片输出项及其相对顺序必须保留；同一顶层工具输出的多张图片只生成一条汇总识图文本。
- `strip_image=true` 删除已识别的 `input_image`；`strip_image=false` 保留图片并仍注入识图文本。
- 工具输出不含有效图片时不得调用辅助模型，也不得重新序列化 `request.Input`。
- JSON 解析和序列化只能使用 `common.Unmarshal` / `common.Marshal`。

### 4. Validation & Error Matrix

| 条件 | 行为 |
|------|------|
| `request` 为 `nil`、`input` 为空或不是数组 | 返回空图片集合；改写直接返回 `nil` |
| 顶层输入项不是对象 | 跳过该项并保留原值 |
| 普通消息 `content` 或工具输出 `output` 不是数组 | 跳过该项，不触发视觉辅助 |
| `input_image` 缺少有效 `image_url` | 跳过该图片 |
| 工具输出只有文本或其他非图片内容 | caller 调用次数为 `0`，`Input` 字节保持不变 |
| 改写时 JSON 解析或序列化失败 | 返回错误，由现有 `failure_policy` 决定中止或跳过 |
| `strip_image=false` | 保留原图片，同时注入识图文本 |

### 5. Good/Base/Bad Cases

- Good: Codex 直接调用 `view_image`，请求包含 `function_call_output.output=[input_image]`；系统调用辅助模型，并把描述作为同一 `output` 的 `input_text` 转发。
- Good: Codex 通过统一 `exec` 查看图片，`custom_tool_call_output.output` 同时包含执行文本和图片；系统保留执行文本、按配置处理图片，并在下一条普通用户消息写入描述。
- Good: 同一工具输出两次返回相同图片；请求级去重只调用一次辅助模型，但描述保留图片 1、图片 2 的编号结果。
- Base: 历史 Codex 客户端仍追加普通 `role=user` 的 `content[].input_image`；继续走既有消息改写分支。
- Base: 工具输出只有 `input_text`；请求不触发视觉辅助且不被重写。
- Bad: 只读取 `inputItem["content"]`，遗漏工具 `output` 中的图片。
- Bad: 把 `custom_tool_call_output` 的识图文本只写回原 `output`，导致目标转换器过滤自定义工具项时同时丢掉描述。

### 6. Tests Required

- `TestApplyVisionAssistRewritesOpenAIResponsesToolOutputs`:
  - `function_call_output` 字符串 `image_url` 能触发 caller，识图文本写回原 `output`。
  - `custom_tool_call_output` 对象 `image_url` 的 detail/MIME 能传入 caller；原 `input_text` 保留，描述进入紧邻用户消息。
- `TestRewriteOpenAIResponsesVisionAssistRequestKeepsCustomToolImage`: `strip_image=false` 时保留自定义工具图片和原文本，并插入描述消息。
- `TestApplyVisionAssistSkipsOpenAIResponsesToolOutputWithoutImage`: 纯文本工具输出不调用 caller，原始 `Input` 不变。
- `TestApplyVisionAssistReusesDuplicateOpenAIResponsesToolImages`: 重复图片只调用一次 caller，两个编号结果都写入描述。
- 回归命令：
  - `go test ./dto ./service ./relay`
  - `go test ./...`

### 7. Wrong vs Correct

#### Wrong

```go
contentItems, ok := inputItem["content"].([]any)
if !ok {
    continue
}
```

问题：工具结果的图片位于 `output`，该实现会返回空图片集合，辅助识图接口永远不会触发。

#### Correct

```go
contentKey := "content"
switch common.Interface2String(inputItem["type"]) {
case "function_call_output", "custom_tool_call_output":
    contentKey = "output"
}
contentItems, ok := inputItem[contentKey].([]any)
```

对于自定义工具输出，图片处理完成后还必须追加普通用户描述消息：

```go
rewrittenItems = append(rewrittenItems, inputItem)
rewrittenItems = append(rewrittenItems, map[string]any{
    "type": "message",
    "role": "user",
    "content": []any{
        map[string]any{"type": "input_text", "text": text},
    },
})
```
