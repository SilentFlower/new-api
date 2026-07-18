# Brief — Responses Compact 日志审计薄层化

## Goal

- 把 Responses Compact 管理员审计逻辑迁出通用日志生成文件。

## Scope

- 新建 `service/responses_compact_audit.go`。
- 保持 `ClearResponsesCompactAudit`、`SetResponsesCompactAudit` 签名和行为。
- 清理 `service/log_info_generate.go` 中 Compact 专属实现和多余 import。

## Non-Goals

- 不改变消费日志结构、计费逻辑或日志可见性规则。

## Key Context

- 当前厚点：`service/log_info_generate.go:20`、`:54`、`:75`、`:124`。
- 审计必须继续写入 `other.admin_info.responses_compact`，且不得记录 body、query、凭证或完整 URL。

## Acceptance

- 定向 service 测试通过。
- 原通用日志文件只保留通用职责。
- 清理上一轮 WS turn 时不删除其他 admin_info。

## Next Step

- 迁移代码到新文件并运行 `go test ./service -run ResponsesCompactAudit -count=1`。
