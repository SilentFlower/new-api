# Implement — RelayInfo Build 方法薄层化

## Checklist

1. 读取 `relay/common/relay_info.go`、`relay/common/billing_model.go`、`relay/constant/responses_compact.go`。
2. 新建 `relay/common/responses_compact.go` 并迁移 4 个方法。
3. 从 `relay/common/relay_info.go` 删除迁出方法和未使用 import。
4. gofmt 涉及 Go 文件。
5. 执行定向测试。

## Validation

- `go test ./relay/common -run 'ResponsesCompact|BillingModel' -count=1`
- `go test ./relay/common ./relay/helper ./relay -count=1`
- `git diff --check`

## Risk

- 风险点：`relay_info.go` import 清理误删其他常量依赖。
- 控制：迁移后通过编译和 relay/common 测试确认。
