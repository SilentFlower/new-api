# Release Operations

## Conclusion

Release operations exist.

## Evidence Checked

- `task.json`、`prd.md`、`design.md`、`implement.md`
- `implement.jsonl`、`check.jsonl`
- 业务提交 `116993edd`、`201474a89`
- 任务记录提交 `75d73b91f`

## Drift Check

此前缺少 `release.md`；本文件按任务发布与回滚检查、实际提交文件和已验证行为补齐。

## SQL Changes

- 无需手工 SQL。
- 部署启动时由现有 GORM `AutoMigrate` 为渠道表增加可空整数列 `user_daily_quota_limit`；必须确认 SQLite、MySQL 和 PostgreSQL 环境的自动迁移正常完成。

## Configuration Changes

- 无新增环境变量或配置中心键。
- 多实例部署必须使用一致的服务端本地时区，否则自然日边界和 `reset_at` 会不一致。
- 多实例全局统计依赖生产 Redis；未配置 Redis 时仅保证当前实例内的额度和并发状态。
- 部署前确认 Redis 健康。Redis 已配置但不可用时，正数每日限额检查会返回稳定 `503`。

## Batch / Deployment Scripts / Data Repair

- 无批处理、部署脚本或数据修复命令。
- 不扫描消费日志回补上线前或 Redis 故障期间的历史额度；历史异步任务只从新版本上线后的正向差额开始记录。

## External Systems / Dependent Platforms

None.

## Release Order

1. 确认所有实例时区一致且 Redis 可用。
2. 部署应用并确认渠道表自动迁移完成。
3. 在测试渠道配置正数每日额度，执行上线后验证。

## Rollback Notes

- 紧急关闭额度检查时，将渠道 `user_daily_quota_limit` 设置为 `0`；正向已结算额度仍会继续记录并可查看。
- 若需完整停止记录，回滚本功能的正向累计薄层接入和领域模块。
- 新增数据库列可以保留；Redis 每日额度 Hash 和并发用户索引依赖 TTL 自动清理，无需全库扫描删除。

## Post-release Verification

- 验证同一用户当日累计未达上限时放行，达到或超过后新请求返回 `429`，且不预扣、不换渠、不禁用渠道。
- 使用并发请求确认软上限允许可接受的小幅超额，随后请求会被阻止。
- 在渠道“用户限制状态”中验证每日额度、个人目标值调整、当前并发自动刷新以及 Redis/内存模式提示。
- 调整个人额度时保留一个在途请求，确认请求不被取消，完成后实际额度继续累加。
- 临时使 Redis 不可用，确认正数限额请求返回 `503`；恢复后检查和管理视图恢复。
- 将每日限额设为 `0`，确认请求不做前置检查但正向额度仍持续记录；再切回正数后立即使用当天已有累计。
