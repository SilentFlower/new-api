# 修复流式 client_gone 本地 usage 误计费实现计划

## Checklist

1. 读取相关定义，禁止猜字段：
   - `relay/common/stream_status.go`
   - `relay/common/relay_info.go`
   - `service/text_quota.go`
   - `service/usage_helpr.go`
   - `service/log_info_generate.go`
   - `model/log.go`
   - `dto/usage` 相关定义
2. 在 `service` 包新增薄层私有 helper：
   - 判断 `client_gone + local_count_tokens`。
   - 构造错误日志 other/admin_info。
   - 执行零额结算并记录审计。
3. 在 `PostTextConsumeQuota` 早期接入 helper，命中后立即 `return`。
4. 添加 focused Go 测试：
   - 命中跳过计费：用户 used quota、渠道 used quota、消费日志、`quota_data` 均不增加。
   - 命中时写错误审计日志或在错误日志关闭时不强制写。
   - `client_gone + 非 local usage` 仍走现有消费路径。
   - `local usage + 非 client_gone` 仍走现有消费路径。
5. 运行验证命令并修正失败。

## Validation Commands

优先运行：

```bash
go test ./service ./model -run 'ClientGone|TextQuota|ConsumeLog|QuotaData' -count=1
git diff --check
```

必要时扩大到：

```bash
go test ./service ./relay/helper ./relay/channel/claude -count=1
go test ./relay ./controller -run 'Relay|ErrorLog|TextQuota' -count=1
```

## Risk Points

- `RecordErrorLog` 本身不判断 `ErrorLogEnabled`；如果 service 层直接调用，必须显式遵守现有错误日志策略。
- `quota_data` 由 `RecordConsumeLog` 间接异步写入；命中跳过计费时不能调用 `RecordConsumeLog`。
- `SettleBilling(ctx, relayInfo, 0)` 必须仍被调用，避免预扣额度不退。
- Claude cache creation token 的统计口径有 5m/1h 与总字段差异，测试不能用错误的重复统计锁定行为。

## Thin-Layer Constraint

本任务只允许在计费收口增加薄层分派和私有辅助逻辑。禁止把判断复制到 `relay/channel/claude`、`relay/channel/gemini`、`relay/channel/openai` 等 adapter；adapter 继续只负责解析上游响应和填充 usage。
