# Brief — 用户限额与 ai-fund 升级

## Goal

- 在现有渠道单用户并发和每日额度基础上增加每周额度，并提供可提前配置、可自动过期的个人提额能力；同步升级 NewAPI 管理端和 ai-fund 号池/用户页面。

## Scope

- NewAPI 渠道新增单用户每周额度字段，按服务端时区每周一 `00:00` 刷新；日限与周限都是基于已结算额度的软上限，可同时生效。
- NewAPI 新增按 `channel_id + user_id` 持久化的个人覆盖记录，可覆盖并发、每日额度和每周额度，三个字段共享一个可选 `expires_at`。
- 覆盖立即生效，不支持未来定时开始；到期或撤销后自动回落，不取消在途请求，也不修改钱包、订阅、Token、消费日志或周期已用量。
- 新增周额度状态服务、429/503 稳定错误、Relay 检查、全部正向计费路径的周累计、管理列表和本周已用额度调整。
- 保留现有每日额度和并发接口，向后兼容扩展 base/effective/override/expiry 字段；新增指定用户统一状态、周额度和个人覆盖管理接口。
- NewAPI 渠道表单增加周限金额字段；用户限制 Dialog 增加每日、每周、并发、个人覆盖四个视图。
- 个人覆盖支持按用户 ID、用户名或显示名搜索任意已存在用户提前配置，不要求已有请求记录；日、周、并发活跃行同时提供快捷入口。
- ai-fund `/pools` 展示和管理渠道日/周/并发状态及周期使用量；`/admin` 提供独立“号池限制”页签，并复用同一工作区管理 Claude/OpenAI 的默认限制、周期使用量和个人覆盖。
- Cloudflare Worker 继续负责 BFF、管理员门禁、金额换算、缓存和脱敏审计；D1 只保存号池映射和审计，不复制限额事实。

## Non-Goals

- 不把个人覆盖扩展到用户钱包、订阅套餐、Token 独立额度、模型或分组维度。
- 不实现严格额度预占、请求排队、未来定时开始、到期主动中断请求或跨周期退款回溯。
- 不让 CF Worker/D1 进入 Relay 请求热路径，也不在 NewAPI 与 D1 之间双写限额状态。
- 不为周限重构现有每日额度或并发模块为通用插件框架。

## Key Decisions

- NewAPI 主数据库是个人覆盖的唯一事实源；Redis/内存只保存周期使用量、并发租约和短期覆盖缓存。
- 个人覆盖采用绝对上限，实际值固定为 `max(渠道默认值, 当前有效覆盖值)`，因此覆盖不能降低用户限制；渠道默认值为不限时拒绝临时提额。
- 同一渠道用户只有一条覆盖记录，三个可空覆盖字段共享 `expires_at`；`expires_at=0` 表示持续有效，三个字段均清空等价于撤销。
- 周期与现有订阅 weekly 约定一致，使用服务端时区周一 `00:00`，通过新周期 key 刷新而非定时清零。
- 覆盖读取故障时 Relay 回落到更保守的渠道默认值并写脱敏告警；管理查询则明确报错，不展示猜测状态。
- 指定用户统一状态接口直接返回日、周、并发和覆盖信息，ai-fund 不再依赖活跃列表分页推断本人状态。
- 个人覆盖采用“主动搜索配置 + 活跃记录快捷入口”双入口；无请求记录时使用量和当前并发显示为 `0`，第一次请求立即应用覆盖。
- ai-fund 普通用户状态只使用服务端绑定的 NewAPI 用户 ID；管理员在本地权限门禁后可搜索任意已存在的 NewAPI 用户，个人覆盖写入仍受 `expected_channel_id` 冲突保护。

## Key Context

- 当前每日额度合同：`.trellis/spec/backend/channel-user-daily-quota.md`；并发合同：`.trellis/spec/backend/channel-user-concurrency.md`。
- 渠道字段和选渠上下文位于 `model/channel.go`、`middleware/distributor.go`；每日检查和正向记录分布在 Relay、任务、工具计费、违规费用和 Midjourney 路径。
- NewAPI 管理入口为 `controller/channel_user_limits.go` 与 `web/src/features/channels/components/dialogs/channel-user-limits-dialog.tsx`。
- NewAPI 前端使用 React 19、Base UI、React Query、Zod 和七语言 i18n；locale 只能通过脚本写入并执行 `bun run i18n:sync`。
- ai-fund 当前活动任务为 `/root/project/ai-fund/.trellis/tasks/08-20-pool-user-limits`，工作树已有未完成实现，后续必须按需求变更流程增量修改。
- ai-fund 现有入口包括 `worker/src/pool_limits.js`、`worker/src/newapi_client.js`、`PoolLimitPanel.vue`、`PoolLimitAdminModal.vue` 与 `Admin.vue`。
- build 分支实现必须把新业务放入独立文件，现有上游热点只保留必要的字段同步、检查和记录调用。

## Risks / Deferred

- 周累计必须镜像全部每日正向记录路径；遗漏任一路径会造成日、周口径不一致，实施和检查阶段需反向扫描所有调用点。
- 任务、Midjourney、视频代理和视觉辅助存在手工渠道上下文切换，必须同时刷新个人有效日/周/并发值。
- 覆盖缓存 TTL 不得跨越 `expires_at`；写后失效失败只能造成有界短暂旧值，并必须记录告警。
- ai-fund 当前任务已在实现中，接入新契约前需要更新其规划材料，不能覆盖现有代码或把两个事实源混入 D1。
- 上线顺序固定为 NewAPI 先、ai-fund 后；回滚顺序相反。

## Acceptance

- 渠道周限可配置为金额或 `0` 不限，按周一 `00:00` 刷新，并与日限同时正确拦截新请求。
- 周累计覆盖普通 Relay、WebSocket、任务、Midjourney、工具调用和违规费用等现有正向记账路径，只归属最终实际渠道且只记录一次。
- 达到日限或周限返回对应 429；状态服务不可用返回 503；均不预扣、不访问上游、不重试、不自动禁用渠道。
- 管理员可查看和修正本周已用额度，且不影响账务、日志、其他周期或在途请求。
- 管理员可搜索从未请求过的用户提前设置个人覆盖，也可从活跃记录快捷进入；两种入口写入同一记录并返回一致状态。
- 覆盖立即生效，可持续有效或按时到期；撤销/到期后回落，超出回落值的在途请求不取消，但新请求被阻止。
- NewAPI 管理 API、React 表单和 Dialog 正确处理权限、分页、金额、显式零值、到期时间、最小字段和七语言文案。
- ai-fund 普通用户看到日/周/并发默认值、有效值、已用/当前值、剩余值、刷新和覆盖到期；管理员可在 `/pools` 与 `/admin` 完成对应管理。
- Worker 出站请求使用严格字段白名单，不泄露 PAT、渠道密钥或完整渠道对象；D1 只记录映射和脱敏审计。
- NewAPI 后端测试、竞态检查、relaykit 独立构建、React 检查与构建、ai-fund Worker 测试、Vue 构建和跨仓场景验证通过。

## Next Step

- 完成本轮 Check-All 修复和重检；通过后更新可执行规范，再进入提交阶段。
