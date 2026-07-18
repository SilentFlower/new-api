# Brief — Alpha Search 请求校验薄层化

## Goal

- 把 Alpha Search 专属校验迁入独立 helper 文件。

## Scope

- 新建 `relay/helper/alpha_search_request.go`。
- 保持 `GetAndValidateAlphaSearchRequest` 签名、错误行为和 max token 上限校验。
- `relay/helper/valid_request.go` 只保留 format switch 调用。

## Non-Goals

- 不修改 Alpha Search handler、计费、上游 URL、重试或 DTO 字段。

## Key Context

- 当前厚点：`relay/helper/valid_request.go:176`。
- DTO：`dto/alpha_search_request.go:11`。
- 既有测试：`relay/helper/alpha_search_request_test.go`。

## Acceptance

- `go test ./relay/helper -run AlphaSearch -count=1` 通过。
- `valid_request.go` 不再承载 Alpha Search 具体校验实现。

## Next Step

- 原样迁移函数并清理 import。
