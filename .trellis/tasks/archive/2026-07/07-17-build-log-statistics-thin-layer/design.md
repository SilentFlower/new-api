# 公共日志与缓存统计 Build 薄层化设计

## 1. 目标结构

### 新建文件

| 文件 | 职责 |
| --- | --- |
| `controller/log_public.go` | 公共 API Key 日志分页、统计和图表 Controller，以及统一筛选参数解析 |
| `model/log_public.go` | 公共 API Key 日志筛选、统计、模型分布、趋势、分页查询和脱敏 |
| `model/log_statistics.go` | Dashboard 使用的多 API Key、多分组额度与 RPM/TPM 汇总 |

### 保持独立的现有文件

| 文件 | 职责 |
| --- | --- |
| `model/token_statistics.go` | Anthropic 缓存 Token 解析、聚合、小时桶和历史统计迁移 |

### 修改的上游热点文件

| 文件 | 治理后保留内容 |
| --- | --- |
| `controller/log.go` | 管理员/用户通用日志入口和旧版日志清理兼容入口 |
| `model/log.go` | `Log` 实体、稳定通用日志查询、导出和清理入口；`SumUsedQuota` 继续作为兼容包装调用独立实现 |

## 2. 行为数据流

### 公共 API Key 日志

Router 继续注册原有 `GetLogByKeyPaged`、`GetLogStatByKey` 和 `GetLogChartDataByKey` 函数，不改路由或中间件。Controller 从 `TokenAuthReadOnly` 写入的 `token_id` 读取身份，解析 `type`、`model_name`、`request_id`、`start_timestamp` 和 `end_timestamp`，再调用 Model 独立领域入口。

Model 继续统一使用 `TokenLogFilterParams` 构造 `LOG_DB` 查询。`count/rpm` 跟随日志类型；quota、prompt/completion/total tokens、TPM 和趋势强制使用消费日志子集；模型名继续复用安全 LIKE helper，请求 ID 保持精确匹配。

分页结果继续清空渠道、用户名和 IP，并从 `other` 删除 `admin_info` 与 `reject_reason`。分页编号、总数限制、错误文案和响应包装保持不变。

### Dashboard 汇总

`SumUsedQuota` 保持原签名，继续把单值 token/group 转换为切片后调用 `SumUsedQuotaWithFilters`。多值实现迁入 `model/log_statistics.go`，保留 token 与 group 交集、消费日志限定、最近 60 秒 RPM/TPM，以及 Anthropic 缓存 Token 统计口径。

### 缓存 Token 统计

`model/token_statistics.go` 保持现状，不移动、不重写。公共统计和 Dashboard 汇总继续调用其中的统一聚合函数，历史 `quota_data.token_used` 迁移版本、幂等锁和旧日志兼容不变。

## 3. 兼容性与安全

- 不改 Router 注册、Gin handler 函数名、查询参数或 API 响应结构。
- 不改 `TokenLogStat`、`TokenModelStat`、`TokenLogFilterParams` 和 `QuotaData` 的字段或 JSON 标签。
- 不改 `LOG_DB` 选择、ClickHouse 分支、复合索引或任何数据库迁移。
- 不改 SQLite、MySQL、PostgreSQL 使用的 GORM 查询和安全 LIKE 语义。
- 不改 API Key 鉴权、频率限制、分页、排序、错误文案和时间跨度限制。
- 不改 Anthropic cache read/write 解析、去重和历史迁移口径。

## 4. 回滚

- 将 Controller 函数原样移回 `controller/log.go`。
- 将公共 Token Model 逻辑原样移回 `model/log.go`。
- 将 `SumUsedQuotaWithFilters` 原样移回 `model/log.go`。
- 删除新增领域文件；不涉及数据、配置、索引或迁移回滚。

## 5. 上游同步复核点

- 上游 `controller/log.go` 是否新增同名 handler 或调整通用日志入口。
- 上游 `model/log.go` 是否修改 `Log` 实体、通用筛选 helper、分页排序或脱敏规则。
- 上游是否调整 ClickHouse 日志排序、日志表索引或 `LOG_DB` 初始化。
- 上游是否新增 Token 统计字段或改变消费日志、缓存 Token 口径。
