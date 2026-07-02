# 实施计划：适配 Responses 识图辅助

## 步骤

1. 梳理 `dto.OpenAIResponsesRequest.Input` 的现有解析方法，确认是否能复用 `ParseInput`，以及是否需要保留消息索引用于改写。
2. 在 `service/vision_assist.go` 增加 Responses 图片抽取逻辑：
   - 支持 `content[].type = "input_image"`。
   - 支持 `image_url` 字符串和对象形式。
   - 记录 `MessageIndex`，用于按消息注入识图文本。
3. 在 `service/vision_assist.go` 增加 Responses 主请求改写逻辑：
   - 将识图文本插入同一消息的首个图片块前。
   - `strip_image=true` 删除图片块。
   - `strip_image=false` 保留图片块。
   - 改写后重新写回 `OpenAIResponsesRequest.Input`。
4. 在 `relay/vision_assist.go` 放行 OpenAI Responses 主请求的视觉辅助预处理，同时保持透传、递归处理、非目标模式的跳过逻辑。
5. 补单元测试：
   - Responses 抽取支持字符串 `image_url`。
   - Responses 抽取支持对象 `image_url.url/detail`。
   - Responses 改写默认删除 `input_image` 并注入 `input_text`。
   - Responses 改写在 `strip_image=false` 时保留 `input_image`。
   - 现有 OpenAI Chat 行为不回退。
6. 运行验证命令并修复失败：
   - `go test ./dto ./service ./relay`

## 检查点

- 所有 JSON 编解码必须通过 `common.Marshal` / `common.Unmarshal`。
- 不修改数据库、前端、渠道管理 UI。
- 不修改受保护项目信息。
- 不把辅助识图结果写入待转发请求之外的无关上下文字段。

## 可能追加

- 如果实现检查发现 DeepSeek 渠道在当前路径必须支持 `ConvertOpenAIResponsesRequest` 才能完成验收，补最小必要适配，并增加对应测试或明确记录为后续任务。
