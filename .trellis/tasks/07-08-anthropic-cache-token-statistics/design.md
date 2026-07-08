# 统计总 Token 包含 Anthropic 缓存 - Design

## Architecture

本任务只调整统计口径，不调整扣费、上游响应或日志明细字段语义。

新增一个后端统一统计入口，负责从 `model.Log` 计算用于统计的 Token 总量：

```
统计 Token = PromptTokens + CompletionTokens + cache_read + cache_write
```

其中：

- `cache_read` 来自 `logs.other.cache_tokens`。
- `cache_write` 优先使用 `cache_creation_tokens_5m + cache_creation_tokens_1h`；如果没有拆分值，则使用 `cache_creation_tokens`。
- 缓存字段缺失、类型异常、非消费日志时按 0 处理，避免历史脏数据影响查询。

## Data Flow

### 新日志写入

`model.RecordConsumeLog` 当前在 `DataExportEnabled` 时调用 `LogQuotaData(..., params.PromptTokens+params.CompletionTokens)` 写入 `quota_data` 缓存。

改为：

1. 基于即将写入的日志字段和 `params.Other` 计算统计 Token。
2. 将该统计 Token 写入 `quota_data.token_used`。
3. `logs.prompt_tokens` / `logs.completion_tokens` 保持现状，日志详情仍能展示原始输入、输出和缓存拆分。

### 从 `logs` 聚合

现有多个查询使用 SQL：

```
sum(prompt_tokens + completion_tokens) as token_used
```

由于缓存信息在 `logs.other` JSON 字符串中，跨 SQLite/MySQL/PostgreSQL 不能直接依赖数据库 JSON 函数。需要改为 Go 侧聚合：

- 先用 GORM 查询符合条件的消费日志必要字段。
- 用统一 helper 计算每条日志的统计 Token。
- 在 Go 中按原有维度聚合 `count`、`quota`、`token_used`。

受影响入口：

- `model.getQuotaDataFromLogs`
- `model.GetLogSummaryByKey`
- `model.GetLogDetailByKeyModel`
- `model.GetTokenQuotaData`

### `quota_data` 历史回算

提供 `quota_data` 重建路径，从 `logs` 全量消费日志重新聚合到小时粒度：

- 聚合维度保持现状：`user_id`、`username`、`model_name`、`token_name`、小时级 `created_at`。
- `count`、`quota`、`token_used` 全部从 `logs` 重算。
- 执行前清理或覆盖同维度旧值，避免双算。

推荐实现为模型层函数，例如 `RebuildQuotaDataFromLogs()`，供启动迁移或管理入口调用。若接入自动迁移，需要保证幂等。

## Compatibility

- 不使用数据库 JSON 函数，保证 SQLite、MySQL、PostgreSQL 一致。
- 保留 `logs` 表结构，不新增必须迁移的日志字段。
- `quota_data` 表结构不变。
- 历史日志 `other` 为空或解析失败时，仅统计 `PromptTokens + CompletionTokens`。

## Performance

Go 侧聚合会读取日志行，必须控制字段和范围：

- 查询只选必要字段：用户、模型、Token 名称、时间、额度、Prompt/Completion、Other、分组或 Token ID。
- 面向图表的查询通常有时间范围；导出已有最大行数限制。
- 历史回算是批处理，应按分页/批次读取，避免一次性加载全部日志。

## Rollback

- 回退代码后，`quota_data` 中已回算的新口径不会自动恢复旧口径。
- 如必须回退统计口径，需要再次按旧公式重建 `quota_data`。
- 因扣费和日志原始字段不变，回滚不影响用户额度余额。
