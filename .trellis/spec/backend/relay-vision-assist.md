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
