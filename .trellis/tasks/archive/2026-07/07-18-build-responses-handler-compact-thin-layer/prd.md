# Responses Handler Compact 分支薄层化

## Goal

把 `relay/responses_handler.go` 中仍然偏厚的 Compact endpoint 校验、请求转换、临时计费快照恢复和审计 outcome 分支迁入 Responses Compact 专属文件，让主 Responses helper 只保留窄调用。

## Background

- 当前 `relay/responses_handler.go:28` 到 `relay/responses_handler.go:39` 直接在主 helper 中校验 Compact endpoint 支持的 API type。
- 当前 `relay/responses_handler.go:41` 到 `relay/responses_handler.go:62` 混合处理普通 Responses 与 Compaction 请求转换。
- 当前 `relay/responses_handler.go:148` 到 `relay/responses_handler.go:166` 在主 helper 中处理 Compact endpoint 的临时查价、快照恢复和结算。
- 当前 `relay/responses_handler.go:172` 到 `relay/responses_handler.go:180` 在普通文本结算路径中包含 Compact V2 audit outcome。
- 当前 `relay/responses_handler.go:186` 到 `relay/responses_handler.go:218` 定义 Compact 请求转换 helper。

## Requirements

- R1：新建 `relay/responses_compact_handler.go` 承载 Responses handler 内 Compact 专属逻辑。
- R2：`relay/responses_handler.go` 保留普通 Responses 主流程，Compact 相关代码改为少量函数调用。
- R3：保留现有行为：
  - Compact endpoint 只支持 OpenAI/Codex API type。
  - Compaction 请求转换字段集合不变。
  - Compact endpoint 临时调用 `ModelPriceHelper` 后必须在成功和失败路径恢复 `OriginModelName`、`PriceData` 和 `FrozenBillingModelName`。
  - Compact audit outcome 对 V2 stream incomplete / terminal event 的判断不变。
- R4：不改变 `ResponsesCompactPassthroughHelper` 的独立透传实现，不把 passthrough 和旧 handler 路径强行合并。
- R5：不得重写 adaptor 转换、disabled fields、Param Override、DoRequest/DoResponse 主顺序。

## Acceptance Criteria

- [ ] `relay/responses_compact_handler.go` 承载 Compact endpoint 校验、请求转换、Compact endpoint 结算和 audit outcome helper。
- [ ] `relay/responses_handler.go` 中 Compact 分支减少为窄调用，普通 Responses 主流程保持原顺序。
- [ ] `go test ./relay -run ResponsesCompact -count=1` 通过。
- [ ] `go test ./relay ./relay/helper ./service -count=1` 和 `git diff --check` 通过，或记录与本任务无关的既有失败。

## Out of Scope

- 不改 Compact passthrough 协议、路径矩阵、usage 解析或退款规则。
- 不改普通 Responses 请求转换、Param Override 或错误映射。
- 不新增 Compact 计费模型或工具价格。
