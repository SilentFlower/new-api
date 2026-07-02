# 设计：适配 Responses 识图辅助

## 数据流

1. 客户端发送 `/v1/responses` 请求，`input` 中可能包含 `content[].type = "input_image"`。
2. `controller/relay.go` 选择渠道后调用 `relay.PrepareRequestForSelectedChannel`。
3. `relay/vision_assist.go` 在未透传、配置启用、目标模型匹配时允许 Responses 主请求进入 `service.ApplyVisionAssist`。
4. `service/vision_assist.go` 从 `dto.OpenAIResponsesRequest.Input` 抽取图片，复用现有 `buildVisionAssistRequest` 构造内部 OpenAI Chat 兼容辅助请求。
5. 辅助渠道调用、计费、缓存、重试、日志仍走现有路径。
6. 辅助成功后，把识图文本作为 Responses 文本内容注入对应消息；默认移除原 `input_image`。
7. 主请求继续走原有 Responses relay 转换和上游转发链路。

## 契约

- Responses 输入只在视觉辅助抽取和改写阶段解析；上游转发仍保留 `dto.OpenAIResponsesRequest.Input` 的 JSON 结构。
- 图片块识别范围：
  - `input` 为消息数组。
  - 消息对象含 `content` 数组。
  - 内容项 `type = "input_image"`。
  - `image_url` 支持字符串或对象 `{ "url": "...", "detail": "..." }`。
- 注入文本使用 Responses 文本块，优先保持同一消息内顺序，在首个图片块位置前插入一次识图文本。
- `strip_image=true` 时删除同一消息内被识别的 `input_image`；`strip_image=false` 时保留原图片块。
- 失败策略、缓存 key、辅助请求格式、端点模式解析和日志字段复用现有视觉辅助行为。

## 变更边界

- `relay/vision_assist.go`：允许 OpenAI Responses 主请求进入预处理。
- `service/vision_assist.go`：新增 Responses 图片抽取和主请求改写分支。
- `dto/openai_request.go`：仅在确有必要时补充 Responses 输入解析辅助类型或方法；避免破坏现有 `Input json.RawMessage` 契约。
- 测试优先放在 `service/vision_assist_test.go` 和 `relay/vision_assist_test.go`，覆盖抽取、改写和 relay 入口判断。

## 风险与取舍

- Responses `input` 形态较灵活；本任务先覆盖标准消息数组和 `content` 数组形态，避免过度泛化造成不稳定改写。
- 如果某渠道自身未实现 `ConvertOpenAIResponsesRequest`，视觉辅助改写只能解决图片残留问题，不能替代渠道 Responses 适配器实现；实现阶段需要显式检查并决定是否补最小缺口。
- 改写 `json.RawMessage` 时必须使用结构化解析和 `common.Marshal` / `common.Unmarshal`，不能用字符串替换。

## 回滚

- 回滚 `service/vision_assist.go` 的 Responses 分支和 `relay/vision_assist.go` 的入口放行即可恢复原行为。
- 测试新增文件或用例可随代码一并回滚，不涉及数据库迁移或配置迁移。
