# Distributor Compact 检测薄层化

## Goal

把 Responses Compact mode 检测从 Distributor 主模型解析函数中收敛到独立 middleware 文件，让 `getModelRequest` 只保留一次窄调用。

## Background

- 当前 `middleware/distributor.go:422` 到 `middleware/distributor.go:442` 在 `getModelRequest` 末尾直接读取 body、调用 `relayhelper.DetectResponsesCompactMode` 并写入 `ContextKeyResponsesCompactMode`。
- `relay/helper/responses_compact.go:29` 已有独立检测函数。
- `middleware/distributor_responses_compact_test.go` 已覆盖分发层使用基础模型和 Compact mode 上下文行为。

## Requirements

- R1：新建 `middleware/responses_compact_detection.go` 承载 Distributor 层 Compact mode 检测和上下文写入。
- R2：`middleware/distributor.go` 中只保留 `if strings.HasPrefix(... "/v1/responses") { ... }` 级别的薄调用，不直接出现 body 读取和 `DetectResponsesCompactMode` 参数拼装细节。
- R3：`POST /v1/responses/compact` 仍不需要读取 body 即识别 V1 path；裸 `/v1/responses` 仍读取原始 body 判断 compaction trigger。
- R4：继续把 mode 写入 `constant.ContextKeyResponsesCompactMode`，供 `relay/common.GenRelayInfo` 读取。
- R5：不得改变普通 Responses、非 Responses 路径、音频/图片/视频/PG 模型解析逻辑。

## Acceptance Criteria

- [ ] `middleware/responses_compact_detection.go` 包含检测和上下文写入函数，注释说明边界。
- [ ] `middleware/distributor.go` 不再直接 import `relay/helper` 仅用于 Compact 检测。
- [ ] `go test ./middleware -run ResponsesCompact -count=1` 通过。
- [ ] `go test ./middleware -count=1` 和 `git diff --check` 通过，或记录与本任务无关的既有失败。

## Out of Scope

- 不改 `relay/helper.DetectResponsesCompactMode` 的检测算法。
- 不改 HTTP 分发选渠、亲和性、Token 权限或能力门禁。
- 不抽取/重写整个 `Distribute`。
