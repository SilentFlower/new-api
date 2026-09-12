# 回滚演练记录（AC10）

日期：2026-09-12。环境：本地文件型 SQLite（`sqlite3` CLI + 本仓库迁移代码），临时 Go 测试驱动，演练后已删除测试文件。

## 演练步骤与证据

| 步骤 | 操作 | 结果 |
| --- | --- | --- |
| 1 | 构造 v1 状态：渠道列日 100/周 500、v1 策略（池子日 100 + 一条 date_range 规则）、旧覆盖表各 1 行 | `policies=1 rev=1 schema=1 limit_overrides=1 period_overrides=1 budget_overrides=0` |
| 2 | `sqlite3 drill.db ".dump channel_period_policies channel_user_limit_overrides channel_user_period_overrides channel_user_budget_overrides" > backup.sql` | 备份 1932 bytes |
| 3 | 启动迁移 `service.MigrateChannelBudgetPolicies` | 耗时 19.8ms；`rev=2 schema=2 budgets=5 budget_overrides=3` |
| 4 | 回滚：`DROP TABLE` 四表后 `sqlite3 drill.db < backup.sql` | `rev=1 schema=1 limit_overrides=1 period_overrides=1 budget_overrides=0`，与步骤 1 一致 |
| 5 | 回滚后新代码读取 v1：`GetChannelPeriodPolicy` 按渠道列兜底转换为 v2 视图（revision 仍为 1，存储不改写） | 服务可继续响应 |
| 6 | 再次执行迁移 | `rev=2 budget_overrides=3`，与首次迁移等价 |

## 生产回滚流程

1. 上线前备份三张旧表。首次从 v1 升级时新表尚不存在，不能把它加入 MySQL/PostgreSQL 必须存在的表名列表：
   - SQLite：`sqlite3 one-api.db ".dump channel_period_policies channel_user_limit_overrides channel_user_period_overrides" > backup.sql`
   - MySQL：`mysqldump --single-transaction <db> channel_period_policies channel_user_limit_overrides channel_user_period_overrides > backup.sql`
   - PostgreSQL：`pg_dump -t channel_period_policies -t channel_user_limit_overrides -t channel_user_period_overrides <db> > backup.sql`
   - 若上线前已存在 `channel_user_budget_overrides`，同时备份该表并记录其存在状态。本地演练预建了空新表，因此四表一同备份。
2. 回滚 = 回退到上一版本代码镜像 + 恢复上述备份（先 `DROP TABLE` 再回放备份，或用数据库自带的表级恢复）。新表同样恢复到上线前状态：已有备份则恢复，首次升级前不存在则删除迁移创建的新表，避免下一次迁移被残留 v2 覆盖记录影响。旧代码不会读取该新表。
3. 迁移是单向的：新代码保存过的 v2 策略在旧代码下会被判定无效并返回 503，因此回滚必须恢复 `channel_period_policies`，不能只回退代码。
4. 新代码在“备份恢复但未重启迁移”期间仍可读 v1（按渠道列兜底转换），不会中断请求。
5. 重新前进：再次启动新代码即自动迁移，结果与首次迁移等价（幂等）。

## 边界

- 回滚演练覆盖 SQLite，已补充下方真实旧版执行证据。MySQL 8.0 / PostgreSQL 16 的迁移契约测试通过；该结果不等同于 MySQL/PostgreSQL 备份恢复演练。
- Redis 计数 key 沿用阶段一身份，迁移与回滚均不清零，无需备份 Redis。

## CHK-007 补充：实际旧版本读取与请求拦截

2026-09-12 22:24–22:26（Asia/Shanghai），从 `git archive HEAD` 导出未含本次 v2 变更的 `f0e225fc1` 版本，独立目录编译运行旧 `service` 包。演练使用临时 SQLite 文件，未连接业务数据库。

1. 当前代码创建 v1：渠道日 100 / 周 500，池子日 100，未来 date_range 个人整段预算 15，个人日/周提额 200/900、整段提额 30。四张策略/覆盖表备份为 1941 bytes SQL。
2. 当前 `MigrateChannelBudgetPolicies` 实际迁移后，断言 `schema=2 revision=2`，复制预算覆盖 3 条。
3. 删除演练库的四张表并回放备份，断言策略逐字段恢复为 v1、revision=1。
4. 在归档的旧版目录执行 `CHANNEL_BUDGET_ROLLBACK_DATABASE=<临时 drill.db> go test ./service -run '^TestChannelBudgetRollbackOldVersion$' -v -count=1`。旧 `GetChannelPeriodPolicy` 读取 `schema=1 revision=1 pool_limit=100`，旧个人日覆盖解析为 200。
5. 使用旧版 `RecordChannelUserQuotaUsage` 写入 99，通过 Gin 请求调用旧版 `CheckSelectedChannelPeriodLimits`，HTTP 200；再写入 1 达到池子上限，请求返回 HTTP 429。没有调用真实供应商。

结果：两个临时驱动测试均通过。执行日志：`/tmp/budget-rollback-prepare.log`、`/tmp/budget-rollback-old-version.log`；临时测试不纳入产品代码。本演练证明实际旧版可读取恢复数据，并保持预算内放行、到达预算拦截的请求行为。
