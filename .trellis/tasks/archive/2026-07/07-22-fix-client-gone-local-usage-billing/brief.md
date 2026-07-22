# Brief — 修复流式 client_gone 本地 usage 误计费

## Goal

- 修复流式请求在客户端断开后，new-api 因本地 token 估算 usage 而记录正常消费并扣费的问题。

## Scope

- 在 `service.PostTextConsumeQuota` 附近做薄层统一防护。
- 命中 `stream client_gone + local_count_tokens + 本地 usage` 时，按 `0` 额度结算并返回，避免写 `type=2` 消费日志和 `quota_data`。
- 按现有错误日志策略记录 `type=5` 审计日志，写入 `admin_info.billing_skipped_reason=client_gone_local_usage`。
- 添加 focused Go 测试覆盖跳过计费、可信 usage 仍计费、非 client_gone 本地 usage 仍保留现有行为。

## Non-Goals

- 不处理历史生产数据；历史数据已单独冲正。
- 不修改 cc2api。
- 不修改模型价格、倍率、tiered billing、数据库结构或迁移。
- 不在 Claude、Gemini、OpenAI 兼容等各渠道 adapter 分散增加判断。
- 不把所有 `client_gone` 一概免费。

## Key Context

- `service.ResponseText2Usage` 会设置 `ContextKeyLocalCountTokens=true` 并生成本地 usage。
- `relay/helper/stream_scanner.go` 会在客户端断开时设置 `StreamStatus.EndReason=client_gone`。
- `PostTextConsumeQuota` 是文本 usage 结算、used quota、`SettleBilling` 和消费日志的统一收口。
- 直接调用 `RecordErrorLog` 前必须尊重现有 `ErrorLogEnabled` 策略。
- `quota_data` 由 `RecordConsumeLog` 间接写入；跳过计费路径不能调用 `RecordConsumeLog`。

## Acceptance

- `client_gone + local_count_tokens` 不更新用户/渠道/token used quota，不写消费日志，不写 `quota_data`，结算额度为 `0`。
- `client_gone + 可信上游 usage` 仍按现有逻辑结算。
- `local_count_tokens + 非 client_gone` 仍按现有逻辑结算。
- 错误审计字段稳定，普通用户日志清洗不暴露 `admin_info`。
- 代码改动集中在 service 计费薄层和测试。
- 定向 Go 测试和 `git diff --check` 通过。

## Next Step

- 用户确认 planning artifacts 和 brief 后，运行 `task.py start .trellis/tasks/07-22-fix-client-gone-local-usage-billing`，再进入实现。
