# 修复流式 client_gone 本地 usage 误计费

## Goal

修复流式请求在客户端断开后，因 new-api 使用本地 token 估算 usage 而被记录为正常消费并扣费的问题。修复必须是薄层处理：在文本计费收口统一防护，不把判断分散到 Claude、Gemini、OpenAI 兼容等各渠道适配器。

## Background

- 生产数据已确认 `2026-07-21` 有 `81` 条 `client_gone + local_count_tokens` 被错误记为 `type=2` 消费，合计冲正 `371,219,386` quota。
- `service.ResponseText2Usage` 会设置 `ContextKeyLocalCountTokens=true`，并用本地估算填充 prompt/completion usage。证据：`service/usage_helpr.go:22`。
- Claude 流式在 `CompletionTokens==0` 或未收到完成事件时会调用 `ResponseText2Usage` 补 usage。证据：`relay/channel/claude/relay-claude.go:133`。
- 流式扫描器在客户端断开时将 `StreamStatus.EndReason` 标记为 `client_gone`。证据：`relay/helper/stream_scanner.go:302`。
- 文本计费收口 `PostTextConsumeQuota` 统一负责 usage 计费、`SettleBilling`、用户/渠道 used quota 更新和消费日志写入。证据：`service/text_quota.go:347`、`service/text_quota.go:391`、`service/text_quota.go:399`、`service/text_quota.go:492`。
- 消费日志当前会写入 `usage_billing_path=local` 与 `stream_status`，说明防护条件已经具备审计信号。证据：`service/text_quota.go:427`、`service/log_info_generate.go:139`。

## Requirements

- R1：当流式请求满足以下全部条件时，不得产生正常消费扣费：
  - `relayInfo.IsStream == true`
  - `relayInfo.StreamStatus.EndReason == client_gone`
  - `ContextKeyLocalCountTokens == true`
  - 当前 usage 是本地估算路径，不是可信上游 usage
- R2：R1 命中时必须把实际结算额度设为 `0`，并让已有 BillingSession/预扣逻辑完成退款或零额结算；不得更新 `users.used_quota`、`channels.used_quota`、`tokens.used_quota/remain_quota` 的消费方向统计。
- R3：R1 命中时不得写 `type=2` 消费日志，也不得写入 `quota_data` 消费统计。
- R4：R1 命中时应保留可审计痕迹：
  - 如果现有错误日志策略允许记录错误日志，则写 `type=5`，`quota=0`。
  - `other.admin_info.billing_skipped_reason` 使用稳定值 `client_gone_local_usage`。
  - 保留 `stream_status`、`usage_billing_path`、模型、token、渠道、请求路径等已有诊断字段。
- R5：不得把所有 `client_gone` 一概免费。若上游已返回可信 usage，本任务不改变现有结算语义。
- R6：不得在各渠道适配器重复判断；只允许在 `PostTextConsumeQuota` 附近增加薄层分派，复杂逻辑放入独立 service 文件或小型私有 helper。
- R7：不修改 cc2api，不修改模型价格，不新增数据库字段或迁移。

## Acceptance Criteria

- [ ] 新增/调整测试覆盖 `client_gone + local_count_tokens` 时不会写消费日志、不会更新 used quota、不会写 `quota_data`，且结算额度为 `0`。
- [ ] 测试覆盖 `client_gone + 可信上游 usage` 仍按现有逻辑结算，避免扩大免费范围。
- [ ] 测试覆盖非 `client_gone` 的本地估算 usage 仍保持现有行为。
- [ ] 错误日志审计字段包含 `billing_skipped_reason=client_gone_local_usage`，且普通用户日志清洗不暴露 `admin_info`。
- [ ] 代码改动集中在 service 计费薄层和测试，不改各渠道 adapter 的业务分支。
- [ ] 通过定向 Go 测试和 `git diff --check`。

## Out Of Scope

- 历史生产数据修复已经单独完成，不纳入本代码任务。
- cc2api 1M 上下文构造、模型上限策略、用户调用习惯治理不纳入本任务。
- 不新增后台巡检任务；可在交付说明中给出人工监控 SQL。
