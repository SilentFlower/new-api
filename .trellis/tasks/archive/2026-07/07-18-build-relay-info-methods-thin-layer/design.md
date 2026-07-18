# Design — RelayInfo Build 方法薄层化

## New File

- `relay/common/responses_compact.go`
  - 承载 `RelayInfo` 的 Compact 状态判断方法。
  - import `relay/constant` 仅用于 legacy relay mode fallback。

## Existing File Thin Point

- `relay/common/relay_info.go`
  - 删除 4 个 Compact 状态方法。
  - 保留 `ResponsesCompactMode`、`ResponsesClientStream`、`OriginModelName` 等结构字段。
  - 保留 `GenRelayInfo` 中读取上下文和填充字段的逻辑。

## Contracts

- 调用方仍通过同一个 receiver 方法调用；移动文件不改变包 API。
- `UsesUpstreamStream` 仍依赖 `IsStream && !UsesResponsesCompactEndpoint()`。
- legacy `RelayModeResponsesCompact` fallback 继续保留。

## Rollback

删除 `relay/common/responses_compact.go`，把 4 个方法恢复到 `relay/common/relay_info.go`。

## Upstream Sync Review Point

上游若改动 `RelayInfo` 字段或 Responses relay mode 常量，只复核新文件方法是否仍引用正确字段和常量。
