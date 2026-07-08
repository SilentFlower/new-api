# 统计总 Token 包含 Anthropic 缓存 - Implement

## Checklist

1. 新增统一统计 helper
   - 从 `model.Log` 或等价参数计算统计 Token。
   - 解析 `other.cache_tokens`、`other.cache_creation_tokens`、`other.cache_creation_tokens_5m`、`other.cache_creation_tokens_1h`。
   - 覆盖数字类型、字符串数字、缺失字段、无效 JSON。

2. 修正新日志写入 `quota_data`
   - `RecordConsumeLog` 写 `LogQuotaData` 时使用统计 helper。
   - 保持 `logs.prompt_tokens` 和 `logs.completion_tokens` 不变。

3. 修正从 `logs` 聚合的查询
   - `getQuotaDataFromLogs`
   - `GetLogSummaryByKey`
   - `GetLogDetailByKeyModel`
   - `GetTokenQuotaData`
   - 保持现有 token/group/username/token_id/time 过滤语义。

4. 实现 `quota_data` 历史重建
   - 从 `logs` 分批读取消费日志。
   - 按 `user_id`、`username`、`model_name`、`token_name`、小时级 `created_at` 聚合。
   - 清理旧 `quota_data` 后写入新聚合结果，或使用事务内覆盖策略。
   - 确保重复执行不双算。

5. 接入回算触发方式
   - 优先考虑幂等迁移或明确的模型层调用点。
   - 若自动启动回算可能影响大库性能，改为管理入口或运维脚本，并在说明中标注。

6. 更新导出明细展示
   - Sheet 1/2 使用新 `token_used`。
   - Sheet 3 保持输入和缓存拆分可见；如需要增加“统计 Token 数”列，应避免破坏原列含义。

7. 测试
   - 新增/更新 `model` 层测试：
     - 缓存读计入统计。
     - 缓存写计入统计。
     - 5m/1h 拆分缓存写优先于总缓存写，避免重复计入。
     - `quota_data` 回算幂等。
     - token/group/username/token_id 过滤结果保持正确。
   - 运行相关测试：
     - `go test ./model`
     - 如改 controller 导出逻辑，运行相关 controller 测试。

## Risky Files

- `model/log.go`
- `model/usedata.go`
- `model/usedata_rankings.go`
- `controller/usedata.go`
- `model/dashboard_filters_test.go`

## Review Gates

- 检查所有 `sum(prompt_tokens + completion_tokens)` 是否仍用于“总 Token”统计；若保留，必须说明不是统计口径入口。
- 检查没有直接引入 `encoding/json` 的 marshal/unmarshal 调用；业务解析应使用 `common` 包装。
- 检查没有使用数据库 JSON 函数导致跨库不兼容。
