# Release Operations

## Conclusion
Release operations exist.

## SQL Changes
None

## Configuration Changes
- 新增渠道设置字段 `use_upstream_model_for_billing`，存储在渠道 `setting` JSON 中。
- 历史渠道缺省为关闭，不需要批量迁移。
- 上线后如需启用该能力，需要在对应渠道编辑页手动打开“重定向后按上游模型计费”。

## Batch / Deployment Scripts / Data Repair
None

## External Systems / Dependent Platforms
- 需要部署当前仓库代码到实际 API 网关服务。
- 如存在多套网关实例或管理后台实例，需要同步发布，避免前端已保存字段但后端未识别，或后端支持但管理后台不可配置。

## Release Order
1. 先部署后端服务。
2. 再部署默认 UI 与经典 UI 静态资源。
3. 按需在目标渠道开启“重定向后按上游模型计费”。

## Rollback Notes
- 功能开关关闭即可恢复按原始请求模型计费与记录日志主模型。
- 代码回滚不涉及数据库结构回滚。

## Post-release Verification
- 对一个配置了 `model_mapping` 的渠道发起请求，确认开关关闭时日志主模型和计费模型仍为原始请求模型。
- 打开渠道开关后再次请求，确认发生模型映射时日志主模型、`billing_model_name` 和计费价格按最终上游模型计算。
- 检查日志 `other` 包含 `origin_model_name`、`upstream_model_name` 和 `billing_model_name`。
