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
func buildVisionAssistRequest(setting dto.ChannelVisionAssistSettings, prompt string, userMessage string, images []VisionAssistImage) *dto.GeneralOpenAIRequest
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
  - 用户原始问题非空时: 独立的用户问题说明与文本块。
  - 当前识图单元包含多张图片时: 独立的多图联合分析说明。
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

## 场景：用户意图驱动与渠道级有界多图识别

### 1. Scope / Trigger

- Trigger: 修改视觉辅助用户文本解析、默认提示词、识图单元划分、`multi_image_mode`、`combined_max_images`、完整请求体大小限制、缓存键、识图结果写回、执行日志或渠道表单。
- 适用范围: OpenAI Chat、Claude Messages、OpenAI Responses 主请求，以及 OpenAI Chat、OpenAI Responses、Anthropic Messages、Gemini Native 辅助端点。
- 风险背景: 用户问题缺失会让辅助模型只生成通用图片描述；无界合并可能超过上游请求体限制；分批边界不稳定或缓存键混入请求级状态会造成错误复用或在新一轮对话中重复识别。

### 2. Signatures

- 渠道配置、规划结果与识图结果：

```go
type ChannelVisionAssistSettings struct {
	MultiImageMode    string `json:"multi_image_mode,omitempty"`
	CombinedMaxImages int    `json:"combined_max_images,omitempty"`
}

type visionAssistUnitPlan struct {
	Units              []visionAssistUnit
	SplitByImageCount  bool
	SplitByPayloadSize bool
}

type VisionAssistResult struct {
	Image      VisionAssistImage
	ImageCount int
	Combined   bool
	Text       string
	CacheHit   bool
	Reused     bool
}
```

- Service 层用户意图、识图单元、请求大小和缓存入口：

```go
func extractVisionAssistUserMessage(request dto.Request) string
func extractVisionAssistUserTexts(request dto.Request) []visionAssistUserText
func resolveVisionAssistUserMessages(request dto.Request, images []VisionAssistImage) map[int]string
func normalizedVisionAssistMultiImageMode(setting dto.ChannelVisionAssistSettings) string
func normalizedVisionAssistCombinedMaxImages(setting dto.ChannelVisionAssistSettings) int
func buildVisionAssistUnitPlan(setting dto.ChannelVisionAssistSettings, prompt string, userMessages map[int]string, images []VisionAssistImage, multiImageMode string, combinedMaxImages int) visionAssistUnitPlan
func estimateVisionAssistImageURLPayloadBytes(image VisionAssistImage) (int, bool)
func estimateVisionAssistRequestEnvelopeBytes(setting dto.ChannelVisionAssistSettings, prompt string, userMessage string, images []VisionAssistImage) (int, bool)
func buildVisionAssistRequest(setting dto.ChannelVisionAssistSettings, prompt string, userMessage string, images []VisionAssistImage) *dto.GeneralOpenAIRequest
func buildVisionAssistCacheKey(setting dto.ChannelVisionAssistSettings, prompt string, userMessage string, multiImageMode string, images []VisionAssistImage) string
```

- 前端表单字段：

```ts
type VisionAssistMultiImageMode = 'separate' | 'combined'

vision_assist_multi_image_mode: VisionAssistMultiImageMode
vision_assist_combined_max_images: number
```

### 3. Contracts

- 用户意图解析：
  - `extractVisionAssistUserMessage` 保留“返回最新非空用户文本”的兼容语义；`ApplyVisionAssist` 必须通过 `resolveVisionAssistUserMessages` 为每个最终识图批次解析自己的用户问题。
  - 图片只绑定同一 `MessageIndex` 的非空用户文本，不能继承更早或更晚消息的问题；新一轮只发图片时应使用通用识图规则。
  - 最新用户消息只有文本、没有新图片时，不得把该文本覆盖到历史图片或改变历史图片缓存键。目标模型必须基于首次注入的识图结果回答追问，辅助模型不重复调用、审计或计费。
  - 最新用户消息携带新图片时，最新问题只能绑定该消息的图片；历史图片继续使用各自原始问题和缓存键，不能被最新问题触发重复识别。
  - OpenAI Chat 使用 `Message.ParseContent()`，Claude 兼容字符串内容和 `type=text` 内容块，Responses 兼容字符串 `input` 和普通 `role=user` 消息。
  - `function_call_output`、`custom_tool_call_output`、system、assistant、tool 内容不得成为用户问题。
  - 完全没有用户文本时返回空字符串，辅助请求退化为“识图规则 + 图片”。
- 辅助请求继续只有一条 `role=user` 多模态消息，内容顺序固定为：识图规则、可选用户问题、可选多图说明、按原顺序排列的图片编号和图片。
- 用户问题只决定识图重点，不能覆盖渠道识图规则；默认提示词必须把图片文字视为待分析内容而不是可执行指令，并保留人物身份等结论的不确定性。
- 多图模式与分批：
  - 后端只接受精确值 `combined`；空值、大小写变体、前后空格和未知值一律归一化为 `separate`。
  - `combined_max_images` 的合法范围是整数 `1-64`；缺失、`0`、负数或超过 `64` 时后端统一使用默认值 `5`。
  - `separate` 每张图片生成一个识图单元，不受合并数量和请求体上限改变分组结果。
  - `combined` 先按 `VisionAssistImage.MessageIndex` 隔离消息，再按原始图片顺序贪心分批；每批最多 `combined_max_images` 张图片，不能跨消息合并。
  - 每批完整序列化辅助请求必须不超过固定 `8 MiB` 安全上限。大小计算必须覆盖模型、规则、用户问题、多图说明、图片编号、detail、MIME、JSON 结构和转义后的图片 URL，不能只累加原始图片字段长度。
  - 大型 Base64 不得在每次候选分批时重复复制；允许序列化空 URL 请求骨架后叠加每个 URL 的 JSON 编码长度，但估算结果必须与 `common.Marshal(buildVisionAssistRequest(...))` 的实际长度一致。
  - 大小估算失败时必须保守切割；单张图片自身超过上限时仍独立成批，由既有辅助调用与失败策略处理，不在规划层拒绝。
  - 分批不能改变图片的全局 `Index`、`MessageIndex` 或写回位置。合并调用失败仍遵循既有重试和失败策略，不隐式降级为逐张识别。
  - 每个成功批次只写回一次结果；图片数、缓存命中数和失败数仍按实际图片数量统计。
- 前端配置：
  - 新建渠道默认 `multi_image_mode=combined`、`combined_max_images=5`；编辑历史渠道时，缺失或非法 `multi_image_mode` 显示为 `separate`。
  - 历史 `combined_max_images` 缺失、非整数、非有限数或超出 `1-64` 时回退 `5`；表单 schema 必须使用整数约束，数字输入使用 `min=1`、`max=64`、`step=1`，并且只在 `combined` 模式显示。
- 缓存：
  - 缓存键按最终识图批次生成，并包含辅助渠道、辅助模型、识图规则哈希、该批次用户文本哈希、规范化多图模式，以及批次内按顺序排列的图片源哈希、detail 和 MIME。
  - 缓存键不得包含请求 ID、会话 ID、消息索引、批次序号、并发数或 worker 完成顺序；这些请求级状态会阻止新一轮对话复用等价批次。
  - 后续纯文本追问不属于图片批次输入，不得进入历史图片缓存键；只有图片所属消息实际发送给辅助模型的派生用户文本、图片内容/顺序或其他既有识图配置变化时才形成新的识图缓存条目。
  - `combined_max_images` 不直接进入缓存键；配置变化导致批次图片组合变化时自然隔离，最终批次组合不变时必须继续复用原缓存。
  - 缓存值只保存识图文本；日志不得新增用户原文、请求正文或图片内容。
- 普通执行日志必须记录 `vision_assist_combined_max_images`、`vision_assist_batch_count`、`vision_assist_batch_image_counts`、`vision_assist_split_applied` 和 `vision_assist_split_reason`。切割原因只使用 `image_count`、`payload_size` 或 `image_count_and_payload_size`。
- 写回主请求时必须保留原始用户文本和非图片内容顺序；`strip_image=false` 时还必须保留原图片内容和顺序，不能因为分批重复或删除图片。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 用户发送图片并提问 | 每个相关辅助批次同时包含识图规则、用户问题和图片，且只有一条 user 消息 |
| 新一轮用户消息只有图片 | 不继承旧问题，使用通用识图规则处理新图片 |
| 最新用户消息只有文本，图片均来自历史消息 | 历史图片继续使用首次识图缓存，辅助 caller 调用次数不增加；最新文本原样保留给目标模型 |
| 最新用户消息同时包含文本和新图片 | 新问题只绑定新图片；历史图片按原问题命中缓存，不重复调用辅助模型 |
| Responses 工具输出含文本和图片 | 图片可以触发视觉辅助，工具输出文本不得成为用户问题 |
| `multi_image_mode=combined` 且同消息有 39 张图片、上限为 `5` | 稳定分成 `5+5+5+5+5+5+5+4`，不同消息仍各自分批 |
| 候选批次完整请求体超过 `8 MiB` | 在加入下一张图片前切割，并记录 `payload_size` |
| 单张图片自身超过 `8 MiB` | 单独成批，不在规划层返回配置或校验错误 |
| `multi_image_mode=separate` | 始终一图一批，不读取 `combined_max_images` 改变分组 |
| `combined_max_images` 缺失、`0`、负数或大于 `64` | 后端和历史表单回退默认值 `5` |
| 前端输入 `2.5` | schema 拒绝；读取历史值时回退默认值 `5` |
| 上限变化导致批次从 `[A,B],[C]` 变为 `[A],[B],[C]` | `[A]`、`[B]` 不误命中旧组合；未变化的 `[C]` 可以命中 |
| 新请求 ID、会话或消息位置不同，但最终有序批次相同 | 命中已有 HybridCache，不再次调用辅助模型 |
| 合并识图失败且策略为 `skip` | 不写入空结果，失败图片数按当前批次实际图片数累计 |

### 5. Good/Base/Bad Cases

- Good: 用户在同一消息上传 39 张图片，配置 `combined_max_images=5`；辅助请求按 `5+5+5+5+5+5+5+4` 发送，结果仍按全局图片编号写回。
- Good: `[A,B],[C]` 已缓存后把上限从 `2` 改为 `1`；新组合 `[A]`、`[B]` 重新识别，未变化的 `[C]` 复用缓存，后续等价请求三批全部命中。
- Good: 首次提交两张图片并完成联合识图，后续连续询问“第二张图最后一句是什么”和“第一张提示是什么”；两次请求都复用首次批次缓存，不产生新的视觉辅助调用或独立审计记录。
- Good: 两张图片字段本身合计小于 `8 MiB`，但加上长 prompt、用户问题和 JSON 包装后完整请求超限；规划器提前拆成两个批次。
- Base: 单图请求在 `combined` 模式下仍只有一个识图单元，写回使用普通图片编号语义。
- Base: 历史渠道没有 `combined_max_images`；前后端都按默认值 `5` 执行。
- Bad: 只累加未编码的图片 URL 字节，忽略 JSON 转义、模型、prompt 和消息包装，导致实际请求体超过安全上限。
- Bad: 把 `combined_max_images`、请求 ID 或批次序号直接加入缓存键，导致最终图片组合未变化时仍重复请求。
- Bad: 把请求中的全部图片合成一个单元，跨原始消息合并不相关图片并破坏写回定位。

### 6. Tests Required

- 用户意图测试：`TestExtractVisionAssistUserMessage`、`TestResolveVisionAssistUserMessagesKeepsHistoricalIntent`、`TestResolveVisionAssistUserMessagesDoesNotReuseOlderQuestionForNewImage`、`TestResolveVisionAssistUserMessagesKeepsOriginalQuestionOnTextOnlyFollowUp`、`TestBuildVisionAssistRequestIncludesUserMessage`。
- 基础多图测试：`TestApplyVisionAssistCombinesImagesFromSameMessage`、`TestBuildVisionAssistUnitsDoesNotCombineAcrossMessages`、`TestApplyVisionAssistCombinedFailureCountsAllImages`、`TestNormalizedVisionAssistMultiImageModeRejectsInvalidValues`。
- 分批边界测试：
  - `TestBuildVisionAssistUnitPlanSplitsCombinedImagesByConfiguredLimit` 断言 `39 -> 5+5+5+5+5+5+5+4`。
  - `TestBuildVisionAssistUnitPlanSplitsCombinedImagesByFullRequestPayloadSize` 断言完整请求体而不是原始图片字段触发切割。
  - `TestBuildVisionAssistUnitPlanKeepsOversizedSingleImageInOwnBatch` 断言单图超限仍独立成批。
  - `TestEstimateVisionAssistRequestEnvelopeMatchesSerializedPayload` 断言估算值精确等于实际序列化长度。
  - `TestNormalizedVisionAssistCombinedMaxImages` 断言默认值和 `1-64` 边界。
- 缓存和原图测试：`TestApplyVisionAssistReusesInitialImageCacheForTextOnlyFollowUps`、`TestApplyVisionAssistReusesCombinedBatchCacheAcrossRequests`、`TestApplyVisionAssistCombinedCacheSeparatesChangedBatchCompositions`、`TestApplyVisionAssistCombinedBatchingKeepsOriginalImagesWhenConfigured`。
- 渠道表单 round-trip 测试必须覆盖新建默认 `5`、历史缺失/越界/小数回退、整数 schema 拒绝小数以及保存后的 `combined_max_images`。
- 多图模式组件测试必须断言 Combined 输入只在对应模式显示，并带有 `min=1`、`max=64`、`step=1`。
- 回归命令：
  - `go test ./service -run 'VisionAssist' -count=1`
  - `go test -race ./service -run 'VisionAssist' -count=1`
  - `cd relaykit && GOWORK=off go build ./...`
  - `cd web && bun test src/features/channels/lib/channel-form.test.ts src/features/channels/components/drawers/sections/__tests__/vision-assist-multi-image-mode.test.tsx`
  - `cd web && bun run typecheck && bun run lint && bun run format:check && bun run build`
  - `go test ./... -count=1 && go vet ./...`
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong

```go
payloadBytes := 0
for _, image := range images {
	payloadBytes += len(visionAssistImageURL(image))
}
```

问题：图片字段长度不等于最终 JSON 请求体长度，会遗漏 prompt、用户问题、多图说明、模型字段、图片元数据和 JSON 转义开销。

#### Correct

```go
envelopeBytes, envelopeBytesOK := estimateVisionAssistRequestEnvelopeBytes(
	setting,
	prompt,
	userMessage,
	images,
)
```

要求：空 URL 请求骨架加各 URL 的 JSON 编码长度必须与 `common.Marshal(buildVisionAssistRequest(...))` 一致；估算失败时保守切割。

缓存边界也必须由最终图片组合决定：

```go
// Wrong: 请求级状态让等价批次无法跨对话复用。
parts = append(parts, "request_id:"+info.RequestId)

// Correct: 最终有序图片组合及其用户问题决定缓存键。
cacheKey := buildVisionAssistCacheKey(
	setting,
	prompt,
	unit.UserMessage,
	multiImageMode,
	unit.Images,
)
```

## 场景：WorkBuddy 系统上下文过滤与缓存键迁移

### 1. Scope / Trigger

- Trigger：修改视觉辅助派生用户文本的 WorkBuddy 过滤规则、缓存键文本来源、旧缓存兼容查询或请求内去重。
- 适用范围：`service/vision_assist.go`、`service/vision_assist_workbuddy.go` 及对应测试；主请求消息改写、公开 Relay DTO 和 `vision_assist:v1` 缓存值结构不在此契约内。
- 目标：只从视觉辅助派生文本中删除边界可靠的已知系统上下文，同时让部署前旧缓存可迁移复用，避免重复识图、审计和计费。

### 2. Signatures

```go
func filterWorkBuddyVisionAssistUserMessage(raw string) (effective string, changed bool)

type visionAssistUnit struct {
	Images            []VisionAssistImage
	UserMessage       string
	LegacyUserMessage string
}

func buildVisionAssistCacheKey(setting dto.ChannelVisionAssistSettings, prompt string, userMessage string, multiImageMode string, images []VisionAssistImage) string
```

### 3. Contracts

- 黑名单首项只包含完整的顶层 `<system-reminder ... data-role=user-context ...>...</system-reminder>`；标签名、属性名和值按 ASCII 大小写不敏感匹配，并把 `_` 与 `-` 视为等价。
- 开始标签允许属性重排、附加属性、常规空白、单双引号和无引号简单属性值。未闭合、畸形、嵌套、自闭合、非 `user-context` 或未知标签必须保留原文，不得猜测性删除。
- `user_query`、`image_local_path` / `image-local-path`、本地路径、图片引用标记和其他未知正文保持原顺序。只裁剪删除边界产生的空白，并用稳定换行连接剩余片段；没有匹配块时必须返回 `raw, false`。
- `UserMessage` 保存过滤后的 effective 文本，用于批次规划、请求体大小估算、辅助请求和 primary cache key；`LegacyUserMessage` 保存 raw 文本，只用于 legacy cache key 兼容查询。主请求始终保留 raw 文本。
- 缓存顺序固定为 primary -> legacy -> caller。只有 primary 正常未命中且两键不同时才查询 legacy；primary 读取错误不得继续访问同一异常缓存后端的 legacy key。
- Legacy 命中按普通缓存命中处理，并使用当前渠道 TTL 回填 primary；回填失败只告警，不能转为 caller。新上游非空结果只写 primary，禁止长期双写 legacy。
- 请求内去重只认 primary key。若较早单元已登记 primary miss，较晚等价单元通过不同 legacy key 命中，必须同时解析较早 pending 单元，不能再调用 caller。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 完整且等价的 `data-role=user-context` 顶层块 | 删除整块，`changed=true` |
| 未闭合、畸形、嵌套、自闭合或其他 role | 原文保留，不能越界删除 |
| 没有黑名单块 | 返回 `raw, false`，primary 与 legacy 键相同且不查询 legacy |
| 过滤后没有正文 | `UserMessage=""`，辅助请求退化为通用识图规则 |
| Primary 命中非空结果 | 直接复用，不查询 legacy，不进入 caller |
| Primary 正常未命中、legacy 命中非空结果 | 复用结果、按当前 TTL 回填 primary，不进入 caller |
| Primary 读取错误 | 记录现有风格告警，跳过 legacy 并进入 caller |
| Legacy 读取错误或缓存值为空 | 记录告警或视为未命中，进入 caller |
| Legacy 命中但 primary 回填失败 | 保持缓存命中结果，只记录告警 |
| 仅本地路径变化 | Primary key 变化，不能复用原路径缓存 |

### 5. Good/Base/Bad Cases

- Good：图片、查询和本地路径相同，只改变系统提醒中的身份文件或连接器状态；两次请求使用同一 primary key，第二次不调用辅助模型。
- Good：部署前 raw key 已缓存；部署后 primary miss、legacy hit，直接返回旧结果并回填 primary。
- Base：普通用户消息没有 WorkBuddy 标记；effective 与 raw 完全一致，维持原缓存行为。
- Bad：只提取 `<user_query>`，导致 `image_local_path`、未知标签或普通正文丢失。
- Bad：删除未闭合块直到消息末尾，误删真实用户问题。
- Bad：把请求 ID 加入 primary key，或新结果永久双写 primary/legacy，造成重复识别或高基数缓存。

### 6. Tests Required

- `TestFilterWorkBuddyVisionAssistUserMessage`：表格断言线上结构、大小写/属性/引号变体、多个块、空结果、非目标 role、未闭合、畸形和嵌套输入的精确输出与 `changed`。
- `TestApplyVisionAssistUsesSystemReminderFilteredCacheKey`：断言系统提醒变化复用 primary、本地路径变化 miss、caller 只看到保留正文且主请求仍含完整上下文。
- `TestApplyVisionAssistReusesLegacyWorkBuddyCacheAndBackfillsPrimary`：预置 legacy，断言 caller 为 0、primary 完成回填且缓存命中数正确。
- `TestApplyVisionAssistLegacyHitResolvesEarlierDuplicatePrimaryMiss`：断言较晚 legacy hit 同时解析较早等价 primary miss，caller 为 0 且两张图片均计为命中。
- 回归命令：`go test ./service -run 'VisionAssist' -count=1`、`go test -race ./service -run 'VisionAssist' -count=1`、`go test ./relay -run 'VisionAssist|RequestPreparationState' -count=1`、`go test ./... -count=1`、`go vet ./...`、`git diff --check`。

### 7. Wrong vs Correct

#### Wrong

```go
// 只用动态原文生成唯一缓存键，会让系统上下文变化反复触发识图。
cacheKey := buildVisionAssistCacheKey(setting, prompt, rawUserMessage, multiImageMode, images)
```

#### Correct

```go
effectiveUserMessage, _ := filterWorkBuddyVisionAssistUserMessage(rawUserMessage)
primaryKey := buildVisionAssistCacheKey(setting, prompt, effectiveUserMessage, multiImageMode, images)

_, primaryFound, primaryErr := getVisionAssistCache().Get(primaryKey)
if primaryErr == nil && !primaryFound && effectiveUserMessage != rawUserMessage {
	legacyKey := buildVisionAssistCacheKey(setting, prompt, rawUserMessage, multiImageMode, images)
	// legacy 命中后复用结果并按当前 TTL 回填 primary；新结果只写 primary。
}
```

## 场景：视觉辅助真实调用的独立消息审计生命周期

### 1. Scope / Trigger

- Trigger：修改视觉辅助请求准备顺序、重试、缓存、计费前置流程、上游调用或消息审计接入。
- 适用范围：`relay/vision_assist.go`、`relay/vision_assist_audit.go`、`service/message_audit.go` 及对应测试。
- 目标：只为实际构造并准备发送的视觉辅助调用创建独立审计记录，正文准确对应最终协议 DTO，且 capture 后所有退出路径都完成 finalize。

### 2. Signatures

```go
func callVisionAssistModel(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	request *dto.GeneralOpenAIRequest,
	images []service.VisionAssistImage,
) ([]service.VisionAssistResult, *types.NewAPIError)

func captureVisionAssistMessageAuditWithWriter(
	c *gin.Context,
	parent *relaycommon.RelayInfo,
	assistInfo *relaycommon.RelayInfo,
	request dto.Request,
	writer visionAssistMessageAuditWriter,
) visionAssistMessageAudit

func (audit visionAssistMessageAudit) finalize(apiErr *types.NewAPIError)
```

### 3. Contracts

- capture 必须发生在 `prepareVisionAssistRequest` 成功之后、token 估算之前；此时 `prepared.req` 已完成端点转换和模型映射。
- 审计正文必须直接使用 `prepared.req`，支持 OpenAI Chat、OpenAI Responses、Claude Messages 和 Gemini Native 四种最终 DTO；不得捕获主请求或转换前的 `GeneralOpenAIRequest` 中间值。
- 每次进入 `callVisionAssistModel` 的真实尝试使用 `assistInfo.RequestId` 作为独立审计请求 ID，并用父 `RelayInfo.RequestId` 建立关联。
- 请求级、Redis 或内存缓存命中不会进入辅助 caller，因此不得伪造审计记录；内部重试每次进入 caller，必须各自生成记录。
- capture 后必须立即注册统一 defer，并让命名返回值 `apiErr` 驱动 finalize；token、定价、并发、预扣费、上游调用和响应处理失败都不能绕过它。
- finalize 成功写入 `status=succeeded`、HTTP 200、实际耗时和映射后的消费日志模型；失败写入 `status=failed`、稳定错误码和错误 HTTP 状态，状态缺失时回退 500。
- `CaptureMessageAudit` 返回 false，或 writer 不完整时，finalize 必须是 no-op；审计不可用不得改变视觉辅助转发、重试、并发或计费行为。
- 视觉辅助审计不得保存上游响应正文；图片正文仍由消息审计现有媒体摘要和内容缩减策略处理。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 辅助渠道读取、启用状态或请求准备失败 | 尚未 capture，不生成记录 |
| 最终 DTO 准备成功 | capture 一条 `vision_assist` 独立记录，然后进入 token/计费流程 |
| token 估算或定价失败 | defer finalize 为 failed，记录稳定错误码和 HTTP 状态 |
| 渠道单用户并发拒绝 | 不调用上游，但已 capture 的记录 finalize 为 failed |
| 预扣费、上游请求或响应解析失败 | 已 capture 的记录 finalize 为 failed |
| 上游成功并完成结算 | finalize 为 succeeded，HTTP 200 |
| capture 返回 false | 继续原视觉辅助调用，结束时不调用 finalize writer |
| 缓存命中 | caller 不执行，不新增审计记录 |

### 5. Good / Base / Bad Cases

- Good：Responses 辅助请求转换为 `/v1/responses` 的 `OpenAIResponsesRequest` 后开始 capture；随后并发限制失败，记录以 failed 结束而不是长期 pending。
- Good：同一图片第一次调用上游并产生记录，第二次命中缓存直接复用结果，不新增虚假调用记录。
- Good：内部重试两次时生成两个独立请求 ID，主请求详情可以看到两次尝试各自的状态。
- Base：消息审计关闭时视觉辅助照常完成计费和上游调用。
- Bad：在 `prepareVisionAssistRequest` 前捕获原始 OpenAI 中间请求，导致 Claude、Responses 或 Gemini 审计正文与真实上游协议不一致。
- Bad：只在上游调用错误分支手工 finalize，遗漏 token、定价、并发和预扣费失败，使记录永久停留在 pending。

### 6. Tests Required

- `TestCaptureVisionAssistMessageAuditUsesPreparedProtocolRequest`：表格覆盖四种最终协议 DTO，断言正文对象、协议、路径、独立类型、关联 ID 和成功 finalize。
- `TestVisionAssistMessageAuditFinalizesFailureAndSkipsUnavailableCapture`：断言失败状态、稳定错误码、HTTP 状态和 capture 不可用时的 no-op。
- `TestCallVisionAssistModelRejectsConcurrencyBeforeUpstream`：断言并发拒绝发生在 capture 后，且 defer 将记录 finalize 为 failed。
- Service 视觉辅助回归必须覆盖每次重试进入 caller、缓存命中不进入 caller以及既有计费/失败策略不变。
- 回归命令：`go test ./relay ./service -run 'VisionAssist|RequestPreparationState' -count=1`，并在跨层改动后运行 `go test ./...` 与 `go vet ./...`。

### 7. Wrong vs Correct

#### Wrong

```go
service.CaptureMessageAudit(service.MessageAuditCaptureInput{
	RequestID: info.RequestId,
	Request:   request,
})
prepared, apiErr := prepareVisionAssistRequest(c, info, request, channel)
if apiErr != nil {
	return nil, apiErr
}
```

问题：捕获发生在协议转换前，正文不是实际发送 DTO；后续早退也可能没有统一 finalize。

#### Correct

```go
prepared, apiErr := prepareVisionAssistRequest(c, info, request, channel)
if apiErr != nil {
	return nil, apiErr
}

audit := captureVisionAssistMessageAuditWithWriter(
	c, info, prepared.info, prepared.req, auditWriter,
)
defer func() {
	audit.finalize(apiErr)
}()
```

要求：capture 时机与统一 defer 必须成对出现；新增任何 capture 后返回分支都自动经过同一 finalize。
