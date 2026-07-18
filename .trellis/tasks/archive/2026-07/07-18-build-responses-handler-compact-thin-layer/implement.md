# Implement — Responses Handler Compact 分支薄层化

## Checklist

1. 读取 `relay/responses_handler.go`、`relay/responses_compact_passthrough.go`、`dto/openai_request.go`、`dto/openai_responses_compaction_request.go`、`relay/common/billing_model.go`。
2. 新建 `relay/responses_compact_handler.go`。
3. 迁移 Compact endpoint API type 校验和 request 转换 helper。
4. 迁移 Compact endpoint 临时查价/恢复/结算逻辑。
5. 迁移 audit outcome 计算逻辑。
6. 清理 `relay/responses_handler.go` import，gofmt。
7. 执行定向测试。

## Validation

- `go test ./relay -run ResponsesCompact -count=1`
- `go test ./relay ./relay/helper ./service -count=1`
- `git diff --check`

## Risk

- 风险点：临时计费快照恢复漏掉错误路径。
- 控制：在新 helper 内集中保存和 defer/显式恢复，结合 `relay-billing-model.md` 契约复核。
