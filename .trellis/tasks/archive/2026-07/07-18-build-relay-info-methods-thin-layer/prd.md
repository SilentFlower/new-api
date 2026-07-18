# RelayInfo Build 方法薄层化

## Goal

把 `RelayInfo` 上的 Responses Compact 状态判断方法从核心结构文件迁出到领域文件，保持方法签名和行为不变，让 `relay/common/relay_info.go` 更接近核心数据结构定义。

## Background

- 当前 `relay/common/relay_info.go:126` 定义 `ResponsesCompactMode` 字段。
- 当前 `relay/common/relay_info.go:278` 到 `relay/common/relay_info.go:305` 定义 `IsResponsesCompact`、`IsResponsesCompactV2`、`UsesResponsesCompactEndpoint`、`UsesUpstreamStream`。
- 计费模型方法已在 `relay/common/billing_model.go` 中，符合 `.trellis/spec/backend/relay-billing-model.md` 的文件所有权要求。
- 仍需把 Compact 状态方法同样迁出，减少 `relay_info.go` 的 build 专属方法密度。

## Requirements

- R1：新建 `relay/common/responses_compact.go` 承载 `RelayInfo` 的 Responses Compact 状态判断方法。
- R2：保留以下方法签名和行为：
  - `func (info *RelayInfo) IsResponsesCompact() bool`
  - `func (info *RelayInfo) IsResponsesCompactV2() bool`
  - `func (info *RelayInfo) UsesResponsesCompactEndpoint() bool`
  - `func (info *RelayInfo) UsesUpstreamStream() bool`
- R3：`relay/common/relay_info.go` 只保留 `RelayInfo` 字段、初始化、通用调试和生成逻辑，不承载 Compact 状态方法。
- R4：不直接读写或移动 `ResolvedBillingModelName` 字段；计费模型方法保持在 `billing_model.go`。
- R5：不改变 Compact V1 path、V1 body bridge、V2 HTTP/WebSocket 或 legacy relay mode 判断。

## Acceptance Criteria

- [ ] `relay/common/responses_compact.go` 包含 4 个方法和完整注释。
- [ ] `relay/common/relay_info.go` 不再定义上述 4 个 Compact 状态方法。
- [ ] `go test ./relay/common -run 'ResponsesCompact|BillingModel' -count=1` 通过。
- [ ] `go test ./relay/common ./relay/helper ./relay -count=1` 和 `git diff --check` 通过，或记录与本任务无关的既有失败。

## Out of Scope

- 不移动 `RelayInfo` 字段。
- 不移动 `billing_model.go` 已有计费方法。
- 不改变任何调用方。
