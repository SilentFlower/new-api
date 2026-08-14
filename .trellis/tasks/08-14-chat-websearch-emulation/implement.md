# 扩展 WebSearch 模拟到 Chat Completions 实施计划

## Implementation Steps

1. 在 `relay/websearch` 增加 Chat 协议辅助逻辑：
   - 识别 `web_search_options` 且无普通 `tools` / `functions` 的纯搜索请求。
   - 从最后一条 user 消息提取字符串或文本块查询。
   - 构造非流式 Chat Completions 响应，并复用现有文本摘要与 usage 估算。
2. 提取 Claude 与 Chat 可复用的渠道 WebSearch provider 执行逻辑：
   - 配置归一化与校验。
   - 代理 Client、30 秒超时、provider 创建与调用。
   - 400/502 错误映射和敏感信息边界。
3. 新增 Chat WebSearch 本地 handler：
   - 模型名、响应 ID、Token 估算。
   - 非流式 JSON 响应。
   - Chat SSE 起始、文本、stop、可选 usage 和 `[DONE]`。
   - 成功后的单次 WebSearch 工具计数与文本结算。
4. 在 `TextHelper` 的 adaptor 初始化后、Chat-to-Responses 与上游 body 构造前接入纯 WebSearch 短路。
5. 保持 Claude 协议响应构造不变，并让现有 Claude 模拟复用通用 provider 执行逻辑。
6. 增加聚焦回归测试，覆盖触发、透传、查询提取、响应、流式、请求不变和计费一次性。
7. 根据实现后的稳定契约更新 `.trellis/spec/backend/relay-websearch-emulation.md`，补充 Chat Completions 模拟边界。

## Likely Files

- `relay/compatible_handler.go`
- `relay/chat_websearch_emulation.go`
- `relay/claude_websearch_emulation.go`
- `relay/websearch_emulation.go`
- `relay/websearch/chat.go`
- `relay/websearch/chat_test.go`
- `relay/claude_handler_test.go` 或新的 relay 聚焦测试文件
- `.trellis/spec/backend/relay-websearch-emulation.md`

实际实现以代码定义和最小改动为准，不为匹配文件清单强制拆分单调用 helper。

## Validation

1. `gofmt` 格式化改动 Go 文件。
2. `go test ./relay/websearch ./relay ./service`
3. `go test ./...`
4. 若实现触及 `relaykit/` 公共 API，额外运行 `cd relaykit && GOWORK=off go build ./...`；按当前设计不应触及。

## Risk Points

- 短路位置过晚会导致 Chat-to-Responses 或参数覆盖先修改请求。
- 对空 `functions` 的识别不准确可能误判混合工具请求。
- 同时写入现有上下文工具计数和 Responses 工具计数会重复收费。
- 流式响应缺少 stop、usage 或 `[DONE]` 会导致客户端等待或解析失败。
- 通用 provider 调用提取可能改变 Claude 当前错误状态码、跳过重试或响应行为。

## Pre-Start Checks

- PRD、设计和实施计划与“纯搜索 + 文本摘要”决策一致。
- `implement.jsonl` 与 `check.jsonl` 至少包含 WebSearch 模拟规范。
- 启动实现前展示并获得最新 `brief.md` 的明确批准。
