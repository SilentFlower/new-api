# 渠道周期预算、时间规则与额度降级契约

## 场景：统一个人、池子与自定义时段软额度

### 1. Scope / Trigger

- 修改渠道周期策略、池子/整段统计、个人规则特批、HTTP 超限降级或管理状态时读取本规范。
- 策略与执行唯一属于 NewAPI，ai-fund 只做映射、鉴权、金额换算、字段裁剪与审计。普通渠道编辑不携带周期策略。
- 本规范是预算 v2 的权威入口，覆盖配置、迁移、按行用量/提额和预算表交互。旧日周规范中的计数存储和正向结算约束继续适用，旧配置字段、独立检查和管理接口已由本规范替代。

### 2. Signatures

- `dto/channel_period_policy.go`：`ChannelPeriodPolicyConfig`、`ChannelBudgetSchedule`、`ChannelBudgetRow`、`ChannelBudgetAction`、`ChannelPeriodPolicyInput`、`ChannelPeriodStatus`；旧形状仅保留于 `dto/channel_period_policy_v1.go` 供兼容转换。
- `model/channel_period_policy.go`：唯一 ChannelId、Revision 和 TEXT Config；`ReplaceChannelPeriodPolicy(ctx, policy)` 按旧版本 CAS，首次插入也检查唯一键竞争。
- `model/channel_user_budget_override.go`：`ChannelUserBudgetOverride` 唯一 `(channel_id,user_id,budget_id)`，额度为 `quota_limit bigint`，另含 expires_at/updated_by/created_at/updated_at；旧日周和规则覆盖表仅供迁移及回滚保留。
- `model/channel_quota_tracking.go`：渠道最近一次计数缺口时间，无金额副本。预算覆盖表与策略、缺口表都必须进入普通和快速迁移。
- `GetChannelPeriodStatus(ctx, channel, userID)` 返回权威指标；`CheckSelectedChannelPeriodLimits(c)` 由 Controller 与 Relay 旧统一检查点调用。
- `RecordChannelUserQuotaUsage(ctx, channelID, userID, quota)` 原子累计限额计数；`RecordChannelUserModelQuotaUsage(ctx, channelID, userID, quota, modelName)` 带客户端原始模型名累计，前者委托后者并传空模型名。relay 入口 `RecordRelayChannelUserQuotaUsage` 传 `relayInfo.OriginModelName`，任务结算传 `task.Properties.OriginModelName`。
- 内核（`service/channel_budget_plan.go` / `channel_budget_usage.go` / `channel_budget_guard.go`）：`buildChannelBudgetPlan(view) channelBudgetPlan` 直接消费 v2；`resolveChannelBudgetRows(plan, now, modelName)` 解析时段状态、下一切换点与每组生效行；`newChannelBudgetCounter(channelID, row, res)` 把行映射到计数 key；`evaluateChannelBudgets(ctx, channel, userID, modelName)` 产出指标及对应行；`SelectChannelLimitFallback(config, sourceChannelID, block) (dto.ChannelLimitFallback, bool)` 选择降级目标。v1 投影 `resolveChannelPeriodSources` 已删除，预览使用 `PreviewChannelBudgetPolicy`。
- `GetChannelPeriodPolicy(ctx, channelID)` / `SaveChannelPeriodPolicy(ctx, channelID, input, updatedBy)` 读取或按版本保存策略；本地缓存发布由 `cacheChannelPeriodPolicy(key, data, revision) []byte` 返回选定版本。
- `controller/relay_attempt.go` 的 `cloneRelayRequest(request dto.Request)` 隔离每次尝试；`service.ChannelLimitFallbackRequestPortable(body []byte) bool` 检查实际协议引用。
- GET/PUT `/api/channel/:id/period-policy`，POST `/period-policy/preview`，GET `/period-policy/targets`；GET/preview 为 ChannelRead，PUT 为 ChannelOperate。
- GET/PUT `/api/channel/:id/budgets/:budget_id/usage`，分别为 ChannelRead / ChannelOperate；PUT/DELETE `/api/channel/:id/budgets/:budget_id/user-overrides/:user_id` 为 ChannelOperate；GET `/api/channel/:id/budget-user-overrides` 为 ChannelRead。
- `GetChannelBudgetUsage(ctx, channelID, budgetID, scope, offset, limit)` / `SetChannelBudgetUsage(ctx, channelID, budgetID, input)`；`ReplaceChannelUserBudgetOverride(ctx, channelID, userID, budgetID, input, updatedBy)` / `DeleteChannelUserBudgetOverride(ctx, channelID, userID, budgetID)`。
- `MigrateChannelBudgetPolicies(ctx)` 在主节点数据库迁移完成后执行；`model.MigrateChannelUserOverridesToBudgets(ctx, now)` 按唯一键只插入缺失覆盖。

### 3. Contracts

- 写入为 `{expected_revision,config}`；config 完整显式包含 `{schema_version:2,default_on_exceed,schedules,budgets}`。`default_on_exceed` 为 `{mode:reject|fallback,channel_id,model}`；行级动作额外允许 inherit。额度为 int64，`limit` 为 `0..common.MaxPeriodQuota`（10^15，保持在 2^53 内）；0 表示不限，不提供零额度封禁。null 不再表示行额度继承。
- 时段只描述日期/每周区间，额度均由预算行承载；`models` 去空白、去重、排序，空数组匹配所有模型，非空按原始模型名精确匹配。行名 1..80 字；最多 128 行、64 个时段（均含停用）；daily/weekly 可引用时段，occurrence 必须引用存在的时段。
- 分组键 `(scope,window,models)` 内按 date_range > weekly > 无时段行解析，再应用个人覆盖。日/周个人提额可覆盖同分组的当前生效行，按计划行顺序取第一个有效提额；occurrence 提额只作用匹配 budget_id。其他分组及池子上限继续约束。
- date_range 使用服务器 time.Local 解释 start_local/end_local，保存 start_at/end_at；weekly 用周一=0 的星期和 HH:mm，支持跨周。区间 `[start,end)`，DST 不存在/歧义边界拒绝；同级同指标相交拒绝。
- ID/created_at 由服务端建立；新行和时段可携带 `new-` 临时 ID，同一请求的引用由服务端归一化。名称/额度修改不换计数身份；模型、窗口或 occurrence 时段身份变化时更新行 created_at，新身份尚无计数时据此计算 tracking_since，已有共享计数仍复用其起点。正在计量的时段边界不得修改；停用、遮盖期间仍累计，已保存行/时段从提交列表删除会转为停用，预览不持久化身份。
- Redis key 保留旧个人日/周结构，新增池子日/周与规则 occurrence Hash 使用相同渠道 hash tag。Lua 预检所有类型与 int64 溢出后 HINCRBY；内存锁顺序固定为新增计数→个人日→个人周。Redis 故障不得改走内存。
- 预算行完整字段为 `{id,name,enabled,scope:user|pool,window:daily|weekly|occurrence,schedule_id,models,limit,on_exceed:{mode,channel_id,model},created_at}`。迁移派生 ID 固定为 `legacy-user-daily` / `legacy-user-weekly` / `legacy-pool-daily` / `legacy-pool-weekly` 与 `rule-<ruleID>-{user,pool}-{daily,occurrence}`；v1 未填写的规则额度不生成行。计数身份 `(window, occurrence 时的 schedule_id, models)`：无模型的 daily/weekly 身份个人字段在旧 `channel_user_{daily,weekly}_quota` key、`__pool` 在 `channel_pool_{daily,weekly}_quota` key；无模型 occurrence 身份两者同为 `channel_period_quota:{cid}:{ruleID}:{occStart}`；其余身份为 `channel_budget:{cid}:{sha1(window|schedule|models) 前 12 位}:{windowStart}`。同身份行共享计数，范围或额度不同不意味着新计数。
- 累计脚本 ARGV 固定为 `(userID, delta, now)`，其后每个 KEY 依次携带 `(expireAt, since, poolFlag)`；`poolFlag=1` 时同时累加 `__pool` 并 `HSETNX __since`。停用规则的 occurrence 仍计数但不展示、不拦截；请求前检查只解析命中原始模型名的行，管理端展示传空模型名取全部行。
- 旧错误码只属于 `scope=user` 且 `models` 为空的行（daily → `channel_user_daily_quota_exceeded`，weekly → `channel_user_weekly_quota_exceeded`）；按模型的行超限统一返回 `channel_period_quota_exceeded`。行级 `on_exceed` 优先，inherit 回落 `default_on_exceed`；行级 `channel_id=0` 表示同渠道换模型，仅允许 models 非空且目标不在该模型集合内。保存时显式指向来源渠道的行级目标也归一为 0；策略级禁止指向自身。
- `CheckSelectedChannelPeriodLimits` 不再按 revision=0 早返回，不回写旧日周上下文键；Controller/Relay 的统一入口及降级目标预检始终使用预算判定。旧日周独立检查、RelayInfo 日周快照字段与对应上下文键已删除。
- 请求仅检查 `used >= limit`，不预占。正向消费按资金结算成功时刻进入所有当时区间；零额、退款、负差额忽略。文本/音频/工具/任务/MJ 的调用必须在资金成功分支。人工修改个人 used 不改变池子/整段、账单或其他人。
- 配置缓存 5 秒，本地最多 4096 项；Redis 发布比较 revision，迟到的旧实例不能覆盖新版本。缓存保存失败返回 committed=true；TTL 期间仍可能存在旧配置，不宣称跨存储强一致。
- 策略缓存的进程级锁只保护本地 map 读取和发布，不跨 Redis GET/Lua 或数据库查询、保存。锁外查询可能迟到，本地发布也必须比较 revision；缓存仍持有更高版本时返回该版本，不覆盖为旧值。数据库保存继续以 revision CAS 防止并发编辑覆盖。
- 统一状态保留并发与 v2 `period_limits`，移除旧日/周状态块；period_limits 含 schema_version/revision/timezone/storage_mode/next_change_at/fallback_enabled/blocked/metrics。指标含 budget_id/budget_name/models、scope、period（daily/weekly/occurrence）、limit/used/remaining（不限为 null）、reset_at/tracking_since/coverage/enforced/source/base_limit，可选 override_limit；时段身份在 source.schedule_id/schedule_name。
- 用量 GET 接受 `scope=user|pool` 和 `p`/page_size，分页使用 common.GetPageQuery（page_size 兼容 ps/size，上限 100）；user 返回 items 的 user_id/username/display_name/used_quota 及分页信息，按用量降序、同值按用户 ID 升序；pool 返回独立汇总 used_quota，均含 window_start/window_end/tracking_since/storage_mode。PUT 为 `{scope,user_id,used_quota}`，设置绝对目标而非增量；只调整选定计数身份的用户字段或池子汇总，不改 revision/资金。旧个人无模型日/周 used_quota 仍不得超过 common.MaxQuota，其他目标不得超过 common.MaxPeriodQuota。审计 `channel.budget_usage_set` 必须含 before/after，前值读取故障停止写入，不能伪记 0。
- 提额 PUT 为 `{limit,expires_at}`，只允许 user 行，必须高于该行基础正额度且不超过 MaxPeriodQuota；expires_at=0 永久，正数必须在未来。PUT/DELETE 均不改策略 revision，审计 `channel.budget_user_override_set/delete` 含 before/after。撤销在新表写 quota_limit=0 的非生效记录，查询/分页排除它，重启迁移遇唯一键不覆盖，重新提额可替换；不得物理删除后让旧表数据重新导入。
- 主节点启动迁移仅转换明确 schema_version=1 或真正无策略记录的旧列输入；v2 跳过，单渠道失败记录并继续，重复启动不提升已迁移 revision。未知/缺失版本、null、空正文的已存储策略不得改写或当成不限，读取返回错误。无策略记录仍可产生 revision=0 的旧列派生视图，保存 v2 后旧列不再参与判定。
- 渠道日/周列和旧覆盖字段仅以 `json:"-"` 保留供迁移；普通创建/更新不再读写或校验。旧 user-daily-quota、user-weekly-quota、period-rules 特批端点返回 404；user-limit-overrides 仅保留并发与 expires_at，携带旧日/周字段返回 400。
- 回滚需回退代码并恢复上线前的三张旧表备份（策略、日周覆盖、规则覆盖）；新预算覆盖表恢复到上线前状态，首次升级前不存在时移除迁移创建的新表。不能只回退代码读取 v2，也不能只用新代码的 v1 兼容读取证明旧版可用；演练必须实际编译旧版，验证恢复后预算内放行和到限拦截。两仓同版本发布，ai-fund 使用实际管理响应生成的 v2 fixture 对齐。
- 预算表展示独立时段列及已用/剩余进度：池子为汇总，个人明确标注当前窗口最高用量用户，进度使用其统一状态中的有效额度。未保存临时行不查询用量，读取错误显示重试；编辑抽屉与父表共享草稿，关闭/Esc 保留值，模型输入保留原始文本，字段通过 form 属性关联父表单；策略级默认动作在表下方，整份策略先预览再按 revision 保存。
- 新计数始终说明 `since_tracking_start`；已知失败周期为 incomplete，不回填历史。Redis+DB 同时故障且进程丢失时未落盘缺口无法恢复；这不是财务对账或分布式事务。内存仅当前进程有效。
- 每次超限只选择一个目标渠道+模型，默认拒绝，仅精确 HTTP Chat/Messages/Responses 三入口可降级一次，与 RetryTimes 无关。目标重新检查 Token 模型、固定渠道、有效组 Ability、启用、端点、全部适用预算、资金和并发。
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
| 不存在策略 | revision=0 旧列派生视图，继续统一判定；查询失败不能伪造不存在 |
| 版本过期 | 409 channel_period_policy_conflict，拒绝覆盖 |
| 越界/未知或缺字段/重叠/非法时间 | 管理 400 invalid_channel_period_policy |
| 已存储策略未知/缺失版本或空正文 | 读取 503；迁移记录失败并保留原数据 |
| user-limit-overrides 携带旧日/周字段 | HTTP 400 且 success=false，不能只在 HTTP 200 正文中标记失败 |
| 提额不高于基础正额度，或对 pool 行提额/撤销 | 400，不写覆盖或成功审计 |
| used_quota 超过其计数支持上界 | 400，不调整计数 |
| 调整前值读取失败 | 503，不调整计数、不写成功审计 |
| 撤销旧提额后重启迁移 | 提额保持撤销，分页不返回零额度占位；重新授权不被旧值覆盖 |
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
- 正常：基础日预算 100，`PUT /api/channel/80/budgets/legacy-user-daily/user-overrides/77` 提交 `{"limit":200,"expires_at":0}` 后显示 200；DELETE 后回到当前组基础值，重启仍保持撤销。
- 边界：Astra 行更换为此前未计数的 Mini 模型集合，用量从新身份读取且 tracking_since 不早于变更时间；只改名或调额继续使用原起点。
- 错误：把所有非 v2 正文当成 v1，或把审计前值读取错误当作 0。正确：仅显式 v1 进入兼容转换，异常返回错误，保留原策略/计数并停止成功写入。

### 6. Tests Required

- `service/channel_period_policy_test.go`：两种存储、假期/跨周/到期/遮盖/停用、140 软超额、人工调整隔离、整数原子性、缺口、缓存损坏/迟到发布、结算失败不累计。
- `service/channel_budget_plan_test.go`：无模型身份的 userKey/poolKey 与旧 key 一致、时段日限与无时段日限共享计数、同组优先级与 next 切换点；模型精确匹配且与整体预算并行；降级 inherit/reject/同渠道/指定渠道；内存与 Redis 指标一致。`service/channel_budget_migrate_test.go` 保护派生 ID、归一化和 v2 校验矩阵。
- `service/channel_period_policy_concurrency_test.go`：用显式屏障暂停存储访问，验证不同渠道 Redis 读取不互相阻塞；内存/Redis 均验证旧数据库快照恢复后返回新版且不覆盖新版缓存，并执行 race 检查。
- `service/channel_limit_fallback_portability_test.go`：三协议工具 Schema、结构化输出、metadata、Claude 工具调用参数可迁移；真实会话、文件、附件、工具资源和密文引用仍不可迁移。
- `model/channel_period_policy_database_test.go`：sqlite + 专用 MYSQL/POSTGRES DSN，重入迁移、CAS、upsert、缺口单调。不得用生产 DSN。
- `service/channel_budget_migrate_database_test.go`：三库完整 v1 策略/旧列/两类覆盖迁移等价，重复启动 revision 不变，撤销后重启不复活，重新授权不被覆盖；专用 CHANNEL_PERIOD_TEST_MYSQL_DSN / CHANNEL_PERIOD_TEST_POSTGRES_DSN 缺失时只能记部分验证。
- `service/channel_budget_repair_test.go`：未知/缺失版本与空正文读取失败、迁移不改原数据；三种周期 × 内存/Redis 的模型变更和改名调额统计起点；用户用量排序与跨页同值顺序。
- `controller/channel_budget_test.go`：用量与提额的参数边界、隔离、revision 不变、真实 before/after 审计、撤销 pool 行拒绝；旧日/周提额字段分别断言 HTTP 400 与 success=false；Redis 审计读取故障后计数不变且无成功日志。
- `controller/channel_limit_fallback_test.go`：三协议 × 流/非流实际模拟上游、目标价格、唯一日志/钱包扣费、原始 DTO 不污染。
- `controller/channel_limit_fallback_preflight_test.go`：完整 Relay 覆盖 Responses 别名的预算正值/零值/缺省及已配置但未触发降级；自定义目标直接访问与降级访问的实际路径、模型、唯一账单、钱包扣费，以及工具同名参数完整保留。
- `controller/relay_attempt_responses_test.go`：先向非 Qwen 模型转换，再从原请求向 Qwen 重试，验证出站过滤仍生效且显式零值、原始输入和指针/切片未被前次尝试污染。
- `controller/channel_limit_fallback_compact_test.go`：只配置基础模型价格，完整 Relay 覆盖 V1 path、历史 body bridge、V2 HTTP 的正常透传、额度耗尽和能力关闭；断言原始请求/响应、唯一基础模型账单和钱包扣款，拒绝时零上游/消费且不调用备用渠道。
- `controller/channel_limit_fallback_boundary_test.go`：完整 Relay 的 retry=0、一次/禁用/恢复/目标限制与权限/不可迁移/Compact、并发释放、阶梯预检；WebSocket 回合准备、任务和 Midjourney 在启用降级时仍因池子耗尽拒绝，上游调用和扣款均为零。
- `controller/channel_period_policy_test.go`：实际管理响应合同与 400/409；ai-fund fixture 来自该响应。
- `controller/channel_limit_fallback_model_test.go`：真实 Relay → 模拟上游 → 结算/日志，覆盖按模型池子/个人日周、同渠道模型切换按目标价格扣费、目标个人限额阻断且不二跳。
- `web/src/features/channels/components/dialogs/__tests__/channel-period-policy-panel.test.tsx`：预算表时段/进度、抽屉关闭和 Esc 保留草稿、提额后的个人进度、读取失败重试、新行不查用量、版本冲突与跨渠道迟到响应。
- React 渠道测试、Vue 真实挂载、Worker 全量；两端 build、NewAPI typecheck/lint/i18n；relaykit 独立 build/vet。
