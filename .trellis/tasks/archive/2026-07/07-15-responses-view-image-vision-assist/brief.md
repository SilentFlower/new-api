# Brief — 修复 Responses view_image 识图辅助触发

## Goal

- 修复 `/v1/responses` 工具输出携带图片时未触发视觉辅助的问题，使 Codex `view_image` 图片进入现有辅助识图链路，并让后续主请求获得可用的图片文字描述。

## Scope

- 从顶层 `function_call_output.output` 和 `custom_tool_call_output.output` 数组抽取 `input_image`，按内容结构触发，不依赖工具名或 `call_id`。
- 复用现有图片 URL/detail/MIME 解析、缓存、重复图片复用、并发、重试、计费、失败策略和日志链路。
- `function_call_output` 在原 `output` 数组写入识图文本；`custom_tool_call_output` 处理原图片后，在其后插入普通用户文本消息，避免描述被目标转换器过滤。
- 支持 `strip_image=true/false`，并保持原有非图片输出项及顺序。
- 在 `service/vision_assist.go` 与 `service/vision_assist_test.go` 内完成最小实现和回归覆盖。

## Non-Goals

- 不修改视觉辅助配置 UI、数据库、提示词、缓存、并发或计费策略。
- 不为无关 Responses 输入项新增完整协议转换能力，不改变现有目标转换器的自定义工具过滤策略。
- 不处理 `/v1/responses/compact`、请求体透传或视觉辅助递归调用。
- 原则上不修改 DTO、relay 入口或渠道适配器。

## Key Context

- 既有实现只扫描 `service/vision_assist.go` 中普通消息的 `content[].input_image`，未扫描工具输出 `output`。
- 已从本机 Codex 真实会话确认两种输入：直接工具返回 `function_call_output.output=[input_image]`；统一工具执行返回 `custom_tool_call_output.output=[input_text,input_image]`。
- `dto.OpenAIResponsesRequest.Input` 继续保持 `json.RawMessage`，所有解析和序列化必须使用 `common.Unmarshal` / `common.Marshal`。
- `VisionAssistImage.MessageIndex` 继续作为顶层输入项索引，不新增定位状态。
- 自定义工具输出在部分转换器中会被过滤，因此识图描述必须放入紧邻的普通用户消息；普通消息在转换后仍可见。
- 风险集中在共享视觉辅助改写：不得造成普通 Responses、OpenAI Chat、Claude 图片处理回退，不得重复插入描述或误删非图片输出。

## Acceptance

- `function_call_output` 和 `custom_tool_call_output` 中的图片都能触发辅助 caller。
- 字符串及对象形式的 `image_url`、detail、MIME 类型均正确解析。
- `function_call_output` 原位获得识图文本；`custom_tool_call_output` 保留非图片输出，并紧邻获得普通用户描述消息。
- `strip_image=true` 移除已识别图片；`strip_image=false` 同时保留描述和图片。
- 无图片工具输出不调用 caller、不改写原始 JSON。
- 重复图片只调用一次辅助模型，同时保持所有图片位置的编号结果。
- `go test ./dto ./service ./relay`、`go test ./...` 与 `git diff --check` 通过。

## Next Step

- 用户确认规划三件套和本 brief 后，运行 `task.py start`；任务进入 `in_progress` 后先执行 `trellis-route(implement)`，再按路由结果实施。
