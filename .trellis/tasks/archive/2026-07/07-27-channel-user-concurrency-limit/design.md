# 技术设计 — 渠道单用户并发限制

## 1. 设计目标

在不改变现有渠道权重、亲和性和重试选择规则的前提下，为每个渠道增加按 `channel_id + user_id` 统计的并发上限。限制发生在选渠完成后、计费预扣和实际上游调用前；每次重试只占用当前尝试渠道的名额。

### 1.1 薄层架构边界

本功能遵循 build 分支的上游同步友好原则：完整定制逻辑进入新领域文件，原有上游热点只增加最薄的生命周期接入，不为本功能重构既有主链路。

- `service/channel_user_concurrency.go` 独占 Redis/内存存储、租约 ID、原子获取、心跳续租、幂等释放及领域错误。
- `controller/channel_user_concurrency.go` 独占 Gin context 读取、尝试 context 取消、协议错误转换和安全日志；对原调用方提供窄的“获取并持有当前渠道租约”入口。
- `controller/relay.go` 只在已确定渠道且完成请求准备后调用一次接入入口，并在尝试结束时释放；不解释 Redis key、TTL、错误分类或心跳状态。
- Responses WebSocket、Task、Midjourney、Claude `count_tokens` 等入口只在各自实际上游生命周期边界调用同一接入能力，不复制计数、错误映射或日志逻辑。
- `relay/vision_assist.go` 因包依赖边界不能反向依赖 Controller，只通过 service 的领域契约获取辅助渠道租约；其原有准备、映射、计费和失败策略保持不变。
- 前端字段接入沿用现有 Channel schema/form/drawer 扩展点，不抽取新的表单框架或改造无关渠道设置。
- 新增测试优先位于新的领域测试文件；只有验证现有入口契约时才对原测试做最小增补。

预期冲突面只包括模型字段、上下文注册、管理校验、各实际上游入口的一处生命周期调用和 Default 表单字段。每处旧文件修改必须能说明为何无法通过新增领域文件独立完成；回滚时删除新领域文件并撤销这些窄调用即可。

## 2. 配置与数据模型

在 `model.Channel` 增加可空整数列：

```go
UserConcurrencyLimit *int `json:"user_concurrency_limit"`
```

- `nil` 和 `0` 均表示不限，正整数表示同一用户在该渠道可同时占用的最大名额。
- 使用指针字段是为了让 GORM `Updates(channel)` 能保存显式 `0`，同时兼容历史记录的 `NULL`。
- `GetUserConcurrencyLimit()` 对 `nil`、负数等异常历史值统一返回 `0`。
- 管理 API 创建、更新统一校验 `0..1000` 的整数；更新请求显式传 `null` 时归一化为 `0`，避免 GORM 忽略 `nil` 后保留旧限制。
- `AutoMigrate(&Channel{})` 自动为 SQLite、MySQL、PostgreSQL 增加普通整数列，不使用数据库专属 SQL 或默认值。
- `middleware.SetupContextForSelectedChannel` 把归一化后的值写入新的渠道上下文键，初选和重试都从同一入口刷新配置。
- 新字段归类为非敏感运营配置，继续受现有 `ChannelWrite` 权限约束，不要求密钥级敏感写权限。

## 3. 租约服务

新增 `service/channel_user_concurrency.go`，只负责名额存储、续租与释放，不处理 HTTP 响应格式或渠道选择。

### 3.1 公共契约

```go
type ChannelUserConcurrencyLease struct { /* 私有状态 */ }

func AcquireChannelUserConcurrency(
    ctx context.Context,
    channelID int,
    userID int,
    limit int,
    onLost func(error),
) (*ChannelUserConcurrencyLease, error)

func (lease *ChannelUserConcurrencyLease) Release(ctx context.Context) error
```

- 所有导出类型和方法添加中文 GoDoc，并按项目规范显式包含 `@param`、`@return`，说明参数、返回值和幂等语义。
- `limit <= 0` 返回无操作租约，不访问 Redis。
- 每个租约使用随机 UUID，不能仅用计数器；这样单个请求可独立续租、释放和过期。
- `Release` 使用 `sync.Once` 保证幂等，并停止心跳。
- 服务暴露可用 `errors.Is` 判断的“达到上限”和“存储不可用/租约丢失”错误，Controller 再转换为协议错误。

### 3.2 Redis 模式

Redis key：

```text
channel_user_concurrency:{channel_id}:{user_id}
```

value 使用 Sorted Set：member 为租约 UUID，score 为租约过期毫秒时间戳。Lua 脚本完成以下原子操作：

1. 获取：删除已过期 member，读取有效数量；未达上限时写入新租约并刷新 key TTL，否则返回超限。
2. 续租：仅当 member 仍存在时更新 score 和 key TTL；member 已过期或丢失时返回租约丢失。
3. 释放：删除当前 member；集合为空时删除 key。

租约 TTL 设为 120 秒，心跳间隔 30 秒。Redis 调用使用短超时上下文，避免 Redis 故障长期阻塞 Relay。获取失败返回不可用错误；续租失败调用 `onLost` 取消 HTTP 上游上下文或关闭 WebSocket；释放失败只记录告警并依赖 TTL 回收，不能把已经成功的响应改写成失败。

### 3.3 无 Redis 模式

进程内存储使用互斥锁保护的 `map[channelID:userID]map[leaseID]expiresAt`：

- 获取前清理过期租约并原子检查数量。
- 心跳更新当前租约过期时间。
- 释放删除当前租约和空 bucket。
- 仅保证当前进程内准确；多实例未配置 Redis 时上限可能按实例倍增，保持 PRD 中已确认的降级语义。

## 4. Controller 接入与错误契约

新增 `controller/channel_user_concurrency.go`，集中完成：

- 从 Gin context 读取当前渠道、用户和限制。
- 创建可取消的上游请求 context。
- 把 service 错误转换为 `types.NewAPIError`：
  - 超限：HTTP `429`，`channel_user_concurrency_exceeded`。
  - Redis 不可用或租约丢失：HTTP `503`，`channel_user_concurrency_unavailable`。
- 两类错误均设置 `skipRetry`，且错误码不使用 `channel:` 前缀，避免 `types.IsChannelError` 抢在 `skipRetry` 前触发重试，也避免 `ShouldDisableChannel` 自动禁用渠道。
- 记录包含 request ID、channel ID、user ID、limit 的 `LogWarn`，不得记录 Token、Key、请求体或 Redis 地址。
- 在错误日志开启时沿用现有安全错误日志；不调用 `processChannelError`。

### 4.1 主 Relay

每次循环尝试的数据流：

```text
选渠并刷新 context
  -> 请求转换/视觉辅助预处理
  -> 获取当前渠道并发租约
  -> 计费预扣
  -> 调用上游并读取流/响应
  -> 释放当前尝试租约
  -> 成功返回，或处理真实上游错误后重试
```

租约使用尝试级 `defer` 释放，保证转换失败、计费失败、网络错误、客户端取消和 panic 均不泄漏。超限/Redis 错误在预扣前直接结束，不进入 `processChannelError` 或重试。视觉辅助在主渠道租约获取前执行，并按辅助渠道单独获取租约，避免辅助渠道与主渠道相同时自占两个名额造成死锁。

### 4.2 异步任务与特殊 Relay

- `RelayTask`：每次提交尝试在 `RelayTaskSubmit` 的价格计算/预扣前获取名额，HTTP 响应完成后释放；重试前释放旧渠道。
- `RelayTaskFetch`：只有 Gemini/Vertex 等确实访问上游的实时查询分支获取名额；纯数据库查询不占用。
- `RelayMidjourney`：在价格检查和 `DoMidjourneyHttpRequest` 前获取名额；从历史任务切换到原渠道时必须使用原渠道的限制，而不是初始分发渠道。
- Claude `count_tokens`：在构造完成、`DoRequest` 前获取名额，无计费但仍受渠道容量保护。
- 视觉辅助：在辅助请求预扣前按辅助渠道获取名额，完成后释放。

任务和 Midjourney 保持各自既有错误体；新增错误仍携带稳定字符串错误码或现有格式允许的等价字段。任何本地并发错误均不作为上游错误处理。

## 5. WebSocket 生命周期

### 5.1 OpenAI Realtime

Realtime 仍由主 `Relay` 循环处理。租约在选渠和请求准备后获取，持续到 `WssHelper` 返回，即客户端或上游 WebSocket 整体关闭。连接内事件不重复计数。

### 5.2 Responses WebSocket

- 首帧解析并选定渠道后、上游 dial 和预扣前获取一个连接级租约。
- `connectResponsesWebSocketTurn` 若在首轮真实上游失败后切换渠道，先释放旧租约，再为候选渠道获取新租约。
- 成功建立后，整个多轮连接复用同一租约；后续 `response.create` 不重复获取。
- 续租失败时关闭当前上游连接并通过现有 WebSocket 错误/关闭路径结束客户端连接。
- 最外层 `defer` 保证正常关闭、异常关闭和客户端断开都释放租约。

## 6. 前端设计

仅修改 `web/default` 渠道新增/编辑表单：

- `Channel` schema 增加 `user_concurrency_limit: z.number().nullish()`。
- 表单 schema 增加整数、最小 `0`、最大 `1000` 校验，默认值为 `0`。
- 编辑旧渠道时 `null/undefined -> 0`。
- 创建和更新 payload 都使用 `?? 0`，保留显式零值。
- 在高级设置的 `Routing Strategy` 区域增加数字输入，固定 `min=0`、`max=1000`、`step=1`，并纳入高级设置错误与已配置状态判断。
- 文案说明：`0` 表示不限；按用户合并所有 Token；WebSocket 在连接关闭前持续占用。
- 所有新增文案使用 `t(...)`，通过临时 `add-missing-keys.mjs` 写入 `en/zh/fr/ja/ru/vi` 后运行 `bun run i18n:sync`，不得直接编辑 locale JSON。

Classic 未增加配置控件，但其更新请求未携带该指针字段时 GORM 会保留原值，不会把已配置限制清空。

## 7. 测试策略

- Model/Controller：验证创建、更新、显式 `0`、显式 `null`、负数、小数、超过 `1000`、权限字段分类和缓存刷新。
- Service 内存模式：验证同用户同渠道上限、跨 Token 共享、用户/渠道隔离、幂等释放、过期清理、心跳和并发竞争。
- Redis 模式：用可控 Redis 测试实例验证 Lua 获取原子性、单租约续期/释放、过期回收和故障返回；测试不得依赖 sleep 竞争，以显式时间或短可控 TTL 为准。
- Main Relay：验证第 `N+1` 个请求不预扣、不调用上游、不重试、不自动禁用；真实上游重试释放旧渠道并获取新渠道。
- 流式/WebSocket：验证连接全生命周期占位、客户端断开释放、多轮不重复计数、续租丢失关闭连接。
- 特殊入口：Task、Midjourney、Claude count_tokens、视觉辅助各至少覆盖一次超限前置拒绝；数据库任务查询不占用。
- 前端：验证字段 round-trip、显式 `0`、边界校验和控件可访问名称。

## 8. 发布与回滚

发布顺序：

1. 部署含数据库迁移和新代码的版本，确认 Redis 正常。
2. 通过管理 API 或数据库把生产渠道 `80` 的 `user_concurrency_limit` 更新为 `4`。
3. 刷新/确认渠道缓存已加载新值。
4. 使用同一测试用户制造 4 个保持中的请求，第 5 个应收到 `429`；同时验证另一用户仍可进入。
5. 观察请求日志、Redis key TTL、渠道错误率和上游并发。

紧急回滚优先把渠道配置改为 `0`，可立即关闭限制且无需回滚数据库列；代码回滚后新增列保留不会影响旧版本。Redis key 带 TTL，可等待自然回收，无需批量删除。

## 9. 主要风险与防护

| 风险 | 防护 |
| --- | --- |
| GORM 忽略显式 `0` | 可空指针字段；前端和 patch 归一化保留 `0`；增加持久化测试。 |
| Redis 计数因崩溃泄漏 | 独立租约 UUID、Sorted Set 过期时间、心跳和 key TTL。 |
| Redis 故障时每实例各自放行 | Redis 已配置时严格失败关闭，不切换内存模式。 |
| 本地错误触发换渠或自动禁用 | 非 `channel:` 错误码、`skipRetry`、不调用 `processChannelError`。 |
| 重试跨渠道串计数 | 尝试级租约，切渠前释放，测试旧/新渠道计数。 |
| 视觉辅助与主渠道相同发生自阻塞 | 主渠道在视觉辅助预处理完成后才获取租约。 |
| WebSocket 心跳丢失后继续占用上游 | `onLost` 关闭上游连接，TTL 回收 Redis 残留。 |
| 高并发日志风暴 | 超限仅写简洁请求级告警，不记录请求内容；后续可按现有日志采样策略优化。 |
| 定制逻辑侵入上游热点 | 核心逻辑集中在新领域文件；旧文件只保留一次窄生命周期调用，并在检查阶段逐文件复核必要性与 diff。 |
