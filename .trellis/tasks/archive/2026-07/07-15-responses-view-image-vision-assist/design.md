# 设计：修复 Responses 工具输出识图辅助

## 问题定义

Responses 视觉辅助只识别普通消息的 `content[].input_image`，而 Codex 会把 `view_image` 结果放在顶层工具输出项的 `output` 数组中，导致图片抽取结果为空，辅助识图链路不执行。

## 已确认输入契约

直接工具调用使用 `function_call_output`：

```json
{
  "type": "function_call_output",
  "call_id": "call_xxx",
  "output": [
    {"type": "input_image", "image_url": "data:image/png;base64,...", "detail": "high"}
  ]
}
```

Codex 统一工具执行使用 `custom_tool_call_output`：

```json
{
  "type": "custom_tool_call_output",
  "call_id": "call_xxx",
  "output": [
    {"type": "input_text", "text": "Script completed..."},
    {"type": "input_image", "image_url": "data:image/png;base64,...", "detail": "high"}
  ]
}
```

既有普通消息形态保持不变：

```json
{
  "role": "user",
  "content": [
    {"type": "input_image", "image_url": "https://example.com/a.png"}
  ]
}
```

## 数据流

```text
/v1/responses 原始 input
  -> relay.PrepareRequestForSelectedChannel
  -> service.ApplyVisionAssist
  -> 按顶层输入项扫描 content 或工具 output
  -> 复用辅助渠道、缓存、去重、并发、重试、计费与日志
  -> 按输入项类型改写 Responses input
  -> 现有 Responses 原生转发或目标协议转换
  -> 主模型收到识图文本
```

## 抽取设计

- 继续由 `extractOpenAIResponsesVisionAssistImages` 解析 `dto.OpenAIResponsesRequest.Input`，不改变 `json.RawMessage` DTO 契约。
- 对普通输入项扫描 `content` 数组。
- 对 `type=function_call_output` 或 `type=custom_tool_call_output` 的输入项扫描 `output` 数组。
- 只识别 `type=input_image` 的内容块；工具名、`call_id` 和相邻调用项不参与触发判断。
- 图片 URL、detail 和 MIME 类型继续通过现有 `parseOpenAIResponsesVisionImage` 解析。
- `VisionAssistImage.MessageIndex` 继续表示顶层输入项索引，现有结果分组、排序、缓存和重复图片复用逻辑无需新增状态。

## 改写设计

### 普通消息

保持现有行为：在首个 `input_image` 前插入一条 `input_text`；`strip_image=true` 删除图片，`false` 保留图片。

### function_call_output

- 在原 `output` 数组首个 `input_image` 前插入识图 `input_text`。
- 保留所有非图片输出项及其顺序。
- 按 `strip_image` 配置删除或保留图片。
- 该结构可被 Responses 原生上游继续接收；现有 Chat、Claude 和 Gemini function output 转换也能保留工具结果内容。

### custom_tool_call_output

- 原 `output` 数组只执行图片删除/保留，非图片输出项及其顺序不变。
- 在该工具输出项之后插入一条普通 Responses 用户消息，`content` 仅包含识图 `input_text`。
- 原因：部分目标转换器按既有策略过滤自定义工具调用及输出；普通用户消息可以在不修改转换器策略的前提下保留识图结果。
- 同一工具输出无论包含一张还是多张图片，只插入一条汇总描述消息，沿用现有 `visionAssistText` 编号格式。

## 边界与错误

- `input` 不是数组、工具 `output` 不是数组、内容项不是对象或没有有效 `image_url` 时跳过，不触发辅助调用。
- 不含图片时不重新序列化 `Input`，保证原始 JSON 不变。
- 抽取阶段保持现有容错语义；改写阶段 JSON 解析或序列化错误继续交由现有 failure policy 处理。
- `strip_image=false` 时仍注入识图文本，但保留工具输出图片。
- `/v1/responses/compact`、请求体透传和视觉辅助递归调用仍由现有 relay 入口跳过。

## 修改边界

- `service/vision_assist.go`：扩展 Responses 工具输出图片抽取与分类型改写。
- `service/vision_assist_test.go`：补直接工具、自定义工具、配置分支、无图片和重复图片回归测试。
- 原则上不修改 DTO、relay 入口、渠道适配器、前端、数据库和配置。

## 兼容与回滚

- 老请求没有工具输出图片时行为不变。
- 普通 Responses 消息、OpenAI Chat 和 Claude Messages 继续走原分支。
- 回滚只需撤销 `service/vision_assist.go` 的工具输出分支及对应测试，不涉及迁移、配置清理或数据恢复。

## 验证重点

- 真实 Codex 两种请求结构都能进入 caller。
- 写回结果经过 `common.Marshal` 后仍是合法 Responses JSON。
- 自定义工具输出的识图普通消息紧邻原工具输出，且不会重复插入。
- 重复图片仍只产生一次未缓存辅助调用。
