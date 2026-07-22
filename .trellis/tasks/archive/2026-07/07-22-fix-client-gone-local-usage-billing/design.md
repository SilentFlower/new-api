# 修复流式 client_gone 本地 usage 误计费设计

## Architecture Boundary

本任务采用薄层处理，边界放在 `service.PostTextConsumeQuota` 的文本计费收口附近。原因是所有文本渠道最终都会进入该函数完成额度结算、用户/渠道统计和消费日志写入；在这里处理可以覆盖 Claude、OpenAI 兼容、Gemini 等路径，避免在各 adapter 中重复判断。

实现建议：

- 在 `service` 包新增独立文件，例如 `text_quota_stream_guard.go`，放置私有 helper。
- `PostTextConsumeQuota` 只增加一处早期分派：
  - 识别 `stream client_gone + local usage`。
  - 命中后走零额结算和错误审计路径并 `return`。
- 不修改 `relay/channel/*` 适配器，不修改 cc2api。

## Decision Rules

命中跳过计费必须同时满足：

- `relayInfo != nil`
- `relayInfo.IsStream`
- `relayInfo.StreamStatus != nil`
- `relayInfo.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone`
- `common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens)`
- `originUsage != nil`

其中 `originUsage != nil` 保证当前路径确实拿到了一个 usage 对象，只是来源是本地估算；`ContextKeyLocalCountTokens` 是本地估算的明确标记。

不命中情况：

- 非流式请求。
- 流式正常结束、EOF、handler stop 或 scanner error。
- 客户端断开但没有本地估算标记。
- 上游返回可信 usage，不设置本地估算标记。

## Settlement Flow

命中跳过计费时：

1. 调用 `SettleBilling(ctx, relayInfo, 0)`，让预扣额度通过既有 BillingSession 流程退回或归零。
2. 不调用 `UpdateUserUsedQuotaAndRequestCount`。
3. 不调用 `UpdateChannelUsedQuota`。
4. 不调用 `RecordConsumeLog`，因此不会写 `quota_data`。
5. 按现有错误日志策略记录 `RecordErrorLog`，`quota=0`。

错误日志必须保留诊断字段：

- `stream_status`
- `usage_billing_path=local`
- `admin_info.local_count_tokens=true`
- `admin_info.billing_skipped_reason=client_gone_local_usage`
- `request_path`
- 渠道、模型、token、分组等既有字段

若现有全局错误日志开关关闭，应尊重现有策略；不得强制打开数据库错误日志。

## Compatibility

- 不新增数据库列，不触发迁移。
- 不改变价格、倍率、tiered billing 表达式和模型快照。
- 不改变已成功完成请求的计费。
- 不改变本地估算在非客户端断开场景下的既有行为。
- 保持 SQLite/MySQL/Postgres 兼容；本任务不写原生 SQL。

## Trade-Offs

- 不在 Claude adapter 里提前不返回 usage：这样避免只修 Claude，漏掉其他渠道未来出现同类本地估算断开路径。
- 不把所有 `client_gone` 免费：如果上游已经返回可信 usage，平台仍可按真实 usage 结算。
- 错误日志只做审计，不参与消费统计：避免 `quota_data` 和 dashboard 再次把异常计为消费。

## Rollback

代码回滚后行为恢复为旧逻辑。由于本任务不做迁移和数据结构变更，回滚不需要数据库操作。
