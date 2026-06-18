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
