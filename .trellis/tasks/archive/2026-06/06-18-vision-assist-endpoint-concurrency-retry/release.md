# Release Operations

## Conclusion
Release operations exist.

## SQL Changes
None

## Configuration Changes
- 新增渠道视觉辅助配置字段：`endpoint_mode`、`max_concurrency`、`retry_count`、`retry_backoff_ms`。
- 历史渠道缺少这些字段时按运行时默认值处理，不需要批量迁移。
- 如果要启用 Gemini / Claude / Responses 等辅助端点模式，需要在渠道编辑页确认对应视觉辅助配置。

## Batch / Deployment Scripts / Data Repair
None

## External Systems / Dependent Platforms
- 需要发布 API 网关服务代码与前端构建产物。
- 使用 Docker 镜像部署时，确认目标环境拉取包含本任务提交的镜像标签。

## Release Order
1. 部署后端与前端构建产物。
2. 在管理端渠道编辑页确认视觉辅助新增配置可见且保存正常。
3. 使用含图片请求验证视觉辅助日志字段、并发/重试配置和失败策略。

## Rollback Notes
Rollback code only. No database rollback is required.

## Post-release Verification
- 验证历史渠道未配置新增字段时仍可正常转发。
- 验证默认 UI 和经典 UI 保存视觉辅助配置不会丢失未知 `setting` 字段。
- 验证视觉辅助失败时 `log.other` 包含端点模式、并发、重试和失败摘要字段。
