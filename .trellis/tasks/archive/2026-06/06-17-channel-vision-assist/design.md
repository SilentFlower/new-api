# 渠道级视觉辅助识别设计

## 架构边界

本功能放在 relay 主链路的“选中渠道后、主请求计费前”预处理阶段，不放在具体渠道适配器内部。

理由：

- 目标渠道是否需要辅助取决于该渠道配置和 `model_mapping` 后的最终上游模型。
- OpenAI 兼容和 Claude 原生请求都需要复用同一套图片提取、缓存、辅助调用和回写逻辑。
- 主请求 token 估算和预扣费必须基于改写后的请求。

## 配置结构

在 `dto.ChannelSettings` 增加 `VisionAssist` 字段，配置保存在现有渠道 `setting` JSON 中，避免数据库迁移。

建议结构：

```go
type ChannelVisionAssistSettings struct {
    Enabled          bool     `json:"enabled,omitempty"`
    AssistChannelId  int      `json:"assist_channel_id,omitempty"`
    AssistModel      string   `json:"assist_model,omitempty"`
    TargetModels     []string `json:"target_models,omitempty"`
    Prompt           string   `json:"prompt,omitempty"`
    CacheTTLSeconds  int      `json:"cache_ttl_seconds,omitempty"`
    FailurePolicy    string   `json:"failure_policy,omitempty"`
    StripImage       bool     `json:"strip_image,omitempty"`
}
```

语义：

- `TargetModels` 为空表示该目标渠道所有含图请求都触发。
- `TargetModels` 非空时，按 `info.UpstreamModelName` 精确匹配；后续如有需要再扩展通配符。
- `FailurePolicy` MVP 支持 `error` 和 `skip`，推荐默认 `error`。
- `StripImage` 推荐默认 `true`，面向非视觉目标模型时移除原始图片。

## 主链路改造

新增预处理入口：

```go
func PrepareRequestForSelectedChannel(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError
```

调用位置：

- `controller.Relay` 中每次选中渠道后调用。
- 对首个渠道，应在 `request.GetTokenCountMeta()`、敏感词检查、`EstimateRequestToken`、`PreConsumeBilling` 之前完成。
- 对 retry 渠道，也需要重新初始化渠道配置和模型映射；但视觉辅助应依赖缓存避免重复识别。

预处理职责：

1. `info.InitChannelMeta(c)`。
2. 对 OpenAI / Claude 请求执行 `helper.ModelMappedHelper(c, info, request)`。
3. 若全局或渠道透传开启，则跳过视觉辅助。
4. 若上下文存在内部辅助调用标记，则跳过视觉辅助。
5. 按渠道 `VisionAssist` 配置和 `info.UpstreamModelName` 判断是否触发。
6. 触发时调用 `service.ApplyVisionAssist` 改写 `info.Request`。
7. 标记本次请求已完成预处理，`TextHelper` / `ClaudeHelper` 不重复执行相同逻辑。

## 请求解析与回写

新增 service 层能力，文件建议为 `service/vision_assist.go`。

核心抽象：

```go
type VisionAssistImage struct {
    Index     int
    Source    types.FileSource
    Detail    string
    MimeType  string
}

type VisionAssistResult struct {
    Image     VisionAssistImage
    Text      string
    CacheHit  bool
}
```

OpenAI：

- 提取 `messages[].content[]` 中 `type == "image_url"` 的图片。
- 将结果插入同一条消息的文本内容，或追加一条紧随其后的 `user` 文本消息。
- `StripImage=true` 时移除 `image_url` 块。

Claude：

- 提取 `messages[].content[]` 中 `type == "image"` 且 `source` 存在的图片。
- 将结果写成 `type == "text"` 的内容块。
- `StripImage=true` 时移除图片块。

改写文本格式：

```text
[图片内容]
以下内容是当前用户消息中图片的可见信息，请直接用于回答用户。
图片 1：...
图片 2：...
```

该文本只面向下游目标模型表达图片内容上下文，不能暴露“辅助识别”、缓存、转写等实现细节，避免目标模型回答时声明自己只是读取了辅助结果。

## 辅助视觉调用

MVP 推荐只用 OpenAI 兼容 chat completions 形式调用辅助模型，即内部构造 `dto.GeneralOpenAIRequest`：

- `Model = AssistModel`
- `Stream = false`
- `Messages` 包含辅助提示词、原图内容块和必要上下文

辅助渠道选择：

- MVP 强绑定 `AssistChannelId`，从 `model.CacheGetChannel` / `model.GetChannelById` 获取。
- 使用临时上下文设置辅助渠道信息，并设置防循环标记。
- 内部调用应尽量复用现有 adaptor 转换和响应处理能力，但不能污染主请求的 `gin.Context` 渠道字段；如复用现有上下文，必须保存并恢复相关 context key。

## 缓存设计

缓存 key：

```text
vision_assist:v1:{assist_channel_id}:{assist_model}:{prompt_hash}:{image_hash}:{detail}:{mime_type}
```

`image_hash`：

- URL 图片：规范化 URL 后 hash。
- base64 / data URL：对数据内容 hash，不把原图写入缓存。

缓存层：

- 优先使用 Redis；未启用 Redis 时可使用内存缓存作为进程级退化。
- TTL 来自渠道配置，默认 86400 秒。

缓存命中后直接复用描述，不发辅助请求。

## 计费与观测

主请求：

- 使用改写后的 `info.Request` 进入 `GetTokenCountMeta`、敏感词检查、`EstimateRequestToken` 和 `PreConsumeBilling`。

辅助请求：

- 按实际使用的 `assist_channel_id` 与 `assist_model` 单独向用户扣费。
- 日志中标记为 `vision_assist`，并记录目标渠道、目标模型、重定向后上游模型、辅助渠道、辅助模型、缓存命中、耗时和错误。
- 缓存命中不产生新的辅助模型上游调用，也不重复扣除辅助模型费用。
- 辅助调用失败且 `FailurePolicy=error` 时，已预扣的辅助费用必须按现有 BillingSession 语义退款。

## 风险与兼容

- 透传模式下跳过改写，避免破坏用户期待的原始请求体。
- `ModelMappedHelper` 需要变成幂等或由预处理阶段统一调用，避免 `TextHelper` / `ClaudeHelper` 重复映射。
- Claude thinking 后缀适配目前在 `ClaudeHelper` 内部处理；视觉辅助触发只依赖 `model_mapping` 后模型，不依赖 thinking 运行时改写。
- 不新增数据库字段，三库兼容风险低。
