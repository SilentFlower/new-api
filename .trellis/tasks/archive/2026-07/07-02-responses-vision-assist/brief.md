# 任务交接摘要：适配 Responses 识图辅助

## 目标

- 让 `/v1/responses` 主请求在启用视觉辅助时也能先抽取 `input_image`、调用辅助识图模型、注入识图文本，并按配置移除原始图片，避免不支持图片的上游收到图片块后报错。

## 范围

- 放行 OpenAI Responses 主请求进入现有视觉辅助预处理链路。
- 在 `service/vision_assist.go` 增加 Responses 图片抽取逻辑，覆盖消息数组中 `content[].type = "input_image"`。
- 支持 `image_url` 字符串形式和对象 `{ "url": "...", "detail": "..." }` 形式。
- 在辅助识图成功后，把识图文本作为 Responses 文本块注入同一消息；默认 `strip_image=true` 时移除原 `input_image`，`strip_image=false` 时保留。
- 复用现有辅助请求构造、端点模式选择、计费、缓存、失败策略、重试和日志字段。
- 补相关单元测试，并运行 `go test ./dto ./service ./relay`。

## 不做范围

- 不重做视觉辅助提示词、缓存策略、并发策略或计费策略。
- 不修改前端配置 UI。
- 不默认为所有不支持 Responses 的渠道实现完整 `/v1/responses` 适配；只处理完成本需求所必需的最小渠道缺口。

## 关键上下文

- 当前 bug 表现为上游返回 `messages[3]: unknown variant image_url, expected text`，说明图片块被转发给了只接受文本块的上游。
- 现有 Chat 转 Responses 会把 chat `image_url` 转成 responses `input_image`：`service/openaicompat/chat_to_responses.go:226`。
- 现有视觉辅助成功后会移除 OpenAI Chat 主请求中的 `image_url` 并注入识图文本：`service/vision_assist.go:694`。
- 当前视觉辅助预处理只覆盖 OpenAI Chat Completions 主请求，`/v1/responses` 会被 `relay/vision_assist.go:49` 跳过。
- 相关项目规范为 `.trellis/spec/backend/relay-vision-assist.md`、`.trellis/spec/backend/quality-guidelines.md` 和 `.trellis/spec/guides/cross-layer-thinking-guide.md`。
- 所有 JSON 编解码必须使用 `common.Marshal` / `common.Unmarshal`，不能直接调用 `encoding/json`。

## 验收

- `/v1/responses` 请求包含 `input_image` 且目标渠道启用视觉辅助时，主请求在转发上游前包含识图文本。
- 默认 `strip_image=true` 时，转发上游的 Responses 请求体不再包含原始 `input_image` 图片块。
- `strip_image=false` 时，转发上游的 Responses 请求体同时包含识图文本和原始 `input_image` 图片块。
- `image_url` 字符串形式和对象形式都有测试覆盖。
- 现有 OpenAI Chat 视觉辅助测试继续通过。
- `go test ./dto ./service ./relay` 通过。

## 下一步

- 用户确认本 brief 后，运行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/07-02-responses-vision-assist`，随后进入 `trellis-route(implement)` 决定实现执行方式。
