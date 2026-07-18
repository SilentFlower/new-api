# Implement — Responses Compact 日志审计薄层化

## Checklist

1. 读取 `service/log_info_generate.go` 和 `service/responses_compact_audit_test.go`，确认现有审计行为。
2. 新建 `service/responses_compact_audit.go`，原样迁移 Compact 审计实现。
3. 从 `service/log_info_generate.go` 删除迁出的 Compact 审计代码和未使用 import。
4. 运行 gofmt 仅格式化涉及的 Go 文件。
5. 执行定向测试与 diff 检查。

## Validation

- `go test ./service -run ResponsesCompactAudit -count=1`
- `go test ./service -count=1`
- `git diff --check`

## Risk

- 风险点：审计写入位置变化后不进入最终日志。
- 控制：保持同包同函数签名，依旧通过 `constant.ContextKeyLogOther` 写入，使用既有测试回归。
