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
- `RecordChannelUserQuotaUsage(ctx, channelID, userID, quota)` 原子累计限额计数；`RecordChannelUserModelQuotaUsage(ctx, channelID, userID, quota, modelName)` 带客户端原始模型名累计，前者委托后者并传空模型名。relay 入口 `RecordRelayChannelUserQuotaUsage` 传 `relayInfo.OriginModelName`，任务结算传 `task.Properties.OriginModelName`。
- 内核（`service/channel_budget_plan.go` / `channel_budget_usage.go` / `channel_budget_guard.go`）：`buildChannelBudgetPlan(view, userDaily, userWeekly) channelBudgetPlan` 由 v1 策略与渠道列派生预算行；`resolveChannelBudgetRows(plan, now, modelName)` 解析时段状态、下一切换点与每组生效行；`newChannelBudgetCounter(channelID, row, res)` 把行映射到计数 key；`evaluateChannelBudgets(ctx, channel, userID, modelName)` 产出指标及对应行；`SelectChannelLimitFallback(config, sourceChannelID, block) (dto.ChannelLimitFallback, bool)` 选择降级目标。`resolveChannelPeriodSources` 只是该计划的 v1 五槽位投影，供 v1 预览与旧个人覆盖校验。
- `GetChannelPeriodPolicy(ctx, channelID)` / `SaveChannelPeriodPolicy(ctx, channelID, input, updatedBy)` 读取或按版本保存策略；本地缓存发布由 `cacheChannelPeriodPolicy(key, data, revision) []byte` 返回选定版本。
- `controller/relay_attempt.go` 的 `cloneRelayRequest(request dto.Request)` 隔离每次尝试；`service.ChannelLimitFallbackRequestPortable(body []byte) bool` 检查实际协议引用。
- GET/PUT `/api/channel/:id/period-policy`，POST `/period-policy/preview`，GET `/period-policy/targets`；GET/preview 为 ChannelRead，PUT 为 ChannelOperate。
- PUT/DELETE `/api/channel/:id/period-rules/:rule_id/user-overrides/:user_id`，ChannelOperate。

### 3. Contracts

- 写入为 `{expected_revision,config}`；完整显式字段，schema_version=1。默认池子日/周额度、规则四额度和整段特批均为 int64，受 `common.MaxPeriodQuota`（10^15，保持在 2^53 内）上界约束；它们只存于策略 JSON 与计数，不受 32 位 quota 列的 `common.MaxQuota` 限制。旧个人日/周覆盖仍受 `common.MaxQuota` 约束。写回 `ContextKeyChannelUserDailyQuotaLimit` / `WeeklyQuotaLimit` 时必须转为 `int`，gin 的 `GetInt` 对 int64 返回 0。
- 规则四项：`user_daily_quota_limit`、`pool_daily_quota_limit`、`user_period_quota_limit`、`pool_period_quota_limit`。null 继承、0 不限、正整数为软上限；不提供零额度封禁。
- 优先级逐项为个人明确覆盖 > date_range > weekly > default，未覆盖指标及池子上限仍约束。整段个人覆盖只作用于匹配 rule_id；不会绕过另一条更高优先级规则。
- date_range 使用服务器 time.Local 解释 start_local/end_local，保存 start_at/end_at；weekly 用周一=0 的星期和 HH:mm，支持跨周。区间 `[start,end)`，DST 不存在/歧义边界拒绝；同级同指标相交拒绝。
- ID/created_at 由服务端建立；名称/额度修改不换计数身份。正在计量的边界不得修改；停用、遮盖期间仍累计，删除转为停用。最多 64 条（含停用），预览不会为新规则保存身份。
- Redis key 保留旧个人日/周结构，新增池子日/周与规则 occurrence Hash 使用相同渠道 hash tag。Lua 预检所有类型与 int64 溢出后 HINCRBY；内存锁顺序固定为新增计数→个人日→个人周。Redis 故障不得改走内存。
- 预算行内核：每行 `{id, scope: user|pool, window: daily|weekly|occurrence, schedule_id, models, limit, on_exceed: inherit|reject|fallback, created_at}`。v1 派生 id 固定为 `legacy-user-daily` / `legacy-user-weekly` / `legacy-pool-daily` / `legacy-pool-weekly` 与 `rule-<ruleID>-{user,pool}-{daily,occurrence}`；规则未填写的整段额度生成 `implicit` 行，只保持状态形状，不参与生效。分组键 `(scope, window, models)` 内按个人覆盖 > date_range 时段行 > weekly 时段行 > 无时段行取一条生效，同级后者覆盖前者；不同分组并行约束。计数身份 `(window, occurrence 时的 schedule_id, models)`：无模型的 daily/weekly 身份个人字段在旧 `channel_user_{daily,weekly}_quota` key、`__pool` 在 `channel_pool_{daily,weekly}_quota` key；无模型 occurrence 身份两者同为 `channel_period_quota:{cid}:{ruleID}:{occStart}`；其余身份为 `channel_budget:{cid}:{sha1(window|schedule|models) 前 12 位}:{windowStart}`。规则内日限与无时段日限同身份，只改上限不换计数；修改行的 `models` 即换身份。
- 累计脚本 ARGV 固定为 `(userID, delta, now)`，其后每个 KEY 依次携带 `(expireAt, since, poolFlag)`；`poolFlag=1` 时同时累加 `__pool` 并 `HSETNX __since`。停用规则的 occurrence 仍计数但不展示、不拦截；请求前检查只解析命中原始模型名的行，管理端展示传空模型名取全部行。
- 旧错误码只属于 `scope=user` 且 `models` 为空的行（daily → `channel_user_daily_quota_exceeded`，weekly → `channel_user_weekly_quota_exceeded`）；按模型的行超限统一返回 `channel_period_quota_exceeded`。降级动作按“行级 `on_exceed` 优先、`inherit` 回落策略级 `fallback`”选择，行级 `channel_id=0` 表示同渠道换模型，此时目标渠道为来源渠道。
- 阶段一契约冻结：指标 JSON 不新增字段，`period` 仍输出 `custom`；`CheckSelectedChannelPeriodLimits` 保留 revision=0 早返回并继续把渠道级个人日/周上限写回 `ContextKeyChannelUserDailyQuotaLimit/Weekly`；`relay/channel_user_{daily,weekly}_quota.go` 与降级预检里的旧检查保留，因为降级目标渠道可能没有策略。这些删除在 v2 迁移完成后进行。
- 请求仅检查 `used >= limit`，不预占。正向消费按资金结算成功时刻进入所有当时区间；零额、退款、负差额忽略。文本/音频/工具/任务/MJ 的调用必须在资金成功分支。人工修改个人 used 不改变池子/整段、账单或其他人。
- 配置缓存 5 秒，本地最多 4096 项；Redis 发布比较 revision，迟到的旧实例不能覆盖新版本。缓存保存失败返回 committed=true；TTL 期间仍可能存在旧配置，不宣称跨存储强一致。
- 策略缓存的进程级锁只保护本地 map 读取和发布，不跨 Redis GET/Lua 或数据库查询、保存。锁外查询可能迟到，本地发布也必须比较 revision；缓存仍持有更高版本时返回该版本，不覆盖为旧值。数据库保存继续以 revision CAS 防止并发编辑覆盖。
- 统一状态新增 `period_limits`：schema/revision/timezone/storage_mode/next_change_at/fallback_enabled/blocked/metrics。指标含 scope、period、limit、used、remaining（不限为 null）、reset_at、tracking_since、coverage、enforced、source。
- 新计数始终说明 `since_tracking_start`；已知失败周期为 incomplete，不回填历史。Redis+DB 同时故障且进程丢失时未落盘缺口无法恢复；这不是财务对账或分布式事务。内存仅当前进程有效。
- 每来源只配置一个目标渠道+模型，默认关闭，仅精确 HTTP Chat/Messages/Responses 三入口可降级一次，与 RetryTimes 无关。目标重新检查 Token 模型、固定渠道、有效组 Ability、启用、端点、六类额度、资金和并发。
- 路由预检早于视觉辅助/预扣/源并发租约；已有普通尝试租约先收口。任何上游副作用、输出、资金会话或已降级状态禁止再切。目标失败不走第三渠道；正常恢复后的新请求使用原选渠。
- 原始请求和每次尝试的 Responses 克隆不得调用其带业务过滤的 `MarshalJSON`：模型映射前的别名不能决定是否删除 `thinking_budget`。克隆使用无该方法的局部定义类型后进行 JSON 深复制；最终出站仍按映射后模型过滤。显式零值和切片、指针的尝试隔离必须保留，包括未配置降级的普通请求。
- 适配器转换和最终出站 JSON 都必须保留全部客户端字段，例外仅顶层 model 和 false stream 省略。原生结构不一致、参数覆盖、上游会话/文件引用时保守拒绝。不靠厂商或模型名猜测兼容性。
- 引用检查只递归进入消息、输入、内容、附件及工具资源等协议位置；工具 Schema、结构化输出 Schema、metadata 和函数业务参数中的 `prompt` / `file_id` / `conversation` 等同名字段不构成上游引用。Claude `tool_use` / `server_tool_use` 的 `input` 是业务参数，顶层 Responses `input` 是协议内容；真实会话、文件、容器、提示模板引用与压缩密文仍拒绝迁移。
- OriginModelName 保持原始审计值，RoutingModelName 表示目标，价格仍经 Resolve/FreezeBillingModelName；目标映射计费开关生效。Advanced Custom 的预检转换和实际发送都使用 `info.RoutingModel()` 匹配模型路由，不能重新按来源模型匹配。阶梯表达式留到真实输入估算阶段，纯预检只判断分组免费语义。
- WebSocket/任务/MJ/Compact 等执行同一额度检查，不自动降级。成功日志 `other.admin_info.channel_limit_fallback` 保留来源、目标与原因；亲和性不把备用误记成原路由成功。
- 降级预检必须先用 `relay.ShouldHandleResponsesCompactPassthrough(info)` 排除全部 Compact 模式，再查询策略或执行模型映射/查价。历史 body bridge 与 V2 HTTP 也使用 `/v1/responses`，只按 URL 排除 `/v1/responses/compact` 会误查旧的 `*-openai-compact` 价格；这些请求继续由独立 Compact 准备和 `prepareMainRelayBilling` 执行基础模型计费、能力及额度门禁。

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
| 仅工具 Schema 或业务参数含引用同名字段 | 不得仅凭字段名拒绝降级，继续目标权限、接口及字段保留校验 |
| 请求含真实上游会话、文件或加密引用 | 保留来源额度错误，不调用备用渠道 |
| 锁外旧查询恢复时缓存已有更高版本 | 返回缓存选定的新版本，不发布旧策略覆盖新策略 |
| 财务成功、计数失败 | 保留业务响应、记录缺口和安全告警，不重试不重复扣费 |

### 5. Scenarios and Examples

- 正常：周一至周五假期，个人每日 5、整段 15；前三天各用 5，第四天日计数刷新仍受整段约束。
- 边界：已用 80/100，两个已放行请求各结算 30，允许到 140，下一个请求才拒绝。
- 个人：假期日限 5、个人明确 20，实际为 20；池子已耗尽仍拒绝。到期回落当前规则。
- 错误：从人工可修改的个人日用量求和作为池子总量；正确：每次实际正向结算独立增加池子计数。
- 错误：改 OriginModelName 或删除 skipRetry 实现降级；正确：副作用前一次协调，保留原始模型并重新冻结目标价格。
- 错误：别名 `alias-model` 的 Responses 请求先调用原 DTO 的序列化方法克隆，导致映射到 `qwen3-32b` 前丢失 `thinking_budget:0`；正确：克隆保留该值，最终发送时按映射后模型处理。
- 正常：来源 `gpt-4o-mini` 超限后，按目标 `gpt-4o` 匹配 Advanced Custom 路由，唯一账单归目标；工具参数名为 `prompt` 或 `file_id` 时也可在全部校验通过后降级。

### 6. Tests Required

- `service/channel_period_policy_test.go`：两种存储、假期/跨周/到期/遮盖/停用、140 软超额、人工调整隔离、整数原子性、缺口、缓存损坏/迟到发布、结算失败不累计。
- `service/channel_budget_plan_test.go`：v1 派生身份的 userKey/poolKey 与旧 key 逐字节一致、规则内日限与无时段日限同身份、implicit 行不生效、同组优先级、`next` 切换点、计数身份去重顺序；模型行仅精确命中且与渠道级行并行、`channel_budget` key 与 12 位哈希；`SelectChannelLimitFallback` 的 nil block / inherit / reject / 同渠道 / 指定渠道；同一结算序列在内存与 Redis 下指标相等。
- `service/channel_period_policy_concurrency_test.go`：用显式屏障暂停存储访问，验证不同渠道 Redis 读取不互相阻塞；内存/Redis 均验证旧数据库快照恢复后返回新版且不覆盖新版缓存，并执行 race 检查。
- `service/channel_limit_fallback_portability_test.go`：三协议工具 Schema、结构化输出、metadata、Claude 工具调用参数可迁移；真实会话、文件、附件、工具资源和密文引用仍不可迁移。
- `model/channel_period_policy_database_test.go`：sqlite + 专用 MYSQL/POSTGRES DSN，重入迁移、CAS、upsert、缺口单调。不得用生产 DSN。
- `controller/channel_limit_fallback_test.go`：三协议 × 流/非流实际模拟上游、目标价格、唯一日志/钱包扣费、原始 DTO 不污染。
- `controller/channel_limit_fallback_preflight_test.go`：完整 Relay 覆盖 Responses 别名的预算正值/零值/缺省及已配置但未触发降级；自定义目标直接访问与降级访问的实际路径、模型、唯一账单、钱包扣费，以及工具同名参数完整保留。
- `controller/relay_attempt_responses_test.go`：先向非 Qwen 模型转换，再从原请求向 Qwen 重试，验证出站过滤仍生效且显式零值、原始输入和指针/切片未被前次尝试污染。
- `controller/channel_limit_fallback_compact_test.go`：只配置基础模型价格，完整 Relay 覆盖 V1 path、历史 body bridge、V2 HTTP 的正常透传、额度耗尽和能力关闭；断言原始请求/响应、唯一基础模型账单和钱包扣款，拒绝时零上游/消费且不调用备用渠道。
- `controller/channel_limit_fallback_boundary_test.go`：完整 Relay 的 retry=0、一次/禁用/恢复/目标限制与权限/不可迁移/Compact、并发释放、阶梯预检；WebSocket 回合准备、任务和 Midjourney 在启用降级时仍因池子耗尽拒绝，上游调用和扣款均为零。
- `controller/channel_period_policy_test.go`：实际管理响应合同与 400/409；ai-fund fixture 来自该响应。
- React 渠道测试、Vue 真实挂载、Worker 全量；两端 build、NewAPI typecheck/lint/i18n；relaykit 独立 build/vet。
