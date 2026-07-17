# 公共日志与缓存统计 Build 薄层化实施计划

## 1. 固化治理前基线

- [ ] 运行公共 Token 日志、Dashboard 汇总、Anthropic 缓存 Token 和日志脱敏定向测试。
- [ ] 运行 Model 定向 race，确认全局测试夹具无新增竞态。
- [ ] 记录 `controller/log.go`、`model/log.go` 和 `model/token_statistics.go` 治理前职责边界。

验证：

```bash
go test ./controller ./model -run 'TokenLog|TokenQuota|TokenModel|SumUsedQuotaWithFilters|StatisticToken|LogFormat|ClickHouse' -count=1
go test -race ./model -run 'TokenLog|TokenQuota|TokenModel|SumUsedQuotaWithFilters|StatisticToken|LogFormat' -count=1
```

## 2. 迁移公共日志 Controller

- [ ] 新建 `controller/log_public.go`，原样迁移三个公共 API Key handler 和统一筛选参数解析。
- [ ] 保持 `token_id` 鉴权上下文、分页、查询参数、错误响应和一个月时间跨度限制不变。
- [ ] `controller/log.go` 只移除对应实现，不调整其他管理/用户日志入口。

## 3. 迁移公共日志与汇总 Model

- [ ] 新建 `model/log_public.go`，原样迁移公共筛选结构、统计、模型分布、趋势、分页和脱敏实现。
- [ ] 新建 `model/log_statistics.go`，原样迁移 `SumUsedQuotaWithFilters`。
- [ ] 保留 `Stat`、`SumUsedQuota`、`SumUsedToken` 和其他上游通用入口的位置与签名。
- [ ] 保持 `model/token_statistics.go` 独立，不移动缓存 Token 解析、聚合和历史迁移。
- [ ] 清理迁出后不再需要的 import，不做无关格式或结构调整。

## 4. 完整回归与安全检查

- [ ] 运行 Controller、Model 完整测试和定向 race。
- [ ] 运行 `go vet`、`gofmt`、`git diff --check` 和符号唯一性扫描。
- [ ] 复核 Router 路由、前端请求参数、API 响应结构和脱敏字段未变化。
- [ ] 复核 `LOG_DB`、ClickHouse、跨数据库查询和 Anthropic 缓存 Token 口径未变化。
- [ ] 对比治理前后热点文件，确认只删除 Build 特有实现块。

最终验证：

```bash
go test ./controller ./model -count=1
go test -race ./model -run 'TokenLog|TokenQuota|TokenModel|SumUsedQuotaWithFilters|StatisticToken|LogFormat' -count=1
go vet ./controller ./model
git diff --check
```

## 5. Review Gates

- [ ] Gate A：治理前公共日志、Dashboard 汇总和缓存 Token 基线通过。
- [ ] Gate B：路由、handler 名、Model 导出签名和 JSON 字段完全不变。
- [ ] Gate C：筛选、统计、趋势、脱敏、分页和错误行为完全不变。
- [ ] Gate D：未改数据库迁移、索引、LOG_DB、ClickHouse 或历史数据。
