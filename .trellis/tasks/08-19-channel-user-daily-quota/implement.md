# 实施计划 — 渠道级用户每日额度限制与用量可视化

## 1. 开发前门禁

- [ ] 展示并确认最终 `brief.md` 后运行 `task.py start`，planning 状态不修改产品代码。
- [ ] 进入 Phase 2 后通过 `trellis-route(target=implement)` 选择执行方式。
- [ ] 实现前加载 `trellis-before-dev`、后端相关规范、`web/AGENTS.md`、`shadcn-ui`、`i18n-translate` 和 React 性能规范。
- [ ] 读取所有涉及的 Channel、RelayInfo、BillingSession、Task、前端 Channel schema/form 类型定义，不猜测字段或签名。
- [ ] 保留当前工作区已有 `.trellis/spec/` 修改，不覆盖或回退用户改动。

## 2. 渠道配置与上下文

- [ ] 在 `model.Channel` 增加 `UserDailyQuotaLimit *int` 和归一化 getter，使用包含 `@return` 的中文 GoDoc。
- [ ] 使用现有 AutoMigrate 增加普通可空整数列，验证 SQLite/MySQL/PostgreSQL 兼容，不增加数据库默认值。
- [ ] 在渠道创建/更新校验中接受 `0..common.MaxQuota`，显式 `null` 归一化为 `0`，保留显式零值。
- [ ] 把字段加入非敏感渠道字段分类、复制/审计和相关 Controller 测试。
- [ ] 增加 `ContextKeyChannelUserDailyQuotaLimit`，由 `SetupContextForSelectedChannel` 在初选和重试时写入。
- [ ] 在 `relay/common.ChannelMeta` 增加 `ChannelUserDailyQuotaLimit int`，由 `RelayInfo.InitChannelMeta` 读取当前选渠 context，确保重试后检查和记账都绑定最终实际渠道配置快照。
- [ ] 增加 Model/Controller/Middleware 测试，覆盖历史 `NULL`、正数、显式 `0/null`、负数、溢出、持久化和 context 刷新。

## 3. 每日额度服务

- [ ] 新建 `service/channel_user_daily_quota.go`，实现自然日周期、Redis Hash、内存 fallback、检查、正向累计、列表和单用户目标值调整。
- [ ] 所有导出类型和方法添加中文 GoDoc，参数和返回值显式写 `@param`、`@return`。
- [ ] Redis key 使用 `channel_user_daily_quota:{channel_id}:YYYY-MM-DD`；累计与 TTL 刷新原子执行。
- [ ] `limit <= 0` 和 `quota <= 0` 快速返回，不访问 Redis；拒绝非法渠道、用户、负数和溢出输入。
- [ ] Redis 已配置但不可用时检查返回不可用错误，不回退内存；结算写失败只告警，不改写成功响应。
- [ ] 内存模式使用短临界区，并按自然日清理旧 bucket。
- [ ] 增加确定性 service 测试，覆盖用户/渠道/日期隔离、软上限、超额后阻止、自然日切换、累计、目标值覆盖、`0` 删除、Redis 故障和内存模式。

## 4. Relay 检查与错误契约

- [ ] 在 `relaykit/types/error.go` 增加每日额度超限和不可用稳定错误码；完成后执行 `cd relaykit && GOWORK=off go build ./...`。
- [ ] 新建 `controller/channel_user_daily_quota.go`，集中读取 context、调用 service、转换 `429/503`、设置 `skipRetry` 和管理员审计信息。
- [ ] 在主 `prepareMainRelayBilling` 的价格计算后、预扣前检查；免费请求跳过。
- [ ] Alpha Search 独立预扣入口调用相同检查能力。
- [ ] 异步任务每次提交尝试在价格计算后检查当前渠道，即使 BillingSession 已存在也必须重新检查候选渠道。
- [ ] 视觉辅助按辅助渠道检查；Midjourney 各正向计费入口在上游调用前检查。
- [ ] 每日额度本地错误不进入 `processChannelError`、换渠重试、自动禁用或计费预扣，并通过统一错误日志入口只记录一次。
- [ ] 增加主 Relay、重试、Task、视觉辅助和 Midjourney 行为测试，断言软上限检查发生在上游和计费之前。

## 5. 正向额度累计

- [ ] 逐项核对现有 `model.UpdateChannelUsedQuota` 正向调用点，仅在资金结算成功、即将记录正向渠道使用量的位置调用同一每日额度累计服务，保持两个统计口径一致。
- [ ] 普通 Relay 从最终 `RelayInfo.ChannelMeta.ChannelUserDailyQuotaLimit` 判断是否追踪；限额快照为 `0` 或额度非正数时快速返回，不访问 Redis。
- [ ] 不把每日额度累计放进 `BillingSession.Settle`，避免资金/Token 结算与后续渠道消费记账的成功边界不一致。
- [ ] 在 `TaskPrivateData` 增加向后兼容的 `ChannelUserDailyQuotaTracked bool`；提交时冻结是否追踪，初始正向扣费按提交日累计，后续正向差额按实际记账日累计，旧任务和关闭限额时提交的任务不补记。
- [ ] 文本、图片、音频、Realtime、视觉辅助、工具调用、普通任务、Midjourney、违规费用和异步任务差额等正向记账点各调用一次；不把预扣额度、普通失败或负向退款写入每日累计。
- [ ] 增加重试最终渠道、现有一次性记账保护、跨日任务差额、旧任务兼容、Midjourney 和违规费用测试，防止重复累计、错误回溯或漏计。

## 6. 当前并发统计

- [ ] 扩展 `service/channel_user_concurrency.go` 的获取/释放 Lua，在同一 Redis 往返中维护 `channel_user_concurrency_users:{channel_id}` 用户索引。
- [ ] 保持原租约 key、TTL、心跳、错误和取消语义不变；索引 key 与租约 key 使用相同渠道 hash tag。
- [ ] 新增只读统计方法：Redis 模式使用索引和 pipeline 清理过期租约并读取有效 `ZCARD`，内存模式从现有 map 计算。
- [ ] 查询时移除空用户和残留索引成员，不返回 lease ID 或内部 key。
- [ ] 扩展 service 测试，覆盖获取加入索引、最后租约释放移除、崩溃残留清理、用户隔离和内存统计。

## 7. 管理 API 与用户摘要

- [ ] 新建 Model 层用户摘要查询，单次读取 `id/username/display_name`，不返回敏感字段。
- [ ] 新建 `controller/channel_user_limits.go`，实现当日额度列表、单用户目标值调整和当前并发列表。
- [ ] 在 `router/channel-router.go` 注册三个路由；列表使用 `ChannelRead`，个人调整使用 `ChannelOperate`。
- [ ] API 响应包含 channel ID、limit、storage mode、reset time、分页元数据和用户行。
- [ ] 调整请求使用 `used_quota` 表示调整后的已使用额度；校验渠道、用户 ID、分页和目标额度边界，不改日志、余额、Token、渠道历史或并发。
- [ ] 增加 Router 权限测试、Controller 成功/空/错误/目标值调整测试和敏感字段边界测试。

## 8. 前端实现

- [ ] 在 `web/src/features/channels/types.ts`、`constants.ts`、`lib/channel-form.ts` 和错误映射中增加每日额度字段。
- [ ] 表单使用现有额度转换函数处理显示单位，验证 `0..common.MaxQuota` 对应范围并保留显式 `0`。
- [ ] 在现有用户并发配置附近增加每日额度输入和说明，不改变无关表单布局。
- [ ] 扩展 `api.ts`、类型和 query keys，增加额度列表、个人目标值调整和并发列表请求。
- [ ] 新建 `ChannelUserLimitsDialog`，使用 Tabs 展示“每日额度”和“当前并发”；实现分页、刷新、自动轮询、空/错/加载状态、“调整后的已使用额度”输入及调整确认。
- [ ] 在 Channels Provider、Dialogs 注册和行操作菜单增加窄入口，使用 lucide 图标和 Tooltip。
- [ ] 当前并发 Tab 仅在 Dialog 打开且选中时轮询；关闭后停止，避免渠道主表额外请求。
- [ ] 按 `i18n-translate` 规范补齐 `en/zh/zh-TW/fr/ja/ru/vi`，运行源码缺键扫描与 `bun run i18n:sync`。
- [ ] 在模块 `__tests__/` 中增加表单 round-trip、Dialog 状态、目标金额校验与确认、轮询启停和 memory 模式提示测试。

## 9. 验证计划

### 后端定向

```bash
go test ./model ./service ./middleware ./controller ./router ./relay -count=1
go test -race ./service -run 'ChannelUserDailyQuota|ChannelUserConcurrency' -count=1
go test -race ./controller -run 'ChannelUserDailyQuota|ChannelUserConcurrency|RelayTask|ResponsesWebSocket' -count=1
go vet ./model ./service ./middleware ./controller ./router ./relay ./relaykit/types
```

### relaykit 独立构建

```bash
cd relaykit
GOWORK=off go build ./...
```

### 前端

```bash
cd web
bun run i18n:sync
bun run typecheck
bun run lint
bun run build
```

### 全仓与 Diff

```bash
go test ./... -count=1
go vet ./...
git diff --check
git diff --stat
```

不添加基于固定耗时阈值、sleep 或随机并发的性能测试；通过结构约束保证 Relay 请求前只有一次轻量状态检查，并在代码审查中确认没有数据库查询或 Redis 扫描进入热路径。

## 10. 发布与回滚检查

- [ ] 部署前确认所有实例时区一致，生产 Redis 健康。
- [ ] 配置一个测试渠道每日额度，验证同一用户累计未达上限时放行、达到或超过后新请求返回 `429`。
- [ ] 使用已有并发限制制造多个同时请求，确认允许可接受的软上限超额且后续请求被阻止。
- [ ] 在渠道 Dialog 验证每日额度、个人目标金额调整、当前并发自动刷新和 Redis/内存模式提示。
- [ ] 个人调整时保留一个在途请求，确认请求不被取消且完成后实际额度继续累加。
- [ ] 临时使 Redis 不可用，确认启用每日限额的请求返回 `503`，恢复后检查和展示恢复。
- [ ] 紧急回滚优先把渠道 `user_daily_quota_limit` 改为 `0`；额度 Hash 和并发索引依赖 TTL 清理，无需全库扫描删除。

## 11. 完成标准

- [ ] PRD 验收项均有自动测试、静态检查或发布验证证据。
- [ ] 原有热点文件只有必要字段、context、检查、累计或 UI 注册接入，无无关重构和格式化。
- [ ] 每日额度检查不访问数据库、不预占估算额度、不使用 Redis `SCAN`。
- [ ] 当前并发可视化不改变现有租约限制行为。
- [ ] `trellis-check-all` 通过；修复后重新运行受影响验证。
- [ ] 使用 `trellis-update-spec` 更新渠道用户并发/每日额度运行状态契约，再进入提交阶段。
