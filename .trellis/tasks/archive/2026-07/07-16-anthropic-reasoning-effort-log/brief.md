# Brief — Anthropic Reasoning Effort 日志适配

## Goal

- 让 Anthropic/Claude 最终请求中明确存在的 `output_config.effort` 在现有消费日志详情中正常显示。

## Scope

- 从 Anthropic 最终请求格式读取 `output_config.effort`，回填 `relayInfo.ReasoningEffort`。
- 参数覆盖修改该字段时，记录覆盖后的字符串值，例如 `xhigh`。
- 复用现有 `other.reasoning_effort` 日志字段和前端展示。
- 补充字段存在、覆盖后改变和字段缺失三个确定性回归场景。

## Non-Goals

- 不从 `thinking.budget_tokens` 推断 effort 档位。
- 不统一 OpenAI Chat、OpenAI Responses、Gemini 或其他渠道的 effort 日志语义。
- 不扩大参数覆盖审计白名单，不修改前端、计费、渠道配置或数据库结构。

## Key Context

- `service/log_info_generate.go:198` 已负责把非空 `relayInfo.ReasoningEffort` 写入 `other.reasoning_effort`。
- `relay/channel/claude/adaptor.go:28` 当前没有从 Claude 请求回填该字段。
- `relay/claude_handler.go:193` 在请求序列化后执行参数覆盖；采集时必须反映覆盖后的 `output_config.effort`。
- 日志只能记录 effort 字符串，不得记录完整请求体、对话或凭证；JSON 编解码使用项目 `common` 封装。

## Acceptance

- `output_config.effort: "high"` 时日志详情显示 `high`。
- 参数覆盖把值从 `max` 改为 `xhigh` 时日志详情显示 `xhigh`。
- 最终请求没有 `output_config.effort` 时不写日志字段，也不根据 thinking budget 猜测。
- 不修改前端即可展示字符串值；相关定向测试、受影响包测试和 `git diff --check` 通过。

## Next Step

- 用户确认本摘要后启动任务，并进入 `trellis-route(implement)` 选择实现执行方式。
