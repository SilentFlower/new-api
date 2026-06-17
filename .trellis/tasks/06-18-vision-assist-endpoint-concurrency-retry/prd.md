# 视觉辅助端点并发与重试配置

## 背景

当前渠道级视觉辅助识别会把目标请求中的图片提取出来，逐张调用配置的辅助渠道与辅助模型生成图片描述，再把原请求改写为文本请求。现有实现固定使用 OpenAI Chat Completions 语义构造内部辅助请求，再交给辅助渠道适配器转换。

用户在使用 Gemini 辅助模型时遇到上游返回：

```json
{
  "error": {
    "code": 400,
    "message": "Request contains an invalid argument.",
    "status": "INVALID_ARGUMENT"
  }
}
```

当前代码已经会把 OpenAI `image_url` 转换为 Gemini `inlineData`，因此问题不一定是字段格式直传，更可能是辅助请求入口语义、模型参数组合、模型能力或图片内容导致。需要允许管理员按渠道选择辅助请求端点/格式，并为多图场景提供有限并发与失败重试配置。

## 目标

- 让渠道配置中的视觉辅助可以选择辅助请求端点/格式，减少 Gemini、Claude、Responses 等不同上游因为固定 Chat Completions 语义产生的不兼容。
- 让一个请求内的多张图片可以有限并发处理，降低串行等待时间，同时保持单图请求、单图缓存和单图失败定位。
- 让辅助模型临时失败时可以按配置重试，避免网络抖动、429、5xx 等可恢复错误直接导致主请求失败。
- 默认 UI 和经典 UI 都能配置这些新增字段。

## 已确认事实

- 当前视觉辅助配置位于 `setting.vision_assist`，后端 DTO 是 `dto.ChannelVisionAssistSettings`。
- 当前已支持字段：启用、辅助渠道 ID、辅助模型、目标上游模型、辅助提示词、缓存 TTL、失败策略、是否移除原始图片。
- 当前缓存粒度是单张图片，缓存 key 包含辅助渠道、辅助模型、prompt、图片源、detail、mime。
- 当前未命中缓存的图片是逐张串行请求辅助模型。
- 当前 `callVisionAssistModel` 会切换 Gin context 与渠道上下文，不能直接在同一个 context 上 goroutine 并发。
- `callVisionAssistModel` 虽然参数能接收多张图，但现有返回逻辑会把同一段响应文本套到每张图上，因此不能直接改成一次请求多张图，否则会污染单图缓存。
- 用户明确要求经典 UI 配置里也可以配置新增项。
- 用户已确认 `auto` 模式规则：Gemini / Vertex Gemini 默认走 `gemini_native`，Claude / Anthropic 默认走 `anthropic_messages`，其他默认走 `openai_chat`。

## 需求

1. 渠道视觉辅助配置新增端点/格式模式：
   - `auto`：按辅助渠道类型自动选择。
   - `openai_chat`：OpenAI Chat Completions 语义。
   - `openai_responses`：OpenAI Responses 语义。
   - `anthropic_messages`：Anthropic Messages 语义。
   - `gemini_native`：Gemini 原生 `generateContent` 语义。

2. `auto` 模式需要按辅助渠道类型选择合理默认：
   - Gemini / Vertex Gemini 类型优先走 `gemini_native`。
   - Claude / Anthropic 类型优先走 `anthropic_messages`。
   - 其他 OpenAI 兼容类型默认走 `openai_chat`。
   - Responses 作为显式模式，不作为所有 OpenAI 兼容渠道的默认。

3. 渠道视觉辅助配置新增执行控制字段：
   - `max_concurrency`：单个主请求内视觉辅助图片处理并发数。
   - `retry_count`：单张图片辅助请求失败后的重试次数。
   - `retry_backoff_ms`：重试基础等待时间，按尝试次数做简单递增退避。

4. 并发处理要求：
   - 每张图片仍独立请求辅助模型，不做多图合并请求。
   - 每张图片独立写入缓存，重复图片仍按现有 cache key 复用。
   - 并发必须隔离辅助请求上下文，不能让多个 goroutine 同时修改同一个 Gin context 的渠道字段。
   - 结果写回原请求时仍按图片原始顺序稳定排序。

5. 重试要求：
   - 只重试可恢复错误：网络错误、超时、429、5xx、临时空响应等。
   - 默认不重试 400 `INVALID_ARGUMENT`、不支持 MIME、图片解析失败、模型不支持图片等不可恢复错误。
   - `failure_policy=error` 时，任一图片最终失败应返回错误。
   - `failure_policy=skip` 时，失败图片跳过，成功图片继续注入，原始图片是否保留仍遵循 `strip_image` 与现有改写逻辑。

6. 日志要求：
   - `log.other` 中记录实际端点模式、最大并发数、重试配置、重试次数统计、失败图片数。
   - 失败时记录最后错误码或错误摘要，便于定位 Gemini 400 这类问题。
   - 不记录完整图片 base64 或过长上游响应。

7. 前端配置要求：
   - 默认 UI 的渠道创建/编辑表单支持新增字段，保存到 `setting.vision_assist`。
   - 经典 UI 的渠道创建/编辑表单支持新增字段，保存到 `setting.vision_assist`。
   - 两套 UI 都必须保留已有 `setting` JSON 中未知字段，保存时不能丢失。
   - 新增文案需要补齐现有前端多语言文件。

## 默认值与兼容性

- 历史渠道缺少新增字段时必须继续可用。
- 运行时建议兼容默认：
  - `endpoint_mode` 空值等同 `auto`。
  - `max_concurrency <= 0` 时按 `1` 处理，保持历史串行行为。
  - `retry_count < 0` 时按 `0` 处理。
  - `retry_backoff_ms <= 0` 时使用安全默认值，例如 `500`。
- UI 新建渠道可显示推荐值：
  - `endpoint_mode=auto`
  - `max_concurrency=2`
  - `retry_count=1`
  - `retry_backoff_ms=500`

## 非目标

- 不把多张图片合并到一次辅助模型请求。
- 不做全局队列或跨请求并发限流。
- 不改变主请求对目标渠道的模型映射与计费语义。
- 不在日志中保存图片内容。
- 不让管理员输入任意 URL path；本任务使用枚举模式保证请求体与响应解析匹配。

## 验收标准

- [ ] 后端 DTO 支持 `endpoint_mode`、`max_concurrency`、`retry_count`、`retry_backoff_ms`，历史 JSON 缺字段时行为兼容。
- [ ] `auto` 模式能按辅助渠道类型选择 Gemini 原生、Claude Messages 或 OpenAI Chat 语义。
- [ ] Gemini 原生模式不再通过 OpenAI Chat Completions 语义构造辅助请求。
- [ ] 一个请求内多张未命中缓存图片可以按配置有限并发处理，结果顺序稳定。
- [ ] 并发执行不会共享可变 Gin context 导致渠道上下文串扰。
- [ ] 可恢复错误按 `retry_count` 和 `retry_backoff_ms` 重试，不可恢复 400 默认不重试。
- [ ] `failure_policy=error` 与 `failure_policy=skip` 的行为符合现有语义并覆盖多图部分失败场景。
- [ ] 单图缓存仍按现有 cache key 独立写入，重复图片不重复请求。
- [ ] `log.other` 增加端点模式、并发、重试与失败统计字段。
- [ ] 默认 UI 和经典 UI 都可以编辑新增配置，并保留未知 `setting` 字段。
- [ ] 前端新增文案完成现有语言文件补齐。
- [ ] 增加后端单元测试覆盖默认兼容、并发限制、重试分类、skip/error 失败策略。
