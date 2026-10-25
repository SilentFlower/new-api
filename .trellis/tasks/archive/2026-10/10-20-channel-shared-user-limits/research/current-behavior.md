# 现有渠道限制调查

## 并发及普通重试

- `model/channel.go:54` 的 `Channel.UserConcurrencyLimit` 仍是渠道独立配置。
- `service/channel_user_concurrency.go:139` 的 `AcquireChannelUserConcurrency` 接收渠道 ID、用户 ID 和上限，底层使用 Redis 租约或进程内存租约。
- `.trellis/spec/backend/channel-user-concurrency.md:10` 明确同用户跨令牌共享、不同渠道和用户隔离；多实例需要 Redis，Redis 已启用但故障时失败关闭。
- `controller/channel_user_concurrency.go:50` 的 Controller guard 在调用上游前获取租约；失败生成 429 或 503，并设置 SkipRetry。
- `controller/relay.go:235` 获取失败后立即跳出普通尝试循环；`controller/relay.go:360` 的 `shouldRetry` 也识别 SkipRetry。
- 共享方案需要真正共享原子计数，不能靠读取各渠道当前计数后求和再获取独立租约，否则并发竞争仍会突破总上限。

## 金额预算及显式降级

- `dto/channel_period_policy.go:28` 的预算行支持 scope=user/pool、window=daily/weekly/occurrence、模型集合、时段、上限和超限动作。
- `.trellis/spec/backend/channel-period-budget-fallback.md` 是预算 v2 权威规范，旧日周规范中已废弃的配置和独立 API 不能作为实现入口。
- `controller/channel_limit_fallback.go:61` 在主并发获取前检查金额预算；HTTP Chat/Messages/Responses 等获准入口可按配置选择唯一目标，目标再次检查预算和权限。
- `service/channel_limit_fallback_select.go:12` 按行级优先、策略级兜底选择目标；同渠道换模型使用来源渠道 ID。
- 因此组内额度超限后的切换必须重新遵守同一共享限制，不能仅依赖普通重试的 SkipRetry。是否允许明确配置的组外降级尚待产品决策。
- 金额继续是已结算用量软上限；增加共享维度不应未经确认变成预占额度或改变真实财务账单归属。

## 历史检索与工作区

- 使用 `trellis mem search '关联渠道'` 检索当前项目，仅命中本次会话，未找到可复用的历史产品决定。
- 工作区已有的归档任务改动和缓存文件记录于任务创建时的 baseline，不属于本功能。
- 初始化 `get_context.py` 的完整报告因既存任务树递归报错；本轮使用已注入的会话上下文、独立状态命令和规范索引继续规划，未修改该脚本。

## 已确认的产品决定

- 用户确认并发按用户在关联渠道内合计，同一用户跨令牌共享，不同用户独立。
- 用户确认关联后共用一套并发、预算和个人提额配置；原渠道配置保留，解除关联后恢复，不与共享限制叠加。
- 用户提出父子关联并确认子渠道继承限制只读：A 为唯一配置来源，修改 A 生效于 B，B 不反向修改 A。ai-fund 继续映射 A，源码保持不变。
- 用户确认历史用量不回填：A 的已有累计保留，B 只将关联后的新增正向消费计入 A。

## 用量兼容性补充

- `service/channel_budget_usage.go:90` 的计数身份依赖渠道、周期、模型集合以及整段时段身份；同周期不同策略的任意行不能直接按行名相加。
- `service/channel_budget_usage.go:156` 的正向累计始终维护渠道全模型的日/周事实，按模型和整段身份依赖已保存规则或持续累计开关；不能宣称任意历史模型/整段用量均完整可得。
- `model/channel_user_budget_override.go:13` 的提额关联渠道 ID、用户 ID 和预算行 ID；把多渠道既有提额自动合并为一个值没有现成产品规则。
- 将新共享计数与原渠道计数区分，可保留解除关联的旧配置及原始账单归属；但启用共享或转组时的用量起点必须在界面和验收中明确。
