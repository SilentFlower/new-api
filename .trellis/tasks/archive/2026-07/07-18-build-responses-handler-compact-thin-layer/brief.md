# Brief — Responses Handler Compact 分支薄层化

## Goal

- 把 `relay/responses_handler.go` 内 Compact 专属分支迁入专属文件。

## Scope

- 新建 `relay/responses_compact_handler.go`。
- 迁移 Compact endpoint API type 校验、请求转换、临时计费快照恢复、结算和 audit outcome。
- 主 Responses helper 保留普通主流程和窄调用。

## Non-Goals

- 不修改 Compact passthrough、普通 Responses 主顺序、Param Override、disabled fields 或错误映射。

## Key Context

- 当前厚点：`relay/responses_handler.go:28`、`:148`、`:186`。
- 计费恢复契约：`.trellis/spec/backend/relay-billing-model.md`。
- Compact 行为契约：`.trellis/spec/backend/relay-alpha-search-compact.md`。

## Acceptance

- `go test ./relay -run ResponsesCompact -count=1` 通过。
- 主 handler 中 Compact 逻辑只剩窄 helper 调用。
- 临时查价成功/失败均恢复冻结计费模型。

## Next Step

- 抽出 Compact handler helper，并保持普通 Responses 执行顺序不变。
