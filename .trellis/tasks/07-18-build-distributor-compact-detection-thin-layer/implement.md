# Implement — Distributor Compact 检测薄层化

## Checklist

1. 读取 `middleware/distributor.go`、`relay/helper/responses_compact.go`、`middleware/distributor_responses_compact_test.go`。
2. 新建 `middleware/responses_compact_detection.go`。
3. 从 `getModelRequest` 移出 body 读取和 detector 参数拼装，只保留窄调用。
4. 清理 `middleware/distributor.go` import。
5. gofmt 涉及文件。
6. 执行定向测试。

## Validation

- `go test ./middleware -run ResponsesCompact -count=1`
- `go test ./middleware -count=1`
- `git diff --check`

## Risk

- 风险点：迁移后 body storage 读取失败路径或 compact path body 读取优化发生变化。
- 控制：保持函数体等价，测试覆盖 Compact mode 上下文与基础模型分发。
