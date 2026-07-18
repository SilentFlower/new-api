# Responses Compact 日志审计薄层化

## Goal

把 Responses Compact 管理员审计逻辑从通用日志生成文件中迁出，形成独立审计文件，保持日志合并、字段隔离和安全行为不变。

## Background

- 当前 `service/log_info_generate.go:20` 定义 `responsesCompactAuditKey`。
- 当前 `service/log_info_generate.go:54` 定义 `ClearResponsesCompactAudit`。
- 当前 `service/log_info_generate.go:75` 定义 `SetResponsesCompactAudit`。
- 当前 `service/log_info_generate.go:124` 定义 `responsesCompactAuditPath`。
- 这些逻辑属于 Responses Compact build 定制审计，不应继续占用通用日志生成文件。

## Requirements

- R1：新建 `service/responses_compact_audit.go` 承载 Responses Compact 审计 key、清理、写入和路径提取逻辑。
- R2：保留 `ClearResponsesCompactAudit(ctx *gin.Context)` 与 `SetResponsesCompactAudit(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, outcome string)` 的函数签名和行为。
- R3：`service/log_info_generate.go` 只保留通用日志合并、quota saturation、普通 other 字段生成逻辑，不承载 Compact 专属审计实现。
- R4：审计信息仍只写入 `other.admin_info.responses_compact`；清理上一轮 WS turn 时只能删除该 key，不能删除 `quota_saturation` 等其他管理员字段。
- R5：审计仍不得记录请求 body、响应 body、query 凭证、完整 URL 或客户端认证信息。

## Acceptance Criteria

- [ ] `service/log_info_generate.go` 删除 Responses Compact 专属审计实现和不再需要的 import。
- [ ] `service/responses_compact_audit.go` 包含完整审计实现，导出函数注释完整。
- [ ] 既有调用方无需改函数名即可编译通过。
- [ ] `go test ./service -run ResponsesCompactAudit -count=1` 通过。
- [ ] `go test ./service -count=1` 和 `git diff --check` 通过，或记录与本任务无关的既有失败。

## Out of Scope

- 不改变消费日志结构。
- 不改变 admin/non-admin 日志可见性过滤规则。
- 不修改 Responses Compact 计费或透传逻辑。
