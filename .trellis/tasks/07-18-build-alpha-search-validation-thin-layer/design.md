# Design — Alpha Search 请求校验薄层化

## New File

- `relay/helper/alpha_search_request.go`
  - 实现 `GetAndValidateAlphaSearchRequest`。
  - 独立 import `gjson`。
  - 复用同包 `exceedsMaxTokensLimit`。

## Existing File Thin Point

- `relay/helper/valid_request.go`
  - `GetAndValidateRequest` switch 仍在 `RelayFormatOpenAIAlphaSearch` 分支调用 `GetAndValidateAlphaSearchRequest`。
  - 移除 Alpha Search 具体实现和未使用 `gjson` import。

## Contracts

- `GetAndValidateAlphaSearchRequest` 函数签名不变。
- 顶层 `model` 重复检测仍基于原始 body。
- `MaxOutputTokens *uint` 的上限校验继续使用统一 max tokens 保护，避免计费乘数溢出。

## Rollback

删除新文件，把函数实现放回 `relay/helper/valid_request.go`，恢复 import。

## Upstream Sync Review Point

上游若改动 `valid_request.go` 的 format switch 或 max token 上限工具，只复核 Alpha Search 薄调用和同包工具复用是否仍成立。
