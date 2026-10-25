# 父渠道限制继承方案调查

## 当前产品决定

- 用户提出 B 关联父渠道 A，希望 ai-fund 不改源码；随后确认子渠道继承限制只读。
- A 持有唯一的并发、预算、个人提额和计数，A 与 B 的同用户请求合计限制。B 的限制配置不复制、不反向写入 A。
- B 的上游地址、密钥、类型和模型仍由 B 独立配置；账单和消费日志按实际物理渠道记录。

## 已核对的接入点

- 当前 `Channel` 没有父渠道限制关联能力；设计采用新增独立关系表持久化，不能借用普通渠道 Group 或 Tag，也不必改变既有渠道字段语义。
- `middleware/distributor.go:455` 的 `SetupContextForSelectedChannel` 建立物理渠道上下文并调用 `ApplyChannelUserEffectiveLimits`；需要把物理渠道身份与限制归属分开。
- `controller/channel_user_concurrency.go` 与 `relay/channel_user_concurrency.go` 的租约都从物理 ChannelId 获取。必须统一改为解析后的限制归属，不能只改配置值，否则仍会有 A/B 两份并发计数。
- `service/channel_budget_guard.go:46` 从 channel.Id 读取策略、覆盖和计数；`service/channel_user_quota_usage.go:31` 按实际渠道入口累计。检查、正向累计及查询都需要一致的归属解析，不能仅让 B 读取 A 的策略。
- `controller/channel_user_limits.go:44` 的管理并发列表按渠道 ID 枚举。A 的查询必须返回父子合计租约，否则 ai-fund 显示与实际拦截不一致。
- `controller/channel.go:948` 的普通并发更新仍按渠道保存。A 保持原合同；B 继承期间的限制写入应明确拒绝，不自动转写 A。
- `worker/src/newapi_client.js:1092` 严格验证统一状态的 channel_id；父渠道 A 返回必须继续标明 A，不能引入独立虚拟组 ID。

## 设计约束

- 新增领域模块承载关系校验、限制归属解析及继承写入保护，旧文件只增加薄接入，遵循 build 分支同步友好规范。
- 建议仅支持一级父子关系，禁止自身关联、环和把已有子渠道的父渠道再次挂到别处。删除父渠道前必须处理其子渠道。
- 关联变更与在途请求需要明确生命周期：已获取租约和已建立的结算归属不得在中途漂移到另一计数桶；不能使在途请求丢失或重复累计。
- HTTP、WebSocket、异步任务、Midjourney、视觉辅助均要使用同一归属原则；不得通过替换物理 ChannelId 破坏上游选取、任务归属或日志。
- 用户已确认 A 原有累计在 B 关联时不重置；B 关联前的历史不回填，关联后的新增消费归入 A。
