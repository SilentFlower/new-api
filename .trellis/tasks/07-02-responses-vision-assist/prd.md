# 适配 Responses 识图辅助

## 目标

当主请求走 `/v1/responses` 且目标渠道启用了视觉辅助时，系统必须像 OpenAI Chat Completions 主请求一样先抽取图片、调用辅助识图模型、把识图结果注入主请求，并按配置移除原始图片，避免不支持图片的上游收到 `image_url` / `input_image` 后返回反序列化错误。

## 背景

- 用户反馈的上游错误为：`messages[3]: unknown variant image_url, expected text`，说明图片块被转发到了只接受文本块的上游。
- 已确认 Chat 转 Responses 时会把 chat `image_url` 转成 responses `input_image`：`service/openaicompat/chat_to_responses.go:226`。
- 已确认现有视觉辅助成功后默认会移除 OpenAI Chat 主请求中的 `image_url`，只注入识图文本：`service/vision_assist.go:694`。
- 已确认当前视觉辅助预处理只覆盖 OpenAI Chat Completions 主请求，`/v1/responses` 主请求会被 `relay/vision_assist.go:49` 跳过。
- 已确认视觉辅助契约要求修改图片抽取、辅助请求构造、端点模式或辅助渠道调用时，必须维护 `service`、`dto`、`relay` 三层之间的格式契约，并补相关测试：`.trellis/spec/backend/relay-vision-assist.md`。

## 需求

- R1：`/v1/responses` 主请求在 `vision_assist.enabled=true`、目标模型匹配、未启用请求体透传时，必须进入视觉辅助预处理。
- R2：视觉辅助必须能从 `dto.OpenAIResponsesRequest.Input` 中抽取图片输入，至少覆盖 Responses 标准消息数组里的 `content[].type = "input_image"`。
- R3：图片源解析必须兼容 `image_url` 为字符串，以及 `image_url` 为对象且包含 `url` 字段的情况；保留可用的 `detail` 信息用于辅助请求和缓存 key。
- R4：辅助识图请求构造、端点模式选择、计费、缓存、失败策略和日志字段必须复用现有视觉辅助机制，不新增独立调用链。
- R5：辅助识图成功后，必须把识别文本以 Responses 可接受的文本内容注入到对应消息中；默认 `strip_image=true` 时必须移除原 `input_image`，避免上游继续收到图片块。
- R6：`strip_image=false` 时允许保留原 `input_image`，但仍必须注入识别文本，行为与现有 OpenAI Chat 视觉辅助配置语义一致。
- R7：`failure_policy=skip` 时允许保持原请求继续转发，并记录失败日志；`failure_policy=error` 时保持现有错误中止语义。
- R8：不得破坏现有 OpenAI Chat、Claude Messages、Gemini Native 视觉辅助行为。
- R9：所有新增 JSON 编解码必须使用 `common.Marshal` / `common.Unmarshal` 等项目封装，不能直接调用 `encoding/json`。

## 不做范围

- 不在本任务中重做视觉辅助提示词、缓存策略、并发策略或计费策略。
- 不在本任务中修改前端配置 UI。
- 不默认为所有不支持 Responses 的渠道实现完整 `/v1/responses` 适配；实现阶段只处理为完成本需求所必需的最小渠道缺口。

## 验收标准

- [ ] `/v1/responses` 请求包含 `input_image` 且目标渠道启用视觉辅助时，主请求在转发上游前会包含识图文本。
- [ ] 默认 `strip_image=true` 时，转发上游的 Responses 请求体不再包含原始 `input_image` 图片块。
- [ ] `strip_image=false` 时，转发上游的 Responses 请求体同时包含识图文本和原始 `input_image` 图片块。
- [ ] `image_url` 字符串形式和对象 `{ "url": "...", "detail": "..." }` 形式都有测试覆盖。
- [ ] 现有 OpenAI Chat 视觉辅助测试继续通过。
- [ ] 至少运行并通过相关包测试：`go test ./dto ./service ./relay`。

## 备注

- 本任务是跨 `dto`、`service`、`relay` 的后端兼容修复，规划阶段需要保留数据流和契约约束。
