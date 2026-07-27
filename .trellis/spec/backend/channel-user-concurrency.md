# 渠道单用户并发限制契约

> 记录渠道配置、Redis/内存租约、Relay 生命周期、错误语义与请求取消传播，防止单个用户占满同一上游渠道。

## 场景：按渠道与用户限制实际上游并发

### 1. Scope / Trigger

- Trigger：修改渠道 `user_concurrency_limit` 字段、渠道缓存上下文、并发租约存储、Relay 重试、流式/非流式 HTTP、Realtime/Responses WebSocket、异步任务实时查询、Midjourney、Claude `count_tokens` 或视觉辅助上游调用。
- 统计维度固定为 `channel_id + user_id`；同一用户的多个 API Token 共享限制，不同用户或不同渠道互不影响。
- 限制只覆盖实际上游调用。管理端渠道测试、纯数据库任务查询和异步任务在上游响应结束后的后台运行时间不占名额。
- 定制逻辑必须保持薄层：Redis、内存、续租和释放归属 `service/channel_user_concurrency.go`；协议错误与 Gin 生命周期归属独立 Controller/Relay 领域文件；既有热点只保留获取、持有、取消和释放调用。

### 2. Signatures

渠道模型与请求上下文：

```go
type Channel struct {
	UserConcurrencyLimit *int `json:"user_concurrency_limit"`
}

func (channel *Channel) GetUserConcurrencyLimit() int

const ContextKeyChannelUserConcurrencyLimit ContextKey = "channel_user_concurrency_limit"
```

租约服务：

```go
var ErrChannelUserConcurrencyExceeded error
var ErrChannelUserConcurrencyUnavailable error

func AcquireChannelUserConcurrency(
	ctx context.Context,
	channelID int,
	userID int,
	limit int,
	onLost func(error),
) (*ChannelUserConcurrencyLease, error)

func (lease *ChannelUserConcurrencyLease) IsLost() bool
func (lease *ChannelUserConcurrencyLease) LostSignal() <-chan struct{}
func (lease *ChannelUserConcurrencyLease) Release(ctx context.Context) error
```

任务查询的可选上下文能力：

```go
type TaskContextFetcher interface {
	FetchTaskWithContext(
		ctx context.Context,
		baseUrl, key string,
		body map[string]any,
		proxy string,
	) (*http.Response, error)
}

func FetchTaskWithContext(
	ctx context.Context,
	adaptor TaskAdaptor,
	baseUrl, key string,
	body map[string]any,
	proxy string,
) (*http.Response, error)
```

稳定错误码：

```go
ErrorCodeChannelUserConcurrencyExceeded    = "channel_user_concurrency_exceeded"
ErrorCodeChannelUserConcurrencyUnavailable = "channel_user_concurrency_unavailable"
```

### 3. Contracts

#### 配置与持久化

- `user_concurrency_limit` 只接受整数 `0..1000`；`nil`、历史 `NULL` 和 `0` 均表示不限。
- `GetUserConcurrencyLimit()` 对 `nil`、零和异常负值统一返回 `0`。
- 更新请求显式传 `null` 时归一化为 `0`；创建和更新 payload 必须使用保留显式零值的写法，不能用 truthy 判断丢弃 `0`。
- 字段是普通可空整数列，依赖 `AutoMigrate`，不得增加数据库方言专属 SQL 或布尔默认值。
- `middleware.SetupContextForSelectedChannel` 每次初选或重试选渠后都必须把归一化值写入 `ContextKeyChannelUserConcurrencyLimit`。
- Default 表单默认值为 `0`，Zod 校验整数和 `0..1000`，数字输入固定 `min=0`、`max=1000`、`step=1`，文案必须走 i18n。

#### 租约存储

- `limit <= 0` 返回无操作租约且不访问 Redis；受限请求要求 `channelID > 0` 且 `userID > 0`。
- Redis 未配置时使用进程内互斥存储，只保证单实例上限；Redis 已配置但客户端或操作不可用时失败关闭，禁止降级到内存继续放行。
- Redis key 固定为 `channel_user_concurrency:{channel_id}:{user_id}`，使用 Sorted Set；member 是 UUID，score 是租约过期毫秒。
- 获取、续租和释放分别使用原子 Lua 脚本。获取时先删除过期 member，再检查数量；集合为空时释放脚本删除 key。
- 租约 TTL 为 `120s`，心跳间隔为 `30s`，单次 Redis 操作超时为 `2s`。
- `Release` 必须幂等，并用 `context.WithoutCancel` 派生释放上下文；释放失败只记录告警并依赖 TTL 回收，不能改写已经完成的成功响应。
- 续租失败或 member 丢失时只触发一次 `onLost`、关闭 `LostSignal()` 并将租约标记为 lost。

#### Relay 生命周期

- 获取时机固定为：渠道已选定且请求准备完成之后，预扣费和实际上游调用之前。
- HTTP 流式和非流式请求持有到该次上游响应处理结束；重试前必须释放旧渠道租约，再为新渠道获取。
- OpenAI Realtime 与 Responses WebSocket 按连接持有；同一连接内多轮请求只获取一次，连接关闭、客户端断开或续租丢失时释放。
- 异步任务提交和实时查询只在实际 HTTP 请求期间持有；纯数据库查询和上游后台处理期不持有。
- Midjourney 从历史任务切回原渠道时，必须同步原渠道 ID 与限制后再获取。
- 视觉辅助按实际辅助渠道和当前用户独立获取；主渠道必须在视觉辅助预处理完成后再获取，避免同渠道自占两个名额。
- 本地超限或租约不可用必须发生在计费与上游调用前，不得进入渠道自动禁用逻辑。

#### 取消传播

- 获取 guard 后会把尝试级可取消 context 写回 `c.Request`；租约丢失时取消该 context。
- 创建上游 `http.Request` 时必须使用当前 `c.Request.Context()`，或在获取 guard 后重新执行 `req = req.WithContext(c.Request.Context())`。
- 通用 HTTP 请求入口必须在发送前把 Gin 请求 context 绑定到实际 `req`；WebSocket 必须使用 `DialContext`。
- 任务适配器优先实现 `TaskContextFetcher`；兼容旧适配器的 fallback 只用于尚未迁移的实现，新增适配器不得忽略传入 context。
- Vertex 服务账号令牌交换、Gemini/Vertex 任务查询、Midjourney HTTP 等前置网络调用也属于上游生命周期，必须使用同一可取消 context。

### 4. Validation & Error Matrix

| 条件 | HTTP/错误码 | 重试、计费与渠道行为 |
| --- | --- | --- |
| `nil`、历史 `NULL` 或 `0` | 无错误 | 不限流，不访问租约存储 |
| 配置小于 `0`、含小数或大于 `1000` | 管理 API 参数错误 | 不保存配置 |
| 同渠道同用户达到上限 | `429 channel_user_concurrency_exceeded` | 不排队、不预扣、不访问上游、不重试、不自动禁用 |
| Redis 已启用但不可用 | `503 channel_user_concurrency_unavailable` | 失败关闭，不回退内存、不预扣、不访问上游、不重试、不自动禁用 |
| 运行中租约续租失败或丢失 | `503 channel_user_concurrency_unavailable`，若响应已提交则终止连接 | 取消实际上游请求或关闭 WebSocket，记录告警 |
| 释放 Redis member 失败 | 保留当前业务结果 | 记录脱敏告警，依赖 TTL 回收 |
| Task/Midjourney 本地并发错误 | 保留各自协议错误体，并映射 `429/503` | 必须标记本地错误，不能按普通 `429/5xx` 重试 |

两类并发错误都必须设置 `skipRetry`，错误码不得使用 `channel:` 前缀，也不得传入 `processChannelError`。日志只包含 request ID、channel ID、user ID、limit 和脱敏错误摘要，不得包含 API Key、Redis 地址、请求体或响应体。

### 5. Good / Base / Bad Cases

- Good：渠道 `80` 配置为 `4`，同一用户用多个 Token 保持 4 个请求，第 5 个立即收到 `429`；另一用户仍可进入。
- Good：首个上游渠道真实失败，释放旧租约后重试到新渠道；两个渠道的计数互不串联。
- Good：Responses WebSocket 完成普通、Compact、普通三轮请求，整个客户端连接只获取一次租约。
- Good：Redis 续租失败后取消 Vertex OAuth/任务查询或关闭 WebSocket，而不是只修改 Gin context 中未被上游请求使用的值。
- Base：历史渠道缺少新字段，所有 Relay 路径保持原行为。
- Base：异步任务提交成功后立即释放；任务在上游继续运行不占并发名额。
- Bad：Redis 故障时回退内存，会让多实例分别放行并突破全局上限。
- Bad：在视觉辅助前先获取主渠道租约，辅助渠道与主渠道相同时可能自阻塞。
- Bad：Task 本地 `503` 先进入通用 `5xx` 判断再检查 `LocalError`，会错误换渠重试。

### 6. Tests Required

- Model/Controller：覆盖 `NULL`、正数、显式 `0/null`、负数、小数、`1001`、非敏感字段分类和持久化往返。
- Middleware：断言每次选渠将归一化限制写入 Gin context。
- Service 内存模式：断言同用户同渠道上限、用户/渠道隔离、过期清理、幂等释放和不限流时跳过 Redis。
- Service Redis 模式：断言 Lua 获取原子性、并发竞争不突破上限、续租、释放、member 丢失和 Redis 不可用时失败关闭。
- Controller/Relay：断言 `429/503` 的稳定错误码、`skipRetry`、不自动禁用、不预扣和不上游。
- 上下文传播：断言通用 HTTP、Realtime WebSocket、Claude `count_tokens`、Midjourney、Gemini/Vertex 任务查询及 Vertex OAuth 在取消后不发出上游请求或及时返回取消错误。
- Responses WebSocket：断言首轮重试释放旧租约，多轮连接只获取一次，断开后释放。
- 视觉辅助：断言并发拒绝发生在辅助上游和预扣之前。
- 前端：断言 API/表单 `4 -> 4`、历史 `null -> 0`、创建/更新保留 `0`，非法边界拒绝；运行 i18n 同步、类型检查和构建。
- 跨层修改完成后运行相关 race 测试、`go test ./...`、定向 `go vet` 和 `git diff --check`。

### 7. Wrong vs Correct

#### Wrong：只取消 Gin context，实际请求仍使用旧 context

```go
guard, apiErr := acquireChannelUserConcurrencyGuard(c)
if apiErr != nil {
	return apiErr
}
resp, err := client.Do(req)
```

问题：如果 `req` 在获取 guard 前已经创建，它仍持有旧 context；续租失败不会取消真实上游请求。

#### Correct：发送前绑定当前尝试 context

```go
guard, apiErr := acquireChannelUserConcurrencyGuard(c)
if apiErr != nil {
	return apiErr
}
req = req.WithContext(c.Request.Context())
resp, err := client.Do(req)
```

通用发送入口也必须执行同样绑定；WebSocket 使用 `DialContext(c.Request.Context(), ...)`。

#### Wrong：先按状态码判断任务重试

```go
if taskErr.StatusCode/100 == 5 {
	return true
}
if taskErr.LocalError {
	return false
}
```

#### Correct：本地错误优先终止

```go
if taskErr.LocalError {
	return false
}
if taskErr.StatusCode/100 == 5 {
	return true
}
```

本地并发保护不是上游故障，必须在通用 `429/5xx` 重试分支之前返回。
