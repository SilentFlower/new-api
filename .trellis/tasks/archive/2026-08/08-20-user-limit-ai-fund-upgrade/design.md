# 技术设计

## 1. 总体边界

采用 NewAPI 执行面 + 两套管理界面的结构：

1. NewAPI 主数据库持久化渠道默认值和个人覆盖记录。
2. NewAPI Relay 在每次实际选渠后解析当前用户的有效并发、日限和周限，并在预扣或上游调用前执行限制。
3. Redis/内存只保存日、周使用量和并发租约；它们不成为个人覆盖的事实源。
4. NewAPI React 管理端消费原生管理 API。
5. ai-fund Cloudflare Worker 作为 BFF 消费相同 API，D1 仅保存号池映射与脱敏审计。

限制事实不从 CF 回流到 Relay 热路径，避免外部依赖、双写和过期状态漂移。

## 2. 数据模型

### 2.1 渠道默认周限

在 `model.Channel` 增加可空整数 `UserWeeklyQuotaLimit`，JSON 字段为 `user_weekly_quota_limit`。

- `nil`、历史 `NULL`、`0` 和异常负值读取时表示不限。
- 写入范围为 `0..common.MaxQuota`。
- 依赖 GORM `AutoMigrate`，不增加数据库默认值或方言专属 SQL。
- 渠道创建、更新、复制、缓存、审计和前端表单都保留显式 `0`。

### 2.2 个人覆盖记录

新增独立模型 `ChannelUserLimitOverride`，一条记录对应一个 `channel_id + user_id`：

| 字段 | 语义 |
| --- | --- |
| `id` | GORM 主键 |
| `channel_id` | 渠道 ID，与 `user_id` 组成唯一索引 |
| `user_id` | NewAPI 用户 ID |
| `user_concurrency_limit` | 可空绝对并发上限 |
| `user_daily_quota_limit` | 可空绝对每日额度上限 |
| `user_weekly_quota_limit` | 可空绝对每周额度上限 |
| `expires_at` | Unix 秒；`0` 表示持续有效 |
| `updated_by` | 最后操作管理员用户 ID |
| `created_at` / `updated_at` | Unix 秒审计时间 |

数据库约束与归一化：

- 三个覆盖字段都为空时删除记录，不保存空壳。
- 并发覆盖范围为 `1..1000`，额度覆盖范围为 `1..common.MaxQuota`。
- 对应渠道默认值为 `0`（不限）时拒绝该维度覆盖。
- 提交时覆盖值必须高于当前渠道默认值；渠道默认值后续变化时，运行时仍按 `max(base, override)` 计算。
- `expires_at` 只允许 `0` 或未来 Unix 秒；不支持未来 `starts_at`。
- 到期记录无需定时任务即可失效；列表和读取按 `expires_at=0 OR expires_at>now` 判断，过期空壳可在后续写入或低频清理中删除。
- 新模型同时加入普通和快速迁移列表，兼容 SQLite、MySQL 与 PostgreSQL。

## 3. 有效限制解析与缓存

新增服务返回稳定结构：

```text
ChannelUserEffectiveLimits
  BaseConcurrency / EffectiveConcurrency
  BaseDailyQuota / EffectiveDailyQuota
  BaseWeeklyQuota / EffectiveWeeklyQuota
  Override values / ExpiresAt / Active
```

解析规则：

1. 读取渠道当前默认值。
2. 从独立覆盖缓存读取 `channel_id + user_id`；未命中时查询主数据库并回填正向或负向缓存。
3. 记录未过期时，对每个非空字段计算 `max(base, override)`。
4. `base=0` 始终表示不限，覆盖不改变该维度。
5. 缓存 TTL 不超过 30 秒，并且不得超过 `expires_at`；这样缓存值不会跨越到期边界。
6. Redis 启用时使用共享缓存；未启用时使用有界进程内缓存。管理写入提交后立即删除或刷新对应缓存键。
7. Relay 解析缓存和数据库同时失败时使用渠道默认值并写请求关联的脱敏告警。覆盖只能放宽限制，因此回落到默认值是更保守的故障行为。
8. 管理查询不隐藏数据库错误，直接返回本地化管理错误，避免页面展示错误的覆盖状态。

不把覆盖字段塞入现有用户鉴权缓存，避免限制配置与认证版本、钱包额度产生耦合。

## 4. 每周额度状态

新增 `service/channel_user_weekly_quota.go`，保持与每日额度相同的独立模块边界，不重构现有每日实现为通用框架。

- 统计维度：`channel_id + user_id + 服务端自然周`。
- 周起点：服务端 `time.Local` 的周一 `00:00`；`reset_at` 为下周一 `00:00`。
- Redis key：`channel_user_weekly_quota:{channel_id}:YYYY-MM-DD`，日期取本周周一。
- Redis 类型：Hash；field 为十进制用户 ID，value 为正向已结算额度。
- TTL 覆盖本周结束后至少七天；累计和目标值覆盖使用原子 Lua。
- 未配置 Redis时使用互斥进程内状态；Redis 已配置但不可用时，正数限额检查失败关闭。
- 周限关闭时仍记录正向额度，使管理员在本周中途开启限制后可以使用已有累计。
- 退款、负向差额和管理员退款不减少周累计。

错误码新增：

- `channel_user_weekly_quota_exceeded` -> 429。
- `channel_user_weekly_quota_unavailable` -> 503。

`relaykit/` 只增加稳定错误码，不引入根模块依赖。

## 5. Relay 数据流

```text
用户认证
  -> 选择实际渠道
  -> 读取渠道默认值 + 解析个人覆盖
  -> 把有效并发/日限/周限写入 Gin context
  -> 价格计算
  -> 免费请求跳过周期限额
  -> 检查每日已结算额度
  -> 检查每周已结算额度
  -> 预扣费
  -> 获取并发租约
  -> 实际上游调用
  -> 一次性结算
  -> 同时记录日、周正向额度
```

- 每次真实换渠重试都重新解析目标渠道覆盖，不能复用上一渠道值。
- Midjourney 历史渠道、异步任务、视频代理和视觉辅助的手工渠道上下文必须同步周限和个人有效值。
- 覆盖撤销或到期不取消在途请求；后续新请求按回落后的限制处理。
- 周限检查与现有每日检查相邻，记录与现有每日正向记录相邻，避免遗漏独立计费路径。

## 6. NewAPI 管理 API

### 6.1 兼容扩展

保留现有接口：

- `GET /api/channel/:id/user-daily-quota`
- `PUT /api/channel/:id/user-daily-quota/:user_id`
- `GET /api/channel/:id/user-concurrency`

列表顶层 `limit` 保持渠道默认值语义；条目中的 `limit` 改为该用户实际有效值，并新增可选字段 `base_limit`、`override_limit`、`override_expires_at`。旧调用方只读 `limit` 时会自然获得正确有效值。

### 6.2 周限接口

```text
GET /api/channel/:id/user-weekly-quota
  permission: ChannelRead

PUT /api/channel/:id/user-weekly-quota/:user_id
  permission: ChannelOperate
  body: {"used_quota": 500000}
```

响应结构与每日额度一致，`reset_at` 指向下周一。

### 6.3 指定用户统一状态

```text
GET /api/channel/:id/user-limit-status/:user_id
  permission: ChannelRead
```

响应只包含最小用户摘要，以及三种限制的渠道默认值、个人覆盖值、实际有效值、当前并发或周期已用、剩余值、刷新时间和覆盖到期时间。该接口供 NewAPI 个人覆盖 Dialog 和 ai-fund 本人视图使用，避免从活跃列表分页中推断用户状态。

### 6.4 个人覆盖管理

```text
GET /api/channel/:id/user-limit-overrides
  permission: ChannelRead

PUT /api/channel/:id/user-limit-overrides/:user_id
  permission: ChannelOperate
  body: {
    "user_concurrency_limit": 8,
    "user_daily_quota_limit": 10000000,
    "user_weekly_quota_limit": 50000000,
    "expires_at": 1787241600
  }

DELETE /api/channel/:id/user-limit-overrides/:user_id
  permission: ChannelOperate
```

- PUT 是整条覆盖记录的替换；三个限制字段允许 `null`，`expires_at=0` 表示持续有效。
- 列表只返回当前有效覆盖，按用户 ID 稳定排序并分页。
- 写入前批量读取最小用户摘要确认用户存在。
- 审计 action 使用 `channel.user_limit_override_upsert` 与 `channel.user_limit_override_delete`，记录 channel/user、非敏感限制值和到期时间。

## 7. NewAPI 管理端

### 7.1 渠道表单

- 新增 `ChannelUserWeeklyQuotaLimitField`，与每日字段共享金额换算、步长和显式空字符串处理规则。
- 更新 Zod schema、默认值、API 回填、创建/更新/复制 payload 和类型定义。
- 并发、日限、周限作为同一组紧凑字段展示，不新建独立设置页面。

### 7.2 用户限制 Dialog

在现有 Dialog 增加四个 Tabs：每日额度、每周额度、当前并发、个人覆盖。

- 日/周/并发列表展示有效值；存在覆盖时显示覆盖状态和到期时间。
- 个人覆盖 Tab 列出当前有效覆盖，支持搜索用户并打开编辑弹窗。
- 用户搜索直接使用管理员用户查询，不要求目标用户先出现在日、周或并发活跃列表；统一状态接口对无记录用户返回零使用状态。
- 日、周、并发活跃行增加“临时调高”快捷动作，复用同一编辑弹窗并自动选中目标用户。
- 编辑弹窗提供三个可选限制输入与一个“设置到期时间”开关；关闭开关提交 `expires_at=0`。
- 输入旁显示渠道默认值，提交前校验覆盖严格高于默认值。
- 撤销覆盖使用确认 Dialog；提交中禁止重复操作。
- 继续使用 React Query 的细粒度 query key，写成功后只失效当前渠道限制数据。
- 复用现有 Base UI Dialog、Tabs、Table、Input、Checkbox/Switch、Combobox、Tooltip 和 Hugeicons，不引入新的 UI 依赖。

所有文案使用 `t(...)`。Locale 写入必须通过 `web/scripts/add-missing-keys.mjs`，随后执行 `bun run i18n:sync` 和源码缺键扫描。

## 8. ai-fund / Cloudflare

### 8.1 Worker 客户端

扩展现有 `worker/src/newapi_client.js`：

- 读取/更新渠道周限。
- 读取周额度列表并设置本周已用额度。
- 读取指定用户统一限制状态。
- 列出、替换和撤销个人覆盖。
- 渠道更新白名单扩展为 `id + user_concurrency_limit + user_daily_quota_limit + user_weekly_quota_limit`，禁止展开完整渠道对象。
- 个人覆盖写入也显式构造固定字段，不能透传浏览器对象。

### 8.2 Worker 业务与路由

- `GET /api/pools/limits` 对 Claude/OpenAI 各调用一次指定用户统一状态接口，继续使用服务端绑定的 `newapi_user_id`。
- `/api/admin/pools/:poolId/user-weekly-quota` 提供周额度活跃列表。
- `/api/admin/pools/:poolId/users/:userId/weekly-quota` 调整本周已用额度。
- 为指定 pool 提供用户搜索、统一状态、覆盖列表、PUT 与 DELETE 路由；搜索条件只接受用户 ID、用户名或显示名，目标用户不依赖本地用量记录。
- 所有个人覆盖写操作携带页面确认时的 `expected_channel_id`，Worker 写前重读 D1 映射；所有管理路由使用本地管理员门禁和 D1 脱敏审计。
- 写成功后失效对应 pool 与用户缓存；D1 不保存有效限额副本。

### 8.3 页面

- `PoolLimitPanel.vue` 增加本周已用、剩余、上限和刷新时间；有效值来自 NewAPI 统一状态。
- `PoolLimitAdminModal.vue` 增加渠道周限和周额度列表，保留当前“调整已用金额”操作。
- `Admin.vue` 增加“号池限制”页签，以嵌入式方式复用 `/pools` 的同一管理工作区；`/pools` 继续使用弹窗壳。
- 用户不需要先在号池产生请求；管理员通过 NewAPI 最小用户搜索选择目标用户，再读取统一状态并配置覆盖，不从日/周/并发活跃列表推断用户身份。
- 个人覆盖与钱包直接调额、订阅额度、周期使用量修正保持不同入口和文案。

## 9. 兼容、迁移与上游同步

- 新渠道字段和新表均由 GORM 迁移，旧数据默认不限且无覆盖。
- 旧每日/并发 API 路径不删除，现有 ai-fund 实现可在 NewAPI 升级期间继续工作。
- NewAPI 新业务优先放在独立 model/service/controller/frontend 文件；原有渠道模型、路由、选渠、计费和 Dialog 只增加必要接入。
- 不抽取或重写现有每日额度模块；周限允许少量镜像实现，并在设计与测试中列出每日模块为同步复核源。
- ai-fund 必须在当前 `08-20-pool-user-limits` 工作树上增量修改，不能重建或覆盖已有组件。

## 10. 运维与回滚

上线顺序：

1. 先部署 NewAPI，完成数据库迁移并提供兼容 API。
2. 验证现有 ai-fund 每日/并发功能不受影响。
3. 再部署 ai-fund 周限和个人覆盖页面。

回滚 ai-fund 不影响 NewAPI 执行；回滚 NewAPI 前必须先回滚依赖新接口的 ai-fund。新表和新列可保留，旧版本会忽略它们；回滚不删除使用量或覆盖数据。
