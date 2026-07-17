# 公共日志与缓存统计 Build 薄层化

## Goal

将公共 API Key 日志查询、筛选、脱敏、统计趋势和 Anthropic 缓存 Token 聚合迁入独立 Controller/Model 领域文件，降低 `controller/log.go` 与 `model/log.go` 的冲突面。

## Requirements

- 保持公共 Token 鉴权、查询参数、分页、排序、频率限制和响应结构不变。
- 保持管理员字段、渠道、用户名、IP、`admin_info` 和 `reject_reason` 脱敏行为不变。
- 保持 RPM/TPM、quota、模型分布、趋势和 Anthropic cache read/write 统计口径不变。
- 查询继续使用 `LOG_DB`，兼容 SQLite、MySQL、PostgreSQL 和现有 ClickHouse 分支。
- `model/log.go` 只保留日志实体和稳定通用入口，公共查看器业务迁入独立文件。

## Acceptance Criteria

- [ ] 公共 Token 日志 Controller 与 Model 逻辑位于独立领域文件。
- [ ] 缓存 Token 解析、聚合和历史迁移职责边界清晰。
- [ ] `model/log.go` 的公共查看器大块实现被移除，仅保留必要共享调用。
- [ ] 全部公共 API 响应、脱敏、筛选和统计行为保持不变。
- [ ] 数据库方言、日志独立库和 ClickHouse 相关测试通过。

## Out Of Scope

- Dashboard Excel 生成和看板页面。
- 新日志字段、新索引或生产数据修复。
