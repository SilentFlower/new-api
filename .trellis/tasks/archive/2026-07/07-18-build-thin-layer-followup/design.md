# Design — Build 薄层后续治理

## Boundary

本父任务只管理批次边界、顺序和整体验收；直接代码修改由 6 个子任务承担。父任务不把多个治理点合并到一个大改中，避免回滚困难和职责扩散。

## Child Task Map

| 顺序 | 子任务 | 主要新文件职责 | 原上游文件薄接入点 |
| --- | --- | --- | --- |
| 1 | RelayInfo Build 方法薄层化 | `relay/common/responses_compact.go` 承载 Compact 状态判断方法 | `relay/common/relay_info.go` 只保留结构字段 |
| 2 | Responses Compact 日志审计薄层化 | `service/responses_compact_audit.go` 承载审计 key、set、clear、path 处理 | `service/log_info_generate.go` 保留通用日志合并与 quota saturation |
| 3 | Alpha Search 请求校验薄层化 | `relay/helper/alpha_search_request.go` 承载 Alpha Search 校验 | `relay/helper/valid_request.go` switch 只保留调用 |
| 4 | Distributor Compact 检测薄层化 | `middleware/responses_compact_detection.go` 承载读取 body、检测 mode、写上下文 | `middleware/distributor.go` 只保留一次函数调用 |
| 5 | Responses Handler Compact 分支薄层化 | `relay/responses_compact_handler.go` 承载 Compact endpoint 校验、请求转换、临时计费快照恢复和 audit outcome | `relay/responses_handler.go` 保留普通 Responses 主流程和窄调用 |
| 6 | 公共 Token 日志前端薄层化 | `components/`、`hooks/`、`lib/` 按职责拆分 Default/Classic 页面内部逻辑 | 原页面入口只保留布局、状态编排和子组件挂载 |

## Compatibility

- 所有 Go 包名、导出函数名和调用方签名保持不变。
- 不改变 `RelayInfo` 字段语义；只是移动方法实现所在文件。
- 不改变 Compact/Alpha Search 原始透传、模型选择、计费、错误码和日志隔离契约。
- 前端拆分不改变路由、API 参数、表格列、筛选项、导出/刷新/切换 key 等用户行为。

## Rollback

每个子任务独立回滚。父任务回滚只需要撤销任务文档；代码回滚按子任务逐项撤销新增文件和薄接入修改。

## Upstream Sync Review Points

- 上游若修改 `service/log_info_generate.go`、`relay/helper/valid_request.go`、`middleware/distributor.go`、`relay/responses_handler.go`、`relay/common/relay_info.go` 或公共日志页面，优先复核薄接入点是否仍在正确边界。
- 若上游新增同类扩展点，不主动重构历史逻辑；只在能进一步减少冲突且行为可测试时迁移。
