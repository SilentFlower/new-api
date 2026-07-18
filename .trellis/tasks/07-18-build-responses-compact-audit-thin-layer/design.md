# Design — Responses Compact 日志审计薄层化

## New File

- `service/responses_compact_audit.go`
  - 定义 `responsesCompactAuditKey`。
  - 实现 `ClearResponsesCompactAudit`。
  - 实现 `SetResponsesCompactAudit`。
  - 实现私有 `responsesCompactAuditPath`。

## Existing File Thin Point

- `service/log_info_generate.go`
  - 删除 Compact 审计实现。
  - 保留 `MergeContextLogOther`、`attachQuotaSaturation*`、`GenerateTextOtherInfo` 等通用日志能力。
  - 移除迁出后不再使用的 `net/url` import。

## Contracts

- `SetResponsesCompactAudit` 的输入、输出和上下文 key 不变。
- `ClearResponsesCompactAudit` 只清理 `admin_info.responses_compact`。
- `MergeContextLogOther` 继续合并所有上下文 other 字段，确保新审计文件写入的数据仍进入最终日志。

## Rollback

删除 `service/responses_compact_audit.go`，把迁出的代码放回 `service/log_info_generate.go`，恢复 import。

## Upstream Sync Review Point

如果上游改动 `service/log_info_generate.go` 的 other/admin_info 合并逻辑，只复核 `MergeContextLogOther` 与新审计文件写入结构是否仍兼容。
