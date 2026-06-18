# 设计方案

## 配置结构

在 `dto.ChannelVisionAssistSettings` 中新增字段：

```go
EndpointMode   string `json:"endpoint_mode,omitempty"`
MaxConcurrency int    `json:"max_concurrency,omitempty"`
RetryCount     int    `json:"retry_count,omitempty"`
RetryBackoffMs int    `json:"retry_backoff_ms,omitempty"`
```

端点模式使用字符串枚举，避免数据库迁移和历史 JSON 兼容问题。运行时统一归一化：

- 空值 -> `auto`
- 未知值 -> `auto`
- `max_concurrency <= 0` -> `1`
- `retry_count < 0` -> `0`
- `retry_backoff_ms <= 0` -> `500`

## 端点模式

新增内部概念 `VisionAssistEndpointMode`，用于决定辅助请求的构造、适配器入口和响应解析。

候选值：

- `auto`
- `openai_chat`
- `openai_responses`
- `anthropic_messages`
- `gemini_native`

`auto` 根据辅助渠道类型解析为实际模式：

- Gemini / Vertex Gemini -> `gemini_native`
- Claude / Anthropic -> `anthropic_messages`
- 其他 -> `openai_chat`

实际实现需要结合 `constant.ChannelType*` 或现有渠道类型常量，避免用展示名称判断。

## 请求执行模型

保持单图请求，不做多图合并。原因：

- 当前缓存粒度是单图，单图请求天然匹配缓存。
- 单图失败可以明确定位是哪张图。
- 多图结构化返回依赖模型遵循 JSON，容易污染缓存。
- Gemini 400 这类问题需要缩小变量，而不是把多张图合并进同一个请求。

`ApplyVisionAssist` 的未命中图片处理从串行循环改为有限并发 worker：

1. 先按现有逻辑查请求内重复和持久缓存。
2. 对唯一未命中图片构造任务队列。
3. 使用 `max_concurrency` 个 worker 调用辅助模型。
4. 每个任务独立重试。
5. 汇总成功结果、失败信息和统计字段。
6. 成功结果按原始图片 `Index` 排序后写缓存并改写主请求。

## 上下文隔离

当前 `callVisionAssistModel` 会通过 `switchContextToVisionAssistChannel` 修改传入的 `gin.Context`。并发后不能多个 worker 共享同一个 `gin.Context`。

可选实现方式：

1. 为每个辅助请求创建隔离的 Gin context，复制必要请求、headers、用户、分组、token、日志上下文字段。
2. 或重构 `callVisionAssistModel`，使辅助渠道上下文写入独立 `RelayInfo`，不再依赖修改父级 Gin context。

优先选择改动面较小的隔离 context 方案，并通过测试验证并发调用不会串扰 `ContextKeyChannelId` / `ContextKeyChannelSetting`。

## 重试分类

新增 helper 判断错误是否可恢复：

- 可恢复：网络错误、超时、HTTP 429、HTTP 5xx、临时空响应。
- 不可恢复：HTTP 400、`INVALID_ARGUMENT`、MIME 不支持、图片解析失败、模型能力不支持、请求转换失败。

当前 `types.NewAPIError` 需要检查可用字段和已有错误码。如果错误结构不足以精确判断，先用 status code 与错误码做保守分类：未知错误不盲目重试，避免重复扣费。

重试退避：

```text
sleep = retry_backoff_ms * attempt
```

其中 `attempt` 从 1 开始。需要尊重请求 context cancellation。

## 日志字段

在主请求 `log.other` 中补充：

- `vision_assist_endpoint_mode`
- `vision_assist_resolved_endpoint_mode`
- `vision_assist_max_concurrency`
- `vision_assist_retry_count`
- `vision_assist_retry_backoff_ms`
- `vision_assist_retry_attempts`
- `vision_assist_failed_image_count`
- `vision_assist_last_error_code`
- `vision_assist_last_error`

错误摘要使用 `common.LocalLogPreview`，禁止记录图片内容。

## 前端

默认 UI：

- `ChannelSettings.vision_assist` 类型增加四个字段。
- 表单 schema 增加对应字段和最小值校验。
- `setting` JSON 构建与读取逻辑增加字段。
- 抽屉高级配置区域增加端点模式下拉、并发数、重试次数、退避时间输入。
- `channel-form-errors` 把新增字段归入高级配置错误。
- 补齐 `en/zh/fr/ja/ru/vi` 文案。

经典 UI：

- `originInputs`、读取、保存、重置、清理临时字段逻辑增加新增字段。
- 视觉辅助配置区域增加 `Form.Select` 与 `Form.InputNumber`。
- 保持 `buildChannelExtraSettings` 合并未知 `vision_assist` 字段。
- 补齐 classic 现有 locale 文案。

## 风险

- Gemini 原生与 Vertex Gemini 的路径、认证、响应解析可能已有差异，需要复用现有 Gemini/Vertex 适配器而不是手写 HTTP。
- Responses 和 Anthropic Messages 的内部请求构造可能需要新增最小转换函数，避免引入完整 relay 分支重复。
- 并发可能改变辅助扣费日志顺序；主请求日志只记录聚合统计，辅助请求自身仍按各自请求记录。
- skip 模式下部分图片失败时，移除原始图片可能导致最终模型完全看不到失败图片；需要明确仅成功图片注入，失败图片是否保留沿用现有 `strip_image` 行为或做最小调整。
