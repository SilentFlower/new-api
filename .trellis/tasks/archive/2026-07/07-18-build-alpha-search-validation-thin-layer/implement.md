# Implement — Alpha Search 请求校验薄层化

## Checklist

1. 读取 `dto/alpha_search_request.go`、`relay/helper/valid_request.go`、`relay/helper/alpha_search_request_test.go`。
2. 新建 `relay/helper/alpha_search_request.go` 并迁移 `GetAndValidateAlphaSearchRequest`。
3. 清理 `relay/helper/valid_request.go` 中迁出的函数和 import。
4. gofmt 涉及 Go 文件。
5. 执行定向测试。

## Validation

- `go test ./relay/helper -run AlphaSearch -count=1`
- `go test ./relay/helper -count=1`
- `git diff --check`

## Risk

- 风险点：迁移后 body storage seek/read 语义变化。
- 控制：原样迁移函数，同包复用原上限工具，运行既有 Alpha Search request 测试。
