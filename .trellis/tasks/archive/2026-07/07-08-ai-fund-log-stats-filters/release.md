# 发布说明：ai-fund 日志统计优化

## 数据库迁移

本次会通过 `Log` 模型 `AutoMigrate` 为 `logs` 表新增两个复合索引：

- `idx_logs_token_created_at(token_id, created_at)`
- `idx_logs_token_type_created_at(token_id, type, created_at)`

主库日志表和独立 `LOG_SQL_DSN` 日志库都会走现有 `AutoMigrate(&Log{})` 路径。ClickHouse 日志库保持现有表结构。

## 生产注意事项

- MySQL 线上只读排查显示 `logs` 约 162 万行，数据约 1.28GB、索引约 704MB，新增索引会产生 IO 和写入放大。
- 建议低峰发布，并观察 MySQL CPU、IO、连接数和应用启动耗时。
- 当前 MySQL `innodb_buffer_pool_size` 为 128MB，明显小于日志数据和索引规模；索引上线后仍建议单独评估调大 buffer pool。
- 本次远程排查没有在线执行 DDL、重启或配置变更。

## 发布后验证

1. 在 `/log` 输入有效 API Key，确认认证阶段快速返回，不触发全历史统计。
2. 默认时间范围加载统计卡片、模型分布、消耗趋势和日志表格。
3. 在 MySQL 执行 `SHOW INDEX FROM logs`，确认两个新索引存在。
4. 对 `token_id + created_at` 和 `token_id + type + created_at` 查询执行 `EXPLAIN`，确认不再只依赖 `idx_logs_token_id` 单列索引。
