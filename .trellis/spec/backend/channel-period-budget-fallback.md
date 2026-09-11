# 渠道周期预算、时间规则与额度降级契约

## 场景：统一个人、池子与自定义时段软额度

### 1. Scope / Trigger

- 修改渠道周期策略、池子/整段统计、个人规则特批、HTTP 超限降级或管理状态时读取本规范。
- 策略与执行唯一属于 NewAPI，ai-fund 只做映射、鉴权、金额换算、字段裁剪与审计。普通渠道编辑不携带周期策略。
- 本规范扩展旧日周规范：已有渠道无策略（revision=0）保留个人覆盖 max 语义；明确配置后按逐指标优先级解析。

### 2. Signatures

- `dto/channel_period_policy.go`：`ChannelPeriodPolicyConfig`、`ChannelPeriodRule`、`ChannelPeriodPolicyInput`、`ChannelPeriodStatus`。
- `model/channel_period_policy.go`：唯一 ChannelId、Revision 和 TEXT Config；`ReplaceChannelPeriodPolicy(ctx, policy)` 按旧版本 CAS，首次插入也检查唯一键竞争。
- `model/channel_user_period_override.go`：唯一 `(channel_id,user_id,rule_id)`，只保存规则个人整段额度和到期时间。
- `model/channel_quota_tracking.go`：渠道最近一次计数缺口时间，无金额副本。三表同时加入普通和快速迁移。
- `GetChannelPeriodStatus(ctx, channel, userID)` 返回权威指标；`CheckSelectedChannelPeriodLimits(c)` 由 Controller 与 Relay 旧统一检查点调用。
- `RecordChannelUserQuotaUsage(ctx, channelID, userID, quota)` 原子累计限额计数。
- GET/PUT `/api/channel/:id/period-policy`，POST `/period-policy/preview`，GET `/period-policy/targets`；GET/preview 为 ChannelRead，PUT 为 ChannelOperate。
- PUT/DELETE `/api/channel/:id/period-rules/:rule_id/user-overrides/:user_id`，ChannelOperate。

### 3. Contracts

- 写入为 `{expected_revision,config}`；完整显式字段，schema_version=1。默认池子日/周额度和规则四额度均受 `common.MaxQuota` 上界约束。
- 规则四项：`user_daily_quota_limit`、`pool_daily_quota_limit`、`user_period_quota_limit`、`pool_period_quota_limit`。null 继承、0 不限、正整数为软上限；不提供零额度封禁。
- 优先级逐项为个人明确覆盖 > date_range > weekly > default，未覆盖指标及池子上限仍约束。整段个人覆盖只作用于匹配 rule_id；不会绕过另一条更高优先级规则。
- date_range 使用服务器 time.Local 解释 start_local/end_local，保存 start_at/end_at；weekly 用周一=0 的星期和 HH:mm，支持跨周。区间 `[start,end)`，DST 不存在/歧义边界拒绝；同级同指标相交拒绝。
- ID/created_at 由服务端建立；名称/额度修改不换计数身份。正在计量的边界不得修改；停用、遮盖期间仍累计，删除转为停用。最多 64 条（含停用），预览不会为新规则保存身份。
- Redis key 保留旧个人日/周结构，新增池子日/周与规则 occurrence Hash 使用相同渠道 hash tag。Lua 预检所有类型与 int64 溢出后 HINCRBY；内存锁顺序固定为新增计数→个人日→个人周。Redis 故障不得改走内存。
- 请求仅检查 `used >= limit`，不预占。正向消费按资金结算成功时刻进入所有当时区间；零额、退款、负差额忽略。文本/音频/工具/任务/MJ 的调用必须在资金成功分支。人工修改个人 used 不改变池子/整段、账单或其他人。
- 配置缓存 5 秒，本地最多 4096 项；Redis 发布比较 revision，迟到的旧实例不能覆盖新版本。缓存保存失败返回 committed=true；TTL 期间仍可能存在旧配置，不宣称跨存储强一致。
- 统一状态新增 `period_limits`：schema/revision/timezone/storage_mode/next_change_at/fallback_enabled/blocked/metrics。指标含 scope、period、limit、used、remaining（不限为 null）、reset_at、tracking_since、coverage、enforced、source。
- 新计数始终说明 `since_tracking_start`；已知失败周期为 incomplete，不回填历史。Redis+DB 同时故障且进程丢失时未落盘缺口无法恢复；这不是财务对账或分布式事务。内存仅当前进程有效。
- 每来源只配置一个目标渠道+模型，默认关闭，仅精确 HTTP Chat/Messages/Responses 三入口可降级一次，与 RetryTimes 无关。目标重新检查 Token 模型、固定渠道、有效组 Ability、启用、端点、六类额度、资金和并发。
- 路由预检早于视觉辅助/预扣/源并发租约；已有普通尝试租约先收口。任何上游副作用、输出、资金会话或已降级状态禁止再切。目标失败不走第三渠道；正常恢复后的新请求使用原选渠。
- 适配器转换和最终出站 JSON 都必须保留全部客户端字段，例外仅顶层 model 和 false stream 省略。原生结构不一致、参数覆盖、上游会话/文件引用时保守拒绝。不靠厂商或模型名猜测兼容性。
- OriginModelName 保持原始审计值，RoutingModelName 表示目标，价格仍经 Resolve/FreezeBillingModelName；目标映射计费开关生效。阶梯表达式留到真实输入估算阶段，纯预检只判断分组免费语义。
- WebSocket/任务/MJ/Compact 等执行同一额度检查，不自动降级。成功日志 `other.admin_info.channel_limit_fallback` 保留来源、目标与原因；亲和性不把备用误记成原路由成功。

### 4. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| 不存在策略 | revision=0 默认；查询失败不能伪造不存在 |
| 版本过期 | 409 channel_period_policy_conflict，拒绝覆盖 |
| 越界/未知或缺字段/重叠/非法时间 | 管理 400 invalid_channel_period_policy |
| 池子或整段耗尽 | 429 channel_period_quota_exceeded，skipRetry；获准协调器可做一次额度降级 |
| 个人日/周耗尽 | 保留旧错误码；相同一次降级规则 |
| 策略/计数不可读 | 503 channel_period_quota_unavailable，不降级、不当作不限 |
| 目标无权/停用 | 403 channel_limit_fallback_unavailable，上游调用 0 |
| 无法保留目标请求字段 | 400 channel_limit_fallback_unavailable，上游调用 0 |
| 财务成功、计数失败 | 保留业务响应、记录缺口和安全告警，不重试不重复扣费 |

### 5. Scenarios and Examples

- 正常：周一至周五假期，个人每日 5、整段 15；前三天各用 5，第四天日计数刷新仍受整段约束。
- 边界：已用 80/100，两个已放行请求各结算 30，允许到 140，下一个请求才拒绝。
- 个人：假期日限 5、个人明确 20，实际为 20；池子已耗尽仍拒绝。到期回落当前规则。
- 错误：从人工可修改的个人日用量求和作为池子总量；正确：每次实际正向结算独立增加池子计数。
- 错误：改 OriginModelName 或删除 skipRetry 实现降级；正确：副作用前一次协调，保留原始模型并重新冻结目标价格。

### 6. Tests Required

- `service/channel_period_policy_test.go`：两种存储、假期/跨周/到期/遮盖/停用、140 软超额、人工调整隔离、整数原子性、缺口、缓存损坏/迟到发布、结算失败不累计。
- `model/channel_period_policy_database_test.go`：sqlite + 专用 MYSQL/POSTGRES DSN，重入迁移、CAS、upsert、缺口单调。不得用生产 DSN。
- `controller/channel_limit_fallback_test.go`：三协议 × 流/非流实际模拟上游、目标价格、唯一日志/钱包扣费、原始 DTO 不污染。
- `controller/channel_limit_fallback_boundary_test.go`：完整 Relay 的 retry=0、一次/禁用/恢复/目标限制与权限/不可迁移/Compact、并发释放、阶梯预检；WebSocket 回合准备、任务和 Midjourney 在启用降级时仍因池子耗尽拒绝，上游调用和扣款均为零。
- `controller/channel_period_policy_test.go`：实际管理响应合同与 400/409；ai-fund fixture 来自该响应。
- React 渠道测试、Vue 真实挂载、Worker 全量；两端 build、NewAPI typecheck/lint/i18n；relaykit 独立 build/vet。
