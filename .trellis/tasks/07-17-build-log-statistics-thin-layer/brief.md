# Brief — 公共日志与缓存统计 Build 薄层化

## Goal

- 将公共 API Key 日志查看器和 Dashboard 多维统计从上游日志热点文件迁入独立领域文件，同时保持全部 API、查询和统计行为不变。

## Scope

- 新建 `controller/log_public.go` 承载公共 API Key 日志分页、统计、图表和筛选参数解析。
- 新建 `model/log_public.go` 承载公共筛选、统计、模型分布、趋势、分页和脱敏。
- 新建 `model/log_statistics.go` 承载 Dashboard 多 API Key、多分组额度与 RPM/TPM 汇总。
- 保持 `model/token_statistics.go` 继续独立承载 Anthropic 缓存 Token 解析、聚合和历史迁移。

## Non-Goals

- 不治理 Dashboard Excel 生成、看板页面或渠道设置前端。
- 不新增日志字段、索引、筛选项、统计口径或数据迁移。
- 不修复现有缺陷，不重构上游通用日志实现。

## Key Context

- Router 和前端继续调用原有 handler 与 API 路径，迁移不能改变函数名或签名。
- `count/rpm` 跟随当前类型；quota、tokens、TPM 和趋势只统计消费日志。
- Anthropic 总 Token 包含 cache read 和 cache creation，并继续复用 `model/token_statistics.go`。
- 公共日志必须清空渠道、用户名、IP，并删除 `other.admin_info` 与 `other.reject_reason`。
- 所有查询继续走 `LOG_DB`，保留 SQLite、MySQL、PostgreSQL 和 ClickHouse 行为。

## Acceptance

- 公共 Controller、公共 Model 和 Dashboard 汇总实现位于独立领域文件。
- `controller/log.go` 与 `model/log.go` 只移除对应 Build 特有实现块。
- API 鉴权、参数、分页、排序、响应、错误、脱敏和一个月时间限制不变。
- RPM/TPM、quota、模型分布、趋势和 Anthropic 缓存 Token 口径不变。
- 相关测试、race、vet、格式和差异检查通过。

## Next Step

- 激活任务后先运行治理前基线，再原样迁移职责、清理 import，并完成全量回归与 Check-All。
