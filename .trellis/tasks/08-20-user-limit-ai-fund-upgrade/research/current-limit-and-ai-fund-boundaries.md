# 当前限制与 ai-fund 边界调研

## NewAPI 现状

- `model/channel.go:54-55` 仅定义 `UserConcurrencyLimit` 与 `UserDailyQuotaLimit`，两者均由渠道配置提供默认上限。
- `.trellis/spec/backend/channel-user-daily-quota.md:9-12` 固定每日额度为 `channel_id + user_id + 服务端自然日` 的已结算软上限。
- `.trellis/spec/backend/channel-user-daily-quota.md:160-165` 固定每日状态使用 Redis Hash；未配置 Redis 时使用进程内状态，已配置但不可用时受限请求失败关闭。
- `.trellis/spec/backend/channel-user-daily-quota.md:182-184` 明确个人调整当前只覆盖“已使用额度”，不改变个人上限、账务或日志。
- `.trellis/spec/backend/channel-user-concurrency.md` 固定并发维度为 `channel_id + user_id`，Redis 租约或进程内租约负责实际并发数量。
- `model/subscription.go:358-367` 已把 weekly 周期对齐到服务端时区的下周一 `00:00`，周限可复用该业务边界。
- `middleware/distributor.go:453-477` 在每次初选或重试选渠时写入渠道限制上下文；手工切换渠道的任务、Midjourney 和视觉辅助路径另有同步点。
- `controller/relay_attempt.go:128-149` 在价格计算后、预扣前检查每日额度；任务、Midjourney 和视觉辅助也在对应预扣或上游调用前执行同类检查。
- `service/quota.go`、`service/text_quota.go`、`service/task_billing.go`、`service/tool_billing.go`、`service/violation_fee.go` 和 Midjourney 路径已集中覆盖每日正向额度记录点。
- `controller/channel_user_limits.go` 与 `web/src/features/channels/components/dialogs/channel-user-limits-dialog.tsx` 已提供每日额度和当前并发的管理 API 与 Dialog。

## ai-fund 现状

- `/root/project/ai-fund` 当前活动任务为 `.trellis/tasks/08-20-pool-user-limits`，状态为 `in_progress`，工作树已有 Worker 与 Vue 页面实现，后续不得回退或覆盖。
- 当前方案把 Claude/OpenAI 号池映射存入 D1 `app_settings`，Worker 使用服务端 NewAPI 管理凭据代理渠道、每日额度和并发接口。
- `/root/project/ai-fund/worker/src/pool_limits.js` 负责号池限制业务；`worker/src/newapi_client.js` 负责 NewAPI 白名单请求；`frontend/src/components/PoolLimitPanel.vue` 与 `PoolLimitAdminModal.vue` 负责展示和管理。
- `/root/project/ai-fund/frontend/src/views/Admin.vue` 已有门户用户、NewAPI 绑定、钱包、订阅和直接调额操作，是按指定用户管理个人覆盖的自然入口。
- D1 已承担门户配置和本地操作审计，不适合作为 Relay 限制事实源；否则 NewAPI 热路径需要依赖外部 Worker/D1，且会产生双写、到期漂移和故障放大。

## 已确认产品决策

- 个人覆盖是立即生效的绝对上限，不是增量。
- 实际有效值为 `max(渠道默认值, 当前有效个人覆盖值)`，因此覆盖永远不会降低限制。
- `expires_at` 可空；空值表示持续有效，设置后到期自动回落。
- 不支持未来定时开始。
- 个人覆盖不依赖活跃请求记录；支持提前搜索任意已存在用户配置，并在活跃日/周/并发列表提供快捷入口。
- 周期使用服务端时区，周一 `00:00` 刷新。
- NewAPI 主数据库是覆盖记录的唯一事实源；CF 只做代理、权限、金额换算、映射和审计。

## 设计结论

- NewAPI 增加渠道周限字段、独立周状态服务、个人覆盖模型和有效限制解析服务。
- Relay 热路径只消费解析后的有效限制；覆盖读取异常时回落到更保守的渠道默认值并记录脱敏告警，不扩大用户权限。
- 增加按 `channel_id + user_id` 查询完整限制状态的管理接口，供 NewAPI Dialog 与 ai-fund 本人视图直接使用，避免从活跃列表分页中猜测个人状态。
- 保留现有每日额度、并发列表和修改接口；新增字段和接口均向后兼容。
- NewAPI 管理端负责通用渠道管理；ai-fund `/pools` 管理渠道默认值与周期活跃列表，`/admin` 使用独立“号池限制”页签复用同一管理工作区。
- ai-fund 普通用户状态只读取服务端绑定的 NewAPI 用户 ID；管理员可通过 NewAPI 最小用户搜索选择任意已存在用户配置个人覆盖，不依赖门户绑定或请求历史。

## 实施边界

- 当前 NewAPI 任务先完成并稳定管理契约。
- ai-fund 当前任务已进入实现阶段；接入新契约前应按需求变更流程更新其 PRD、设计和实施清单，再在现有改动上增量实现。
- 两个仓库分别执行测试与构建，最后通过 Worker -> NewAPI 的只读和写入场景完成跨仓验证。
