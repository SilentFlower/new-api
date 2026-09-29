# 工作流独立账号与令牌契约

## 1. Scope / Trigger

- 适用于通过 `new-api` 普通用户承载工作流额度、把 root 令牌迁移到独立账号、查询生效归属，以及为单个工作流轮换令牌的改动。
- 一个工作流使用稳定的原始 `workflow_id` 作为令牌名，以经确认的中文短名作为 `users.username` 和 `users.display_name`。用户余额承担额度限制；生效令牌使用无限令牌额度，不能让订阅额度绕过用户余额。

## 2. Signatures

| 路由 | 请求 | 成功数据 |
| --- | --- | --- |
| `POST /api/token/migrate` | `token_ids: int[]`；可选 `targets: [{token_id:int, username:string, user_quota:int}]` | `results[]`，逐项含 `token_id`、`token_name`、`status`，成功项含 `new_username/new_user_id` |
| `GET /api/token/workflow/status` | 查询参数 `username`、`workflow_id` | `user` 与 `tokens[]`；令牌项含 `effective_state_matches`，不含 Key |
| `POST /api/token/workflow/issue` | `user_id`、`old_token_id`、`workflow_id` | `token_id`、带 `sk-` 前缀的 `key` |
| `POST /api/token/workflow/retire` | `user_id`、`old_token_id`、`replacement_token_id` | `retired_token_id` |

- 四个路由都继承 `UserAuth()` 并叠加 `RootAuth()`；handler 再检查 root 角色。迁移、签发和停用接口使用 `CriticalRateLimit()`。
- 名称上限由 `model.UserNameMaxLength` 和 `model.TokenNameMaxLength` 约束为 64 个 Unicode 码点。`users.username/display_name`、`tokens.name`、`logs.username/token_name` 不在 GORM 标签中指定 `size:64`；当前 MySQL 驱动把这些有索引的字符串映射为 `varchar(191)`，PostgreSQL 驱动映射为 `text`。`quota_data.username/token_name` 沿用原有 `size:64`。

## 3. Contracts

- `token_ids` 每批为 1 至 `MigrateTokensBatchMaxSize`（100）项。`targets` 缺席时保持原迁移行为；存在时必须与 `token_ids` 一一对应且不重复。`username` 原样使用，不截断、不加后缀；显式名称拒绝首尾空白、无效 UTF-8 和控制或不可见字符。`user_quota` 是原始整数额度，显式 `0` 有效，上限为 `int(1000000000 * common.QuotaPerUnit)`。
- 每枚令牌分别在事务中创建启用的普通用户并只迁移 `token.user_id`；失败只回滚该令牌，其余项继续。新用户的 `record_ip_log` 必须为 true；响应、日志和审计不得包含随机密码或迁移令牌明文。
- 迁移事务提交后必须刷新令牌缓存。缓存刷新失败时，该结果标为失败，即使数据库迁移已完成；开通流程先保存 Key，再使用状态接口核对 `effective_state_matches=true`，最后才发布 Secret。
- `status` 用精确用户名与令牌名查询；`effective_state_matches` 必须比较实际 `ValidateUserToken` 得到的令牌 ID、用户 ID、名称、状态及完整访问策略，不能只比较数据库记录。
- `issue` 只接受已启用、开启 IP 记录的普通用户及其已启用的同名无限额令牌。已有一枚同用户同名替代令牌时，只有 group、有效期、模型限制、IP 限制、跨组重试和自动分组策略均相同才能复用；存在多枚时拒绝。
- `retire` 只在替代令牌属于同用户、同工作流、已启用且访问策略相同时停用旧令牌，并更新或删除旧令牌缓存。先把新 Key 切入 Gateway Secret、确认所有旧 Pod 退出，再调用停用接口。
- `scripts/provision_workflow_accounts.py` 只处理清单中 `enabled=true` 的项；启用前要求已批准名称、运行时注册、Gateway 验证、明确的能力列表、空的直连路径列表及正数额度。凭证包和 Secret 文件保持私有权限；重复执行须从已有账号及凭证包恢复，不能静默创建第二套账号。
- 对已是 `varchar(64)` 的生产 MySQL，部署移除五列定长标签的版本之前，先核对现有列属性并将这五列预扩至 `varchar(191)`，保留原来的 NULL/default 和索引语义；否则启动期 `AutoMigrate` 会尝试扩列。预扩列应紧接新版本部署：旧版本仍声明 64，若预扩列后旧进程重新启动，可能再次收窄列宽。MySQL 8.2 的预扩列使用 `ALGORITHM=INPLACE, LOCK=NONE` 和较短的会话 `lock_wait_timeout`；元数据锁获取失败时先排查长事务，再重试，不能假定在线 DDL 完全不影响并发请求。

## 4. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| 非 root 调用任一工作流账号接口 | 拒绝，不返回账号状态或 Key |
| `token_ids` 为空或超过 100 项；`targets` 数量、ID、名称或额度非法 | 整批请求在迁移前拒绝 |
| 单枚令牌不存在、用户名冲突或事务失败 | 仅该项返回 `failed`，其他项继续 |
| 数据库已迁移但缓存仍指向旧用户或旧策略 | 状态项 `effective_state_matches=false`；开通流程停止发布 Secret |
| 待切换令牌策略不一致、属于其他用户或存在多枚 | 拒绝签发或停用，不改变旧令牌状态 |
| Secret 已有另一枚 Key 或凭证包无法恢复 | 开通流程停止，人工核对归属 |
| 部署前五列仍为 `varchar(64)` | 先完成数据库预扩列并核对目标列宽，避免应用启动时触发隐式 DDL |
| 预扩列后旧版本重启 | 旧模型可能把列宽重新收窄；将预扩列与新版本部署安排在同一窗口 |
| 在线扩列等待元数据锁超时 | 保持旧应用运行；排查阻塞事务，在低流量时段重试 |

## 5. Scenarios and Examples

- Normal：工作流 `material-standard-dedup` 以中文名迁入一个普通用户、设置有限用户余额；状态接口确认有效归属后发布其唯一 Key。轮换时先签发同策略 Key，更新 Secret 并滚动 Gateway，最后停用旧 Key。
- Base：旧调用方只传 `token_ids`，仍按原自动命名和额度继承规则迁移。
- Incorrect：数据库显示令牌已归新用户，就忽略 `effective_state_matches=false` 并发布 Secret；实际缓存仍可能以旧用户计费。
- Correct：把状态接口的有效归属核验作为发布门禁；失败时停止并修复缓存或归属，不生成第二个用户。
- Schema base：业务请求最多接受 64 个 Unicode 码点；五个用户、令牌及日志名称列可存储超过 64 个字符，`quota_data` 的两个名称列保持 64。
- Schema incorrect：直接部署移除 `size:64` 的模型标签，让 `AutoMigrate` 在启动期对线上日志表扩列。
- Schema correct：新镜像就绪后，在同一部署窗口核对五列原有 NULL/default 与索引，显式在线扩至 `varchar(191)` 并确认列宽，随即部署应用；若元数据锁超时则停止本次上线。

```json
{"token_ids":[12],"targets":[{"token_id":12,"username":"标准物料查重","user_quota":500000}]}
```

## 6. Tests Required

- `TestValidateMigrateTargetsPreservesLegacyAndAcceptsZeroQuota` 与 `TestValidateMigrateTargetsRejectsIncompleteOrAmbiguousBatch` 断言兼容路径、零额度和整批预检。
- `TestMigrateSingleTokenInTxUsesExplicitNameAndZeroQuota`、`TestMigrateSingleTokenInTxConflictKeepsOriginalOwner` 断言原样名称、额度及事务回滚。
- `TestGetWorkflowAccountStatusDoesNotExposeKey`、`TestGetWorkflowAccountStatusRejectsStaleTokenCache` 断言无敏感输出及实际鉴权归属。
- `TestWorkflowTokenRotationKeepsAccountAndDisablesOldToken`、`TestWorkflowTokenRotationRejectsCrossAccountReplacement`、`TestWorkflowTokenRotationRejectsRelaxedReplacementPolicy` 断言同用户连续账单与策略边界。
- `TestWorkflowNamePhysicalMigration` 须在隔离 MySQL 与 PostgreSQL 测试库验证旧窄列升级：五个名称列可读写 64 和 65 个中文字符，`quota_data.username/token_name` 列宽为 64 且可读写 64 个中文字符；MySQL 再次 `AutoMigrate` 不重复发出 `ALTER TABLE`。业务层单测继续验证名称上限；开通脚本测试须覆盖中断恢复、幂等、凭证权限及发布前状态核验。
