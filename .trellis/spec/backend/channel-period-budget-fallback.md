# 渠道周期预算、时间规则与额度降级契约

## 场景：统一个人、池子与自定义时段软额度

### 1. Scope / Trigger

- 修改渠道周期策略、池子/整段统计、模型用量持续累计开关、个人规则特批、HTTP 超限降级或管理状态时读取本规范。
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
- `controller/relay_attempt.go` 的 `cloneRelayRequest(request dto.Request)` 隔离每次尝试；额度降级不在本地判断原始请求中的上游状态是否可移植。
- GET/PUT `/api/channel/:id/period-policy`，POST `/period-policy/preview`，GET `/period-policy/targets`；GET/preview 为 ChannelRead，PUT 为 ChannelOperate。
- GET/PUT `/api/channel/:id/budgets/:budget_id/usage`，分别为 ChannelRead / ChannelOperate；PUT/DELETE `/api/channel/:id/budgets/:budget_id/user-overrides/:user_id` 为 ChannelOperate；GET `/api/channel/:id/budget-user-overrides` 为 ChannelRead。
- `GetChannelBudgetUsage(ctx, channelID, budgetID, scope, offset, limit)` / `SetChannelBudgetUsage(ctx, channelID, budgetID, input)`；`ReplaceChannelUserBudgetOverride(ctx, channelID, userID, budgetID, input, updatedBy)` / `DeleteChannelUserBudgetOverride(ctx, channelID, userID, budgetID)`。
- GET `/api/channel/:id/budgets/usage-summary` 为 ChannelRead（`controller/channel_budget_usage_summary.go`）；`service.GetChannelBudgetUsageSummary(ctx, channel *model.Channel) (dto.ChannelBudgetUsageSummaryView, error)` 复用 `resolveChannelBudgetRows` / `readChannelBudgetCounter` / `listChannelBudgetUsage` / `GetChannelPeriodStatus`，不新增存储访问路径；DTO 为 `ChannelBudgetUsageSummaryView` / `ChannelBudgetUsageSummaryItem` / `ChannelBudgetUsageTopUser`。
- 前端纯函数（`web/src/features/channels/lib/`）：`channel-budget-status.ts` 的 `deriveChannelBudgetStatus` / `groupChannelBudgetCounters` / `channelBudgetEffectiveLimit`；`channel-schedule-state.ts` 的 `resolveChannelScheduleStates(schedules, previewRows, now, timezone)`；`channel-policy-error-rows.ts` 的 `matchChannelPolicyErrorRows(message, rows)` / `channelPolicyErrorDetail(message)`。
- `MigrateChannelBudgetPolicies(ctx)` 在主节点数据库迁移完成后执行；`model.MigrateChannelUserOverridesToBudgets(ctx, now)` 按唯一键只插入缺失覆盖。
- 模型累计扩展仍使用上述 v2 API：`ChannelPeriodPolicyConfig.ModelUsageTrackingEnabled *bool`、`ModelUsageTracking *ChannelModelUsageTracking` 与 `ChannelBudgetRow.CounterSource string` 均可缺省。`service/channel_model_usage.go` 负责来源归一化、原始事实与聚合读取；`channel_model_usage_adjustment.go` 的 `setChannelModelBudgetUsage(ctx, counter, scope, userID, used, now)` 保存绝对用量调整快照。

### 3. Contracts

- 写入为 `{expected_revision,config}`；config 完整显式包含 `{schema_version:2,default_on_exceed,schedules,budgets}`。`default_on_exceed` 为 `{mode:reject|fallback,channel_id,model}`；行级动作额外允许 inherit。额度为 int64，`limit` 为 `0..common.MaxPeriodQuota`（10^15，保持在 2^53 内）；0 表示不限，不提供零额度封禁。null 不再表示行额度继承。
- config 额外允许 `model_usage_tracking_enabled:boolean` 和 `model_usage_tracking:{first_enabled_at,enabled_at,disabled_at}`，预算行额外允许 `counter_source:"continuous_model"`；均不是旧 v2 的必填字段。无历史配置默认关闭，旧客户端保存时省略开关须保留服务端旧值，显式 false 才表示关闭。切换时间与来源由服务端根据旧配置确定，客户端不能用回传元数据改写历史；持久化仍用原 TEXT 配置，无新增表或环境变量。
- 时段只描述日期/每周区间，额度均由预算行承载；`models` 去空白、去重、排序，空数组匹配所有模型，非空按原始模型名精确匹配。行名 1..80 字；最多 128 行、64 个时段（均含停用）；daily/weekly 可引用时段，occurrence 必须引用存在的时段。
- 分组键 `(scope,window,models)` 内按 date_range > weekly > 无时段行解析，再应用个人覆盖。日/周个人提额可覆盖同分组的当前生效行，按计划行顺序取第一个有效提额；occurrence 提额只作用匹配 budget_id。其他分组及池子上限继续约束。
- date_range 使用服务器 time.Local 解释 start_local/end_local，保存 start_at/end_at；weekly 用周一=0 的星期和 HH:mm，支持跨周。区间 `[start,end)`，DST 不存在/歧义边界拒绝；同级同指标相交拒绝。
- ID/created_at 由服务端建立；新行和时段可携带 `new-` 临时 ID，同一请求的引用由服务端归一化。名称/额度修改不换计数身份；模型、窗口或 occurrence 时段身份变化时更新行 created_at，新身份尚无计数时据此计算 tracking_since，已有共享计数仍复用其起点。正在计量的时段边界不得修改；停用、遮盖期间仍累计，已保存行/时段从提交列表删除会转为停用，预览不持久化身份。
- Redis key 保留旧个人日/周结构，新增池子日/周与规则 occurrence Hash 使用相同渠道 hash tag。Lua 预检所有类型与 int64 溢出后 HINCRBY；内存锁顺序固定为新增计数→个人日→个人周。Redis 故障不得改走内存。
- 预算行完整字段为 `{id,name,enabled,scope:user|pool,window:daily|weekly|occurrence,schedule_id,models,limit,on_exceed:{mode,channel_id,model},created_at}`。迁移派生 ID 固定为 `legacy-user-daily` / `legacy-user-weekly` / `legacy-pool-daily` / `legacy-pool-weekly` 与 `rule-<ruleID>-{user,pool}-{daily,occurrence}`；v1 未填写的规则额度不生成行。计数身份 `(window, occurrence 时的 schedule_id, models)`：无模型的 daily/weekly 身份个人字段在旧 `channel_user_{daily,weekly}_quota` key、`__pool` 在 `channel_pool_{daily,weekly}_quota` key；无模型 occurrence 身份两者同为 `channel_period_quota:{cid}:{ruleID}:{occStart}`；其余身份为 `channel_budget:{cid}:{sha1(window|schedule|models) 前 12 位}:{windowStart}`。同身份行共享计数，范围或额度不同不意味着新计数。
- 累计脚本 ARGV 固定为 `(userID, delta, now)`，其后每个 KEY 依次携带 `(expireAt, since, poolFlag)`；`poolFlag=1` 时同时累加 `__pool` 并 `HSETNX __since`。停用规则的 occurrence 仍计数但不展示、不拦截；请求前检查只解析命中原始模型名的行，管理端展示传空模型名取全部行。
- 以上为既有预算计数语义。开启持续累计后，每次正向结算按客户端原始模型名同时记录个人与池子的日、周事实，即使没有模型预算。关闭后仅停止尚无预算需求的模型/周期；已保存的 `continuous_model` 行（包括停用行）所需事实继续累计。旧预算身份保留原来源及人工值；首次开启后新建的非空模型日/周身份使用 continuous_model，同身份跨 scope/日周时段继续复用来源。全模型及 occurrence 仍走原计数。
- 原始事实 key 为 `channel_model_usage:{cid}:{daily|weekly}:{完整 SHA-256 模型摘要}:{windowStart}`，Hash 字段为用户 ID、`__pool`、`__since`；与旧计数一并进入同一次累计 Lua，最多追加两个 key，不增加结算写往返。事实与调整均在窗口结束后 86400 秒过期；内存调整附在既有周期桶上并复用清理与锁顺序，不能另建无界历史表。
- continuous_model 人工调整不改原始事实：`channel_model_adjustment:{cid}:{原计数身份摘要}:{windowStart}` 按用户 ID 或 `__pool` 保存十进制字符串数组 `[绝对目标,各模型当时原始值...]`，后续用量为目标加各模型快照后的增量。调整只影响同计数身份及所选字段，不影响其他模型集合、其他用户或另一 scope。一个 Lua 原子读取全部选中模型及调整；请求检查只读当前用户、池子和起点，管理列表才用 HGETALL。金额预检与聚合必须保留 int64 精度，禁止 Lua tonumber/浮点求和造成精度丢失或负数。
- 开启前及关闭缺口不回填；重新开启可复用当前窗口保留的事实。新来源行即使创建较晚，也从事实起点读取；缺少事实时以当前可证明的累计起点为准。最近关闭区段与窗口相交时保守标记 incomplete，包括跨日/周关闭；已有持续预算可能仍被保守提示缺口。空原始模型名继续旧计数并记录缺口，不创建空模型事实。内存重启不能声称恢复了历史，Redis 已存事实的起点不因进程重启后移。
- 发布累计功能时先更新全部 new-api 实例，再开放开关及配套前端，避免旧实例把新来源读成旧计数。关闭开关不等于代码回滚；回退到不认识新来源的程序须配套恢复发布前策略与对应计数，不能仅关闭开关后直接回退。
- 旧错误码只属于 `scope=user` 且 `models` 为空的行（daily → `channel_user_daily_quota_exceeded`，weekly → `channel_user_weekly_quota_exceeded`）；按模型的行超限统一返回 `channel_period_quota_exceeded`。行级 `on_exceed` 优先，inherit 回落 `default_on_exceed`；行级 `channel_id=0` 表示同渠道换模型，仅允许 models 非空且目标不在该模型集合内。保存时显式指向来源渠道的行级目标也归一为 0；策略级禁止指向自身。
- `CheckSelectedChannelPeriodLimits` 不再按 revision=0 早返回，不回写旧日周上下文键；Controller/Relay 的统一入口及降级目标预检始终使用预算判定。旧日周独立检查、RelayInfo 日周快照字段与对应上下文键已删除。
- 请求仅检查 `used >= limit`，不预占。正向消费按资金结算成功时刻进入所有当时区间；零额、退款、负差额忽略。文本/音频/工具/任务/MJ 的调用必须在资金成功分支。人工修改个人 used 不改变池子/整段、账单或其他人。
- 配置缓存 5 秒，本地最多 4096 项；Redis 发布比较 revision，迟到的旧实例不能覆盖新版本。缓存保存失败返回 committed=true；TTL 期间仍可能存在旧配置，不宣称跨存储强一致。
- 策略缓存的进程级锁只保护本地 map 读取和发布，不跨 Redis GET/Lua 或数据库查询、保存。锁外查询可能迟到，本地发布也必须比较 revision；缓存仍持有更高版本时返回该版本，不覆盖为旧值。数据库保存继续以 revision CAS 防止并发编辑覆盖。
- 统一状态保留并发与 v2 `period_limits`，移除旧日/周状态块；period_limits 含 schema_version/revision/timezone/storage_mode/next_change_at/fallback_enabled/blocked/metrics。指标含 budget_id/budget_name/models、scope、period（daily/weekly/occurrence）、limit/used/remaining（不限为 null）、reset_at/tracking_since/coverage/enforced/source/base_limit，可选 override_limit；时段身份在 source.schedule_id/schedule_name。`fallback_enabled` 与请求期一致：`blocked` 时表示至少一条拦截行的有效动作（行级优先、inherit 回落 `default_on_exceed`）为 fallback，未拦截时才表示策略默认动作为 fallback；不能只看默认动作，否则仅在行上配置降级的模型会被误报为拒绝。
- 用量 GET 接受 `scope=user|pool` 和 `p`/page_size，分页使用 common.GetPageQuery（page_size 兼容 ps/size，上限 100）；user 返回 items 的 user_id/username/display_name/used_quota 及分页信息，按用量降序、同值按用户 ID 升序；pool 返回独立汇总 used_quota，均含 window_start/window_end/tracking_since/storage_mode。PUT 为 `{scope,user_id,used_quota}`，设置绝对目标而非增量；只调整选定计数身份的用户字段或池子汇总，不改 revision/资金。旧个人无模型日/周 used_quota 仍不得超过 common.MaxQuota，其他目标不得超过 common.MaxPeriodQuota。审计 `channel.budget_usage_set` 必须含 before/after，前值读取故障停止写入，不能伪记 0。
- 聚合用量 GET 无参数，响应 `{channel_id,revision,storage_mode,now,items:[{budget_id,scope,window_start,window_end,tracking_since,used_quota,pool_used_quota,top_user?}]}`：只含已保存行（含停用行），顺序与策略 `budgets` 一致；池子行 `used_quota` 为池子汇总；个人行 `used_quota` 为当前窗口用量最高用户的已用（同值按用户 ID 升序），`pool_used_quota` 为同计数身份的池子汇总，`top_user` 为 `{user_id,username,display_name,used_quota,effective_limit,override}`，`effective_limit` 取该用户统一状态中该行的生效上限（含提额，`override` 标记是否有提额），无用户用量时省略 `top_user`。不改 revision、不写审计；预算表与用量页签只用此接口，行内明细才走按行用量接口。每个不同的最高用量用户各评估一次整份策略，规模上界为行数。
- 提额 PUT 为 `{limit,expires_at}`，只允许 user 行，必须高于该行基础正额度且不超过 MaxPeriodQuota；expires_at=0 永久，正数必须在未来。PUT/DELETE 均不改策略 revision，审计 `channel.budget_user_override_set/delete` 含 before/after。撤销在新表写 quota_limit=0 的非生效记录，查询/分页排除它，重启迁移遇唯一键不覆盖，重新提额可替换；不得物理删除后让旧表数据重新导入。
- 主节点启动迁移仅转换明确 schema_version=1 或真正无策略记录的旧列输入；v2 跳过，单渠道失败记录并继续，重复启动不提升已迁移 revision。未知/缺失版本、null、空正文的已存储策略不得改写或当成不限，读取返回错误。无策略记录仍可产生 revision=0 的旧列派生视图，保存 v2 后旧列不再参与判定。
- 渠道日/周列和旧覆盖字段仅以 `json:"-"` 保留供迁移；普通创建/更新不再读写或校验。旧 user-daily-quota、user-weekly-quota、period-rules 特批端点返回 404；user-limit-overrides 仅保留并发与 expires_at，携带旧日/周字段返回 400。
- 回滚需回退代码并恢复上线前的三张旧表备份（策略、日周覆盖、规则覆盖）；新预算覆盖表恢复到上线前状态，首次升级前不存在时移除迁移创建的新表。不能只回退代码读取 v2，也不能只用新代码的 v1 兼容读取证明旧版可用；演练必须实际编译旧版，验证恢复后预算内放行和到限拦截。两仓同版本发布，ai-fund 使用实际管理响应生成的 v2 fixture 对齐。
- 「用户限制状态」弹窗页签固定为 预算 / 时段 / 用量 / 个人覆盖 / 当前并发，默认预算；策略面板只挂载一次，按页签切换预算表与时段列表，两者共用同一份 RHF 草稿与常驻保存栏，切页签不丢改动。预算表每行状态由前端推导，优先级 已停用 > 未到时段 > 已超限 > 接近上限（已用 ≥ 生效上限 85%）> 生效中；生效上限个人行取 `top_user.effective_limit`，池子行取 `limit`，0 显示不限不画进度；停用与未到时段行整体弱化。共用计数按 `(scope, window, occurrence 时的 schedule_id, 排序后的 models)` 分组，同组除创建最早的已保存行外显示「与「X」共用计数 · 当前 Y · 此行上限 Z」而非进度条。未保存临时行不查询用量，聚合读取失败显示重试。
- 时段状态：优先取预览行 `source.start_at/end_at`（服务器时区的当前或下次出现区间）判断进行中/下次开始；无引用时指定日期按 `start_at/end_at`、每周按 `view.timezone` 在前端推算下次开始，时区名无法识别（未设置 TZ 时为 `Local`）退回浏览器时区；停用时段 `active=false`，指定日期结束后显示已结束。预算表时段列显示「下次 时间」，时段列表显示「未开始 · 时间」。
- 保存动线：任何改动出现常驻保存栏（改动数与摘要、放弃、预览并保存），预览弹出逐字段 diff 与保存后生效结果，确认后按 revision 保存且 PUT body 与预览一致；放弃、重新加载、关闭弹窗在有脏草稿时确认。编辑、用量、提额从行上以 Sheet 打开，编辑抽屉关闭/Esc 保留草稿，行内校验名称必填、整段必须绑定时段、本渠道换模型要求本行指定模型，模型输入保留原始文本，字段通过 form 属性关联父表单；「本渠道换模型」前端写 `channel_id=0`，读取 0 或本渠道 id 均显示「本渠道」。策略级默认动作在工具栏。
- 后端 400/409 的 message 原文追加在面板顶部提示后；400 按行名定位（后端行级文案固定为「<原因>：<行名>」「<行名> 的<原因>：…」或「<原因>：<A> / <B>」，前端先按分隔段精确匹配，无命中再子串匹配），命中行加 `data-error` 高亮并在名称下显示去掉哨兵前缀的原因，打开该行编辑抽屉时顶部同样显示；预览失败不锁定草稿，保存失败与 409 锁定并要求重新加载。个人覆盖页签拆为预算提额与并发覆盖两个区块，并发覆盖为独立 Sheet；撤销提额、撤销并发覆盖、调整用量均先确认。
- “持续累计模型用量”开关属于渠道策略草稿，默认关闭、只读权限下禁用，变更后必须重新预览。类型层及两端表单保留来源与覆盖元数据；React 比较草稿和预览时须先经过同一 schema 归一化，不能仅因可选键插入顺序不同让有效预览消失。开关文案说明只从开启后累计，七语言同步维护。
- 新计数始终说明 `since_tracking_start`；已知失败周期为 incomplete，不回填历史。Redis+DB 同时故障且进程丢失时未落盘缺口无法恢复；这不是财务对账或分布式事务。内存仅当前进程有效。
- 每次超限只选择一个目标渠道+模型，默认拒绝，仅精确 HTTP Chat/Messages/Responses 三入口可降级一次，与 RetryTimes 无关。目标重新检查 Token 模型、固定渠道、有效组 Ability、启用、端点、全部适用预算、资金和并发。
- 路由预检早于视觉辅助/预扣/源并发租约；已有普通尝试租约先收口。任何上游副作用、输出、资金会话或已降级状态禁止再切。目标失败不走第三渠道；正常恢复后的新请求使用原选渠。
- 原始请求和每次尝试的 Responses 克隆不得调用其带业务过滤的 `MarshalJSON`：模型映射前的别名不能决定是否删除 `thinking_budget`。克隆使用无该方法的局部定义类型后进行 JSON 深复制；最终出站仍按映射后模型过滤。显式零值和切片、指针的尝试隔离必须保留，包括未配置降级的普通请求。
- 适配器转换和最终出站 JSON 都必须保留全部客户端字段，例外仅顶层 model 和 false stream 省略。原生结构不一致或目标接口无法转换时仍在目标准备阶段拒绝，不靠厂商或模型名猜测兼容性。
- 管理员配置额度降级即授权网关将原请求原样交给唯一目标尝试，除顶层 model 外不得因 `previous_response_id`、文件、容器、`encrypted_content`、Claude `redacted_thinking` 等状态字段提前拒绝；状态能否跨模型或渠道复用由目标上游判定。目标上游拒绝时返回该次真实错误且不再第三跳。
- OriginModelName 保持原始审计值，RoutingModelName 表示目标，价格仍经 Resolve/FreezeBillingModelName；目标映射计费开关生效。Advanced Custom 的预检转换和实际发送都使用 `info.RoutingModel()` 匹配模型路由，不能重新按来源模型匹配。阶梯表达式留到真实输入估算阶段，纯预检只判断分组免费语义。
- WebSocket/任务/MJ/Compact 等执行同一额度检查，不自动降级。成功日志 `other.admin_info.channel_limit_fallback` 保留来源、目标与原因；亲和性不把备用误记成原路由成功。
- 降级预检必须先用 `relay.ShouldHandleResponsesCompactPassthrough(info)` 排除全部 Compact 模式，再查询策略或执行模型映射/查价。历史 body bridge 与 V2 HTTP 也使用 `/v1/responses`，只按 URL 排除 `/v1/responses/compact` 会误查旧的 `*-openai-compact` 价格；这些请求继续由独立 Compact 准备和 `prepareMainRelayBilling` 执行基础模型计费、能力及额度门禁。

### 4. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| 不存在策略 | revision=0 旧列派生视图，继续统一判定；查询失败不能伪造不存在 |
| 版本过期 | 409 channel_period_policy_conflict，拒绝覆盖 |
| 越界/未知或缺字段/重叠/非法时间 | 管理 400 invalid_channel_period_policy |
| 累计开关为 null/字符串、非法扩展字段或来源值 | 管理 400，保留旧配置；缺省可选字段不按缺必填处理 |
| 原始事实/调整损坏、聚合溢出或当前事实小于调整快照 | 读取失败关闭，不能伪造零用量或产生负额度；Redis 故障不切内存 |
| 已存储策略未知/缺失版本或空正文 | 读取 503；迁移记录失败并保留原数据 |
| user-limit-overrides 携带旧日/周字段 | HTTP 400 且 success=false，不能只在 HTTP 200 正文中标记失败 |
| 提额不高于基础正额度，或对 pool 行提额/撤销 | 400，不写覆盖或成功审计 |
| used_quota 超过其计数支持上界 | 400，不调整计数 |
| 调整前值读取失败 | 503，不调整计数、不写成功审计 |
| 聚合用量的策略/计数不可读或用户状态评估失败 | 503 channel_period_policy_unavailable，不返回部分 items；用户摘要查询失败返回数据库错误 |
| 撤销旧提额后重启迁移 | 提额保持撤销，分页不返回零额度占位；重新授权不被旧值覆盖 |
| 池子或整段耗尽 | 429 channel_period_quota_exceeded，skipRetry；获准协调器可做一次额度降级 |
| 个人日/周耗尽 | 保留旧错误码；相同一次降级规则 |
| 策略/计数不可读 | 503 channel_period_quota_unavailable，不降级、不当作不限 |
| 目标无权/停用 | 403 channel_limit_fallback_unavailable，上游调用 0 |
| 无法保留目标请求字段 | 400 channel_limit_fallback_unavailable，上游调用 0 |
| 仅工具 Schema 或业务参数含引用同名字段 | 不得仅凭字段名拒绝降级，继续目标权限、接口及字段保留校验 |
| 请求含真实上游会话、文件或加密引用 | 原样调用唯一目标；目标拒绝时返回真实错误且不再第三跳 |
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
- 正常：先开启累计，用户已消费模型 A 60，再新增 A 日预算 100，新来源预算显示已用 60、剩余 40；旧来源身份继续保留原用量，不能自动迁移以覆盖人工调整。
- 边界：A 原始值 60、B 为 20，将 `{A,B}` 个人日用量设为 10 后，A 又消费 5，则该组合为 15；A 单模型为 65，B 为 20，池子原始总量为 85。关闭后再开启只保留确实记录的部分，不补算未记录时段。
- 错误：为了让新组合预算有历史，直接把 A 的原始事实清零或把所有旧行切到 continuous_model。正确：新身份聚合原始事实，人工调整另存目标与原子快照，已有同身份行保持权威来源。
- 错误：把所有非 v2 正文当成 v1，或把审计前值读取错误当作 0。正确：仅显式 v1 进入兼容转换，异常返回错误，保留原策略/计数并停止成功写入。
- 正常：8 行策略打开预算页签只发 1 次 `GET /budgets/usage-summary`；个人日行上限 10 且最高用量用户已提额到 16，进度与剩余按 16 计算并标注「提额至 16」。
- 错误：前端逐行调用 `/budgets/:budget_id/usage` 画进度，或把带时段的日行画成独立进度条；正确：只用聚合摘要，带时段的日/周行与同范围同模型的无时段主行共用计数并显示文字说明。
- 错误：后端 400「预算引用的时段不存在：假期个人每日」只显示在顶部，或按子串把「假期个人」行一起标红；正确：先按分隔段精确匹配只高亮「假期个人每日」，行下与编辑抽屉显示去前缀原因。

### 6. Tests Required

- `service/channel_period_policy_test.go`：两种存储、假期/跨周/到期/遮盖/停用、140 软超额、人工调整隔离、整数原子性、缺口、缓存损坏/迟到发布、结算失败不累计。
- `service/channel_model_usage_test.go`：内存/Redis 的预算前累计、默认关闭、渠道隔离、旧来源保留、关闭/重开与跨周缺口、正向结算、组合/用户/池子调整隔离、原子失败、并发调整、精度/损坏及读取失败；相关并发用例执行 race。`service/channel_model_usage_benchmark_test.go` 仅在隔离 Redis 测量开关写开销与多模型读取，不用耗时阈值充当正确性断言。
- `controller/channel_model_usage_test.go`：真实 API 验证提前累计、非法扩展值、旧客户端保存保留开关/时间/来源，输出 `worker/src/fixtures/channel-model-usage-contract.json` 供 ai-fund 两层和 Vue 使用；数据库合同测试覆盖 SQLite/MySQL/PostgreSQL 的 false、切换时间和来源 TEXT 往返。
- `service/channel_budget_plan_test.go`：无模型身份的 userKey/poolKey 与旧 key 一致、时段日限与无时段日限共享计数、同组优先级与 next 切换点；模型精确匹配且与整体预算并行；降级 inherit/reject/同渠道/指定渠道；内存与 Redis 指标一致。`service/channel_budget_migrate_test.go` 保护派生 ID、归一化和 v2 校验矩阵。
- `service/channel_period_policy_concurrency_test.go`：用显式屏障暂停存储访问，验证不同渠道 Redis 读取不互相阻塞；内存/Redis 均验证旧数据库快照恢复后返回新版且不覆盖新版缓存，并执行 race 检查。
- `model/channel_period_policy_database_test.go`：sqlite + 专用 MYSQL/POSTGRES DSN，重入迁移、CAS、upsert、缺口单调。不得用生产 DSN。
- `service/channel_budget_migrate_database_test.go`：三库完整 v1 策略/旧列/两类覆盖迁移等价，重复启动 revision 不变，撤销后重启不复活，重新授权不被覆盖；专用 CHANNEL_PERIOD_TEST_MYSQL_DSN / CHANNEL_PERIOD_TEST_POSTGRES_DSN 缺失时只能记部分验证。
- `service/channel_budget_repair_test.go`：未知/缺失版本与空正文读取失败、迁移不改原数据；三种周期 × 内存/Redis 的模型变更和改名调额统计起点；用户用量排序与跨页同值顺序。
- `controller/channel_budget_test.go`：用量与提额的参数边界、隔离、revision 不变、真实 before/after 审计、撤销 pool 行拒绝；旧日/周提额字段分别断言 HTTP 400 与 success=false；Redis 审计读取故障后计数不变且无成功日志。
- `controller/channel_limit_fallback_test.go`：三协议 × 流/非流实际模拟上游、目标价格、唯一日志/钱包扣费、原始 DTO 不污染；Responses `encrypted_content` 与 Claude `redacted_thinking` 原样到达目标。
- `controller/channel_limit_fallback_preflight_test.go`：完整 Relay 覆盖 Responses 别名的预算正值/零值/缺省及已配置但未触发降级；自定义目标直接访问与降级访问的实际路径、模型、唯一账单、钱包扣费，以及工具同名参数完整保留。
- `controller/relay_attempt_responses_test.go`：先向非 Qwen 模型转换，再从原请求向 Qwen 重试，验证出站过滤仍生效且显式零值、原始输入和指针/切片未被前次尝试污染。
- `controller/channel_limit_fallback_compact_test.go`：只配置基础模型价格，完整 Relay 覆盖 V1 path、历史 body bridge、V2 HTTP 的正常透传、额度耗尽和能力关闭；断言原始请求/响应、唯一基础模型账单和钱包扣款，拒绝时零上游/消费且不调用备用渠道。
- `controller/channel_limit_fallback_boundary_test.go`：完整 Relay 的 retry=0、一次/禁用/恢复/目标限制与权限/上游状态原样转发/Compact、并发释放、阶梯预检；WebSocket 回合准备、任务和 Midjourney 在启用降级时仍因池子耗尽拒绝，上游调用和扣款均为零。
- `controller/channel_period_policy_test.go`：实际管理响应合同与 400/409；ai-fund fixture 来自该响应，`CHANNEL_PERIOD_CONTRACT_OUTPUT` 同时输出 `usage_summary`。
- `controller/channel_budget_usage_summary_test.go` 与 `TestChannelPeriodPolicyManagementContract`：摘要覆盖全部已保存行且顺序与策略一致、个人行返回最高用量用户及含提额的 `effective_limit`、响应不泄露渠道 key；`router/channel_router_test.go` 断言该路由为 ChannelRead。
- `controller/channel_limit_fallback_model_test.go`：真实 Relay → 模拟上游 → 结算/日志，覆盖按模型池子/个人日周、同渠道模型切换按目标价格扣费、目标个人限额阻断且不二跳。
- `web/src/features/channels/components/dialogs/__tests__/channel-period-policy-panel.test.tsx`：预算表时段/进度、抽屉关闭和 Esc 保留草稿、提额后的个人进度、读取失败重试、新行不查用量、版本冲突与跨渠道迟到响应。
- 同一 React 面板测试还须覆盖开关默认关闭、只读禁用、预览后保存与显式关闭，并断言可选键顺序变化不使预览失效；以及保存栏出现与计数、diff 与 PUT body 一致、放弃草稿确认、共用计数说明只画一条进度条、本渠道换模型预览 body `on_exceed.channel_id === 0`、后端 400 行 `data-error` 高亮与抽屉 `role=alert` 原因、未到时段行显示「Next start」。
- `web/src/features/channels/components/dialogs/__tests__/channel-user-limits-dialog.test.tsx`：页签顺序为 Budgets / Schedules / Usage / Personal overrides / Current concurrency、时段页签改动切回预算页签后保存栏仍在、脏草稿 Escape 关闭先弹确认且确认后才回调 `onOpenChange(false)`、并发覆盖表单位于 `[role=dialog][aria-label="Concurrency override"]` 而非 alertdialog、聚合用量列表按行打开明细并提交绝对目标。
- `web/src/features/channels/lib/__tests__/`：`channel-budget-status.test.ts`（状态优先级、计数身份分组、生效上限）、`channel-schedule-state.test.ts`（指定日期起止、预览来源优先、每周跨周与本周下次开始、停用、时区回退）、`channel-policy-error-rows.test.ts`（三种后端文案格式精确匹配、子串回退、前缀剥离）。
- React 渠道测试、Vue 真实挂载、Worker 全量；两端 build、NewAPI typecheck/lint/i18n；relaykit 独立 build/vet。
