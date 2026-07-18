# Alpha Search 请求校验薄层化

## Goal

把 Alpha Search 专属请求校验从通用请求校验文件中迁出，形成独立文件，保持原始 body 透明转发边界和计费安全校验不变。

## Background

- 当前 `relay/helper/valid_request.go:42` 在通用 switch 中调用 `GetAndValidateAlphaSearchRequest`。
- 当前 `relay/helper/valid_request.go:176` 到 `relay/helper/valid_request.go:209` 实现 Alpha Search 校验。
- 当前 `dto/alpha_search_request.go:11` 定义最小 DTO：`ID`、`Model`、`MaxOutputTokens`。
- `relay/helper/alpha_search_request_test.go` 已覆盖模型缺失、重复 model、显式零值和 `max_output_tokens` 上限。

## Requirements

- R1：新建 `relay/helper/alpha_search_request.go` 承载 `GetAndValidateAlphaSearchRequest` 完整实现。
- R2：`relay/helper/valid_request.go` 只保留 format switch 对该函数的调用，以及通用 max token 上限工具。
- R3：继续使用 `common.UnmarshalBodyReusable` 解析最小 DTO；继续从 `common.GetBodyStorage` 读取原始 body 检查重复顶层 `model`。
- R4：`model` 必须是唯一非空字符串；`max_output_tokens` 必须复用现有 `maxTokensLimit` / `exceedsMaxTokensLimit`。
- R5：不得引入封闭 DTO 重组上游 body；未知字段、显式 `0` / `false` 的透传行为属于 Alpha Search handler 既有契约，校验层不得破坏。

## Acceptance Criteria

- [ ] `relay/helper/alpha_search_request.go` 包含 Alpha Search 校验逻辑和导出函数注释。
- [ ] `relay/helper/valid_request.go` 不再 import `gjson`，只在 switch 中调用 Alpha Search 校验。
- [ ] `go test ./relay/helper -run AlphaSearch -count=1` 通过。
- [ ] `go test ./relay/helper -count=1` 和 `git diff --check` 通过，或记录与本任务无关的既有失败。

## Out of Scope

- 不修改 Alpha Search handler 的上游 URL、计费、重试或日志实现。
- 不改变错误文案。
- 不扩展请求 DTO 字段。
