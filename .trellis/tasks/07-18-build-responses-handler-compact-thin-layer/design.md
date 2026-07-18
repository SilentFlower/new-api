# Design — Responses Handler Compact 分支薄层化

## New File

- `relay/responses_compact_handler.go`
  - `validateResponsesCompactEndpoint(info *relaycommon.RelayInfo) *types.NewAPIError`
  - `responsesRequestForHandler(info *relaycommon.RelayInfo) (*dto.OpenAIResponsesRequest, *types.NewAPIError)` 或等价窄 helper
  - `responsesRequestFromCompaction` / `responsesRequestForCompaction`
  - `postResponsesCompactEndpointQuota(c, info, usageDto) *types.NewAPIError`
  - `responsesCompactAuditOutcome(info *relaycommon.RelayInfo) string`

函数可保持私有；只有主 handler 需要同包调用。

## Existing File Thin Point

- `relay/responses_handler.go`
  - 初始化 channel meta 后调用 Compact endpoint 校验 helper。
  - 通过 helper 获取普通化后的 request。
  - Compact endpoint 结算使用 helper，主文件不直接保存/恢复计费快照。
  - audit outcome 通过 helper 计算。

## Contracts

- `ResponsesHelper` 对普通 Responses 的执行顺序保持：模型映射 -> adaptor 初始化 -> request body 构造 -> DoRequest -> status mapping -> DoResponse -> quota。
- Compact endpoint 的临时查价恢复必须覆盖错误返回和成功返回。
- `service.SetResponsesCompactAudit` 调用语义不变。
- JSON marshal 继续使用 `common.Marshal`。

## Rollback

删除 `relay/responses_compact_handler.go`，把 helper 内容恢复到 `relay/responses_handler.go` 对应位置。

## Upstream Sync Review Point

上游若改动 `ResponsesHelper` 普通主流程，只复核薄调用的插入点是否仍位于相同阶段，不为复用重排上游逻辑。
