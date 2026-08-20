# 实施计划

## 1. NewAPI 数据模型与迁移

- 在 `model/channel.go` 增加 `UserWeeklyQuotaLimit` 与归一化 getter，覆盖历史 `NULL`、显式 `0`、正数和异常负数测试。
- 新建个人覆盖模型文件，定义唯一索引、跨数据库 GORM 查询、整条替换、删除、有效读取和分页列表。
- 把新模型加入 `migrateDB` 与 `migrateDBFast`；验证 SQLite 迁移，并检查 MySQL/PostgreSQL 标签没有方言依赖。
- 扩展渠道创建、更新、复制、审计和缓存刷新逻辑，保留显式 `0/null`。

## 2. 个人覆盖解析服务

- 新建独立服务文件，定义基础值、覆盖值和实际有效值结构。
- 实现 `max(base, override)`、不限渠道、到期回落、空记录和异常数据归一化。
- 增加以 `channel_id + user_id` 为键的正向/负向缓存，TTL 上限 30 秒且不跨越 `expires_at`。
- 管理写入提交后失效或刷新共享缓存；缓存/数据库读取同时失败时 Relay 回落渠道默认值并记录脱敏告警。
- 为多实例 Redis、无 Redis 单实例、写后失效、到期边界和数据库故障回落增加测试。

## 3. 每周额度状态服务

- 以每日额度模块为行为源，新建周限独立服务、Redis Lua、内存状态和测试文件。
- 实现周一 `00:00` 周期、周 key、TTL、检查、正向累计、列表、直接用户读取和目标值覆盖。
- 覆盖用户/渠道/周隔离、软上限并发超额、显式 `0`、溢出、Redis 故障和不限时仍记录。
- 不修改每日额度的既有键、TTL 或管理语义。

## 4. Relay 与错误契约

- 在选渠和所有手工渠道切换路径解析个人有效并发、日限和周限，写入 context 与最终渠道快照。
- 在现有每日额度检查旁增加周限检查；免费请求保持跳过周期限额。
- 在全部每日正向记录点旁增加周累计，保持一次性结算、最终渠道、任务差额和退款语义。
- 增加周限 429/503 错误码、协议适配、skipRetry 和管理员错误日志字段。
- 修改 `relaykit/` 后执行独立构建，确认没有根模块依赖。

## 5. NewAPI 管理 API

- 扩展现有日限/并发列表条目，返回 base/effective/override/expiry，同时保持旧字段兼容。
- 新增周限列表与本周已用额度调整接口。
- 新增指定用户统一限制状态接口，直接读取日、周和并发状态，不从分页列表推断。
- 新增个人覆盖列表、整条替换和撤销接口，使用最小用户摘要、稳定分页、权限与管理审计。
- 增加 Controller、Router、权限、参数边界、敏感字段排除和管理错误测试。

## 6. NewAPI React 管理端

- 更新 Channel schema、类型、默认值、表单回填和提交转换，增加每周金额字段。
- 新建周限表单字段，复用每日额度的金额步长和空字符串提交规则。
- 扩展 API client 与类型，支持周限、统一状态和个人覆盖。
- 扩展 `ChannelUserLimitsDialog` 为每日、每周、并发、个人覆盖四个 Tabs；加入用户搜索、覆盖编辑、到期时间和撤销确认。
- 在日、周、并发活跃行增加复用同一编辑弹窗的快捷入口，并覆盖无历史使用记录用户的提前配置测试。
- 使用 Base UI 现有组合组件和 Hugeicons，检查 Dialog/Combobox/Tabs 上下文、分页收缩和移动端布局。
- 所有文案通过 `t(...)`；只通过 `add-missing-keys.mjs` 写七语言 locale，完成同步和缺键扫描。

## 7. NewAPI 验证

- 定向测试：
  - `go test ./model -run 'ChannelUser.*Limit|WeeklyQuota|LimitOverride' -count=1`
  - `go test ./service -run 'ChannelUser.*Quota|ChannelUser.*Override|ChannelUserConcurrency' -count=1`
  - `go test ./controller ./router -run 'ChannelUser.*Limit|WeeklyQuota' -count=1`
- 竞态与静态检查：
  - `go test -race ./service -run 'ChannelUser.*Quota|ChannelUser.*Override|ChannelUserConcurrency' -count=1`
  - `go vet ./model ./service ./controller ./router ./middleware ./relay`
  - `git diff --check`
- 模块与全量：
  - `cd relaykit && GOWORK=off go build ./...`
  - `go test ./... -count=1`
- 前端：
  - `cd web && bun test src/features/channels`
  - `cd web && bun run typecheck && bun run lint && bun run format:check`
  - `cd web && bun run i18n:sync && bun run build`

## 8. ai-fund 需求变更接入

- 在修改 ai-fund 产品代码前，针对当前 `.trellis/tasks/08-20-pool-user-limits` 执行需求变更流程，更新其 PRD、设计和实施计划，保留现有工作树改动。
- 扩展 `newapi_client.js` 的周限、统一状态和个人覆盖白名单调用及单测。
- 扩展 `pool_limits.js` 与 Worker 路由，保持服务端绑定用户 ID、本地管理员门禁、局部失败和缓存失效。
- 扩展 `/pools` 用户面板与管理员弹窗；在 `/admin` 增加独立“号池限制”页签，并复用同一管理工作区。
- ai-fund 个人覆盖通过 Worker 代理 NewAPI 最小用户搜索和统一状态接口；覆盖无请求记录用户、映射变化冲突和长期/临时覆盖场景。
- D1 只追加脱敏审计，不增加限额镜像表或到期任务。

## 9. ai-fund 与跨仓验证

- Worker：`node --test worker/src/newapi_client.test.js worker/src/pool_limits.test.js worker/src/index.test.js`。
- 前端：`cd frontend && npm run build`，并运行已有管理账务与号池相关 Node 测试。
- 桌面和移动视口验证普通用户、管理员、长期覆盖、临时覆盖、到期回落、周刷新和单池故障。
- 联调场景：
  - 渠道日限/周限/并发均关闭。
  - 同时开启日限与周限，分别触发 429。
  - 设置个人覆盖后立即放行，到期后回落且不取消在途请求。
  - 调整日/周已用额度不改变覆盖、钱包、订阅和日志。
  - ai-fund Worker 不返回 PAT、渠道密钥、完整渠道对象或 D1 内部信息。

## 10. 风险与回滚复核

- 重点复核原有热点改动是否仅为字段同步、检查和记录调用，没有顺手重构每日额度、并发或计费流程。
- 重点复核所有每日正向记录路径都有对应周记录，所有手工渠道切换都解析了个人覆盖。
- 重点复核缓存 TTL 不跨越到期时间，管理写后缓存不会长期保留旧值。
- ai-fund 上线必须晚于 NewAPI；回滚顺序相反。
