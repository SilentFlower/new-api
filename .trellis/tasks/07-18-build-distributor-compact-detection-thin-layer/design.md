# Design — Distributor Compact 检测薄层化

## New File

- `middleware/responses_compact_detection.go`
  - 实现 `detectAndStoreResponsesCompactMode(c *gin.Context) error`。
  - 内部决定是否读取 `common.GetBodyStorage`。
  - 调用 `relayhelper.DetectResponsesCompactMode`。
  - 写入 `constant.ContextKeyResponsesCompactMode`。

## Existing File Thin Point

- `middleware/distributor.go`
  - 在 `getModelRequest` 末尾保留一次窄调用。
  - 错误沿用原逻辑向上返回。
  - 保留其他模型解析分支原样。

## Contracts

- Compact mode 检测发生在模型解析后、返回 `ModelRequest` 前，与当前上下文写入时机一致。
- V1 path 不读取 body，裸 Responses 读取 body。
- 不改变 `shouldSelectChannel` 和 `modelRequest.Model`。

## Rollback

删除新文件，把函数体恢复到 `middleware/distributor.go:422` 附近，并恢复 import。

## Upstream Sync Review Point

上游若改动 `getModelRequest` 的路径分支，只复核末尾 Compact 检测调用是否仍在所有 `/v1/responses` 请求返回前执行。
