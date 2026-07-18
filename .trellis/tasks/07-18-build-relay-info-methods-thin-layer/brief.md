# Brief — RelayInfo Build 方法薄层化

## Goal

- 把 RelayInfo 的 Responses Compact 状态方法迁入领域文件。

## Scope

- 新建 `relay/common/responses_compact.go`。
- 迁移 `IsResponsesCompact`、`IsResponsesCompactV2`、`UsesResponsesCompactEndpoint`、`UsesUpstreamStream`。
- `relay/common/relay_info.go` 保留字段和初始化逻辑。

## Non-Goals

- 不移动字段，不修改计费模型方法，不改变调用方。

## Key Context

- 当前厚点：`relay/common/relay_info.go:278`。
- 计费方法已在 `relay/common/billing_model.go`，本任务不重复治理。

## Acceptance

- `go test ./relay/common -run 'ResponsesCompact|BillingModel' -count=1` 通过。
- 4 个方法签名和行为不变。

## Next Step

- 迁移方法并清理 `relay_info.go` import。
