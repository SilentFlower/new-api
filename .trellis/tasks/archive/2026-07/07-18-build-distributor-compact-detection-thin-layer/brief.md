# Brief — Distributor Compact 检测薄层化

## Goal

- 把 Responses Compact mode 检测从 Distributor 主函数迁入独立 middleware 文件。

## Scope

- 新建 `middleware/responses_compact_detection.go`。
- `middleware/distributor.go` 只保留 `/v1/responses` 检测入口调用。
- 保持 `ContextKeyResponsesCompactMode` 写入和错误路径。

## Non-Goals

- 不修改检测算法、选渠、亲和性、Token 权限或能力门禁。

## Key Context

- 当前厚点：`middleware/distributor.go:422`。
- 检测函数：`relay/helper/responses_compact.go:29`。
- 相关测试：`middleware/distributor_responses_compact_test.go`。

## Acceptance

- `go test ./middleware -run ResponsesCompact -count=1` 通过。
- Distributor 主文件不再承载 Compact 检测细节。

## Next Step

- 抽出 `detectAndStoreResponsesCompactMode` 并保留窄调用。
