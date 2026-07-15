# 修复 Responses view_image 识图辅助触发

## Goal

修复 `/v1/responses` 请求中工具输出携带图片时未触发视觉辅助的问题，使 Codex 通过 `view_image` 查看本地图片后，图片能够进入现有辅助识图、缓存、计费、失败处理和主请求改写链路，并让不支持视觉的目标模型收到可用的图片文字描述。

## Background

- 2026-07-02 的既有适配只覆盖 Responses 标准消息数组中的 `content[].type = "input_image"`；对应实现位于 `service/vision_assist.go:575` 和 `service/vision_assist.go:791`。
- 本机 Codex 真实会话记录确认存在两种当前未覆盖的图片回传结构：
  - 直接工具调用：顶层 `function_call_output.output` 为包含 `input_image` 的数组。
  - 统一工具执行：顶层 `custom_tool_call_output.output` 为包含 `input_text`、`input_image` 的数组。
- 现有抽取逻辑只读取每个顶层输入项的 `content` 数组，不读取工具输出项的 `output` 数组，因此上述两种请求不会产生任何 `VisionAssistImage`，视觉辅助调用也不会发生。
- Codex 的历史版本也可能在 `view_image` 工具结果后追加独立的 `role=user` 图片消息；该标准消息形态已经由现有实现覆盖，本任务不得使其回退。
- OpenAI Responses API 官方契约允许请求输入同时承载文本、图片和工具调用结果；本任务保持 `dto.OpenAIResponsesRequest.Input` 的原始 JSON 契约，不把灵活输入结构收窄为固定 DTO。

## Requirements

- R1：当 `/v1/responses` 顶层 `function_call_output.output` 数组包含 `type = "input_image"` 时，视觉辅助必须能够抽取图片并调用现有辅助识图链路。
- R2：当 `/v1/responses` 顶层 `custom_tool_call_output.output` 数组包含 `type = "input_image"` 时，视觉辅助必须能够抽取图片并调用现有辅助识图链路。
- R3：工具输出中的 `input_image.image_url` 必须兼容字符串和对象 `{ "url": "...", "detail": "...", "mime_type": "..." }`，并沿用现有 detail、MIME 类型、缓存键和文件源处理规则。
- R4：辅助识图成功后，识别文本必须进入后续主请求可见的 Responses 文本内容；写回时保留原有非图片输出项及其顺序。
- R5：默认 `strip_image=true` 时必须移除已识别的工具输出 `input_image`；`strip_image=false` 时必须保留原图片，同时仍注入识别文本。
- R6：同一工具输出包含多张图片时，必须复用现有批量抽取、去重、缓存和结果编号语义，不得重复调用相同图片。
- R7：现有标准消息 `content[].input_image`、OpenAI Chat、Claude Messages 和各视觉辅助端点模式的行为不得回退。
- R8：辅助渠道调用、计费、并发、重试、缓存、失败策略和日志字段必须复用现有视觉辅助机制，不新增独立调用链。
- R9：所有 JSON 编解码必须使用 `common.Marshal` / `common.Unmarshal` 等项目封装，不得直接调用 `encoding/json`。

## Technical Notes

- 已确认按内容结构处理任何 `function_call_output` / `custom_tool_call_output` 中的 `input_image`，不依赖工具名或 `call_id` 关联。
- 该规则同时覆盖直接 `view_image`、通过统一 `exec` 间接调用 `view_image`，以及其他能够返回图片内容的工具。
- 工具输出不含 `input_image` 时不得触发视觉辅助，也不得改写原请求。
- `function_call_output` 的识图文本写回原 `output` 数组；`custom_tool_call_output` 因部分目标转换器会过滤自定义工具项，识图文本写入紧邻其后的普通 `role=user` 消息，确保转换后仍然可见。

## Out of Scope

- 不修改视觉辅助配置 UI、数据库结构、提示词、缓存策略、并发策略或计费策略。
- 不为与本缺陷无关的 Responses 输入项新增完整协议转换能力。
- 不处理 `/v1/responses/compact`；现有视觉辅助入口仍只放行普通 `/v1/responses`。

## Acceptance Criteria

- [ ] `function_call_output.output=[{"type":"input_image",...}]` 能触发一次视觉辅助调用，并把识别文本写回同一工具输出项。
- [ ] `custom_tool_call_output.output=[{"type":"input_text",...},{"type":"input_image",...}]` 能触发视觉辅助调用，改写后保留原 `input_text`，并在紧邻的普通用户消息中包含识别文本。
- [ ] 默认 `strip_image=true` 时，两种工具输出形态的转发请求均不再包含已识别的 `input_image`。
- [ ] `strip_image=false` 时，识别文本和原 `input_image` 同时存在。
- [ ] 工具输出中的字符串 `image_url` 和对象 `image_url.url/detail/mime_type` 均有测试覆盖。
- [ ] 不含 `input_image` 的工具输出不调用辅助模型，且请求 JSON 保持不变。
- [ ] 同一工具输出中重复图片只调用一次辅助模型，但每个图片位置都按原有编号语义获得识别结果。
- [ ] 现有标准消息形态的 Responses 视觉辅助测试继续通过。
- [ ] 相关包测试通过：`go test ./dto ./service ./relay`。
- [ ] 跨层回归测试通过：`go test ./...`。
