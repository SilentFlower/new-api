# Release Operations

## Conclusion

Release operations exist.

- [09-11-channel-period-budget-fallback] 首版、后续回归修复及最终号池展示已发布；本次只归档记录，不再执行部署或数据操作。
- 最终展示提交为 ai-fund `45dc87b`；NewAPI 保持 `build-0bb0c14`，没有为展示新增接口字段或重启服务。

## Evidence Checked

- `task.json`、`prd.md`、`design.md`、`implement.md`、`implement.jsonl`、`check.jsonl`。
- `research/redis-recovery-20260911.md`、两仓 Git 状态和提交文件列表。
- NewAPI `097cc8fdc`、`34e7869b9`、`0bb0c1489`；ai-fund `2c275bf`、`45dc87b`。
- `model/main.go` 的普通及快速迁移注册、三个新增模型，以及 ai-fund 部署规范。
- `/tmp/pool-fallback-release/deployment-result.json`、Worker / Pages 发布日志、测试日志及生产页面资源校验结果。

## Drift Check

- 缺失 `release.md`，本次补齐首版及后续已执行的上线事项。
- 早期记录中的“尚未提交／部署”是当时状态；最终状态以 `implement.md` 的“最终状态与归档依据”和本文件为准。
- 本轮没有新的标准 Check-All 报告或正式 spec_update_result；已有测试、构建、规范变更与线上资源证据分别记录，不互相替代。

## SQL Changes

- [09-11-channel-period-budget-fallback] 首版通过 NewAPI 启动迁移注册 `ChannelPeriodPolicy`、`ChannelUserPeriodOverride`、`ChannelQuotaTracking`，普通和快速迁移入口均覆盖；不需要另行执行手写 SQL。
- 最终号池展示不新增 NewAPI 或 D1 表，不修改生产金额、策略或账单。SQLite、MySQL 8.0、PostgreSQL 16 合同测试已通过，最低支持版本未实测。

## Configuration Changes

- [09-11-channel-period-budget-fallback] 池子预算、规则、个人特批及降级目标通过既有管理接口保存，降级默认关闭；D1 只保留池子映射与本地审计。
- 复用现有 Worker 的 NewAPI 地址、管理员凭据及认证密钥；本轮没有新增或轮换 Secret，不能用本地过期凭据覆盖线上值。
- Redis 持久化修复已完成：`/root/new-api/redis-data:/data`，AOF 开启且每秒同步；该挂载与既有 Compose override 必须保留。

## Batch / Deployment Scripts / Data Repair

- [09-11-channel-period-budget-fallback] 2026-09-11 已按用户授权增量恢复 8 个个人／号池日周键，保留正常消费；回执和备份见服务器 `/root/new-api/backups/quota-restore-20260911/`。不得再次叠加相同历史增量。
- 随后已完成 Redis 数据目录迁移，迁移前后 40 个额度哈希、324 个字段完全一致；备份见 `/root/new-api/backups/redis-persistence-20260911/`。
- 最终展示按 ai-fund SOP 发布 Worker，再发布 Pages 的 `main` 生产分支；无额外批处理或数据修复。为展示准备的 NewAPI 镜像未启用，不作为本次上线产物。

## External Systems / Dependent Platforms

- [09-11-channel-period-budget-fallback] Worker `ai-hub`：版本 `01ab105b-5017-41c7-b576-b5307366a0cf`，主域名 `https://ai-api.hub.flower-cli.com`。
- Pages `ai-hub-all`：生产部署 `c227969b`，页面 `https://ai.hub.flower-cli.com/pools`。
- NewAPI `https://ai.flower-cli.com`：保持既有 `build-0bb0c14`。本轮未调用真实供应商模型进行验收。

## Release Order

1. [09-11-channel-period-budget-fallback] 首版先发布支持周期策略的 NewAPI，再发布 ai-fund；该阶段已经完成。
2. 最终提示复用既有接口，只发布 ai-fund Worker 和 Pages；无需再次更新 NewAPI。
3. 本次推送和归档不重复上述发布，不重建 Redis 或数据库容器。

## Rollback Notes

- [09-11-channel-period-budget-fallback] 最终展示可回退 ai-fund Worker 到 `8547aa24-7c30-4589-a176-17df1738b9c1`、Pages 到 `6e20add1-f936-4b9e-a26a-c766dffce5ee`；保留既有 Secret、绑定和数据。
- 若未来回滚整个周期预算能力，应先明确停用相应策略与降级；旧 NewAPI 不执行新增限制。不得删除策略表、Redis 计数、恢复回执或财务日志。

## Post-release Verification

- [09-11-channel-period-budget-fallback] 已验证 Worker 全量 584 项、相关 Vue 组件 8 项、安装器 11 项及前端构建；覆盖个人隔离、整体超限、恢复、配置版本变化与单池失败。
- 生产 `/pools` 返回 200，`Pools-Czw1lvLO.js` 与本地构建 SHA-256 相同；NewAPI 公开版本仍为 `build-0bb0c14`。
- 已登录生产用户的浏览器全链路尚未实测：本地认证密钥未通过线上认证，未修改线上认证配置。后续有有效会话时由维护者核对本人超限、其他用户正常、号池整体超限三种显示；本轮不通过修改生产额度制造场景。
