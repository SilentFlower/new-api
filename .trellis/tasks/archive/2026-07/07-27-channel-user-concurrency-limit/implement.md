# 实施计划 — 渠道单用户并发限制

## 1. 开发前确认

- [ ] 展示并确认最终 `brief.md` 后运行 `task.py start`，planning 状态不修改业务代码。
- [ ] 进入 Phase 2 时调用 `trellis-route(target=implement)`，按路由结果使用实现子代理。
- [ ] 实现代理先读取 `implement.jsonl`、`prd.md`、`design.md`、`implement.md` 和涉及文件中的实体/DTO 定义。
- [ ] 保存当前 `git status --short`，不覆盖 `docker-compose.yml` 和其他任务目录中的用户改动。
- [ ] 按上游同步友好规范列出“新领域文件 / 必改旧文件 / 每个旧文件唯一必要接入点”，实现期间不扩大该清单。

## 2. 后端实施顺序

### 2.1 渠道字段和管理 API

- [ ] 在 `model.Channel` 增加 `UserConcurrencyLimit *int` 与中文 GoDoc 的 `GetUserConcurrencyLimit()`；导出方法注释显式包含 `@param`、`@return`。
- [ ] 依赖现有 `AutoMigrate(&Channel{})` 完成三库迁移，不新增方言 SQL或数据库默认值。
- [ ] 在 `validateChannel` 校验 `0..1000`；更新请求显式 `null` 归一化为 `0`。
- [ ] 把新字段加入 `channelNonSensitiveFields`、管理审计变更字段和相关字段分类测试。
- [ ] 增加 `ContextKeyChannelUserConcurrencyLimit`，由 `SetupContextForSelectedChannel` 在初选和重试时写入。
- [ ] 增加 Model/Controller 测试，覆盖历史 `NULL`、正数、显式 `0/null`、非法边界、更新持久化和缓存刷新。

### 2.2 租约服务

- [ ] 新建 `service/channel_user_concurrency.go`，实现 Redis Sorted Set 租约和无 Redis 内存租约；所有导出类型和方法使用包含 `@param`、`@return` 的中文 GoDoc。
- [ ] 使用三个原子 Lua 脚本实现获取、续租、释放；Redis key 只包含 channel ID 和 user ID。
- [ ] 实现 120 秒 TTL、30 秒心跳、Redis 操作短超时、UUID lease ID 和幂等释放。
- [ ] 续租失败或租约丢失调用一次 `onLost`；释放失败仅返回错误供调用方告警，依赖 TTL 回收。
- [ ] 增加 service 测试，覆盖上限竞争、隔离、过期、续租、幂等释放、Redis 故障和无 Redis 模式。

### 2.3 错误与 Controller 公共接入

- [ ] 在 `types/error.go` 增加 `channel_user_concurrency_exceeded` 和 `channel_user_concurrency_unavailable` 常量，保持无 `channel:` 前缀。
- [ ] 新建 `controller/channel_user_concurrency.go`，集中读取上下文、获取租约、转换 `429/503` 错误和写安全告警。
- [ ] 为 HTTP 请求建立可取消的尝试 context；租约丢失时取消上游请求。
- [ ] 测试两类错误均 `skipRetry`、不被 `ShouldDisableChannel` 判为自动禁用条件，并保留 OpenAI/Claude 错误码。

### 2.4 主 Relay 和重试

- [ ] 在 `controller.Relay` 每次尝试完成请求准备后、`prepareMainRelayBilling` 前获取当前渠道租约。
- [ ] 用尝试级闭包或等价结构确保任意返回、错误和 panic 都释放租约，且释放发生在下一次选渠前。
- [ ] `controller/relay.go` 只调用 Controller 并发领域入口并持有返回的租约，不直接解释 Redis、TTL、心跳或错误类型。
- [ ] 超限/Redis 错误直接结束并记录安全错误日志，不调用 `processChannelError`。
- [ ] 验证流式、非流式、OpenAI Realtime 和真实上游重试生命周期。
- [ ] 增加回归测试：不预扣、不调用上游、不重试、不自动禁用；跨渠道重试计数不串联。

### 2.5 Responses WebSocket

- [ ] 在首轮选渠后、`connectResponsesWebSocketTurn` dial/预扣前获取连接级租约。
- [ ] 调整 connector 的返回状态，使重试切渠时可以释放旧租约并把新租约交给外层连接生命周期持有。
- [ ] WebSocket 原有文件只保留连接级获取、换渠和释放调用，复用统一错误转换，不复制租约实现。
- [ ] 多轮连接复用同一租约；后续 turn 不重新计数。
- [ ] 续租失败关闭当前上游连接并走现有客户端关闭/错误路径。
- [ ] 补充首轮超限、重试换渠、多轮复用、客户端断开和租约丢失测试。

### 2.6 异步任务和特殊入口

- [ ] `RelayTask` 每次提交尝试在预扣前获取租约，HTTP 提交结束释放。
- [ ] Gemini/Vertex 实时任务查询只在实际 `FetchTask` 时获取原任务渠道租约，纯数据库查询不占用。
- [ ] Midjourney 提交/动作在确定最终渠道后、价格与网络调用前获取租约；查询路径仅实际访问上游时占用。
- [ ] Claude `count_tokens` 在 `DoRequest` 前获取并在响应体读取结束后释放。
- [ ] 视觉辅助在辅助渠道预扣前获取租约，失败时按现有视觉辅助 failure policy 传播。
- [ ] 为每个入口增加最小行为测试，确保本地并发错误不进入上游错误重试/封禁。
- [ ] 逐入口复核薄层边界：旧文件不包含 Redis key、Lua、TTL、内存 map 或重复错误映射。

## 3. Default 前端

- [ ] 使用 `shadcn-ui` 项目规则确认现有 Input/Form 组合，不新增依赖。
- [ ] 在 `types.ts`、`channel-form.ts` 增加字段类型、Zod 整数边界、默认值、旧值解析和创建/更新 payload。
- [ ] 更新 `channel-form-errors.ts` 和高级设置的“已配置”判断。
- [ ] 在 `Routing Strategy` 区域增加 `0..1000` 数字输入与说明，保证移动端双列布局下文本不溢出。
- [ ] 使用临时 `web/default/scripts/add-missing-keys.mjs` 一次写入六种语言，运行 `bun run i18n:sync` 后删除临时脚本。
- [ ] 更新 `channel-form.test.ts`，覆盖旧 `null`、正数、显式 `0` 和非法边界；按需增加组件交互测试。

## 4. 验证命令

### 4.1 后端定向

```bash
go test ./model ./service ./middleware ./controller ./relay -count=1
go test -race ./service -run 'ChannelUserConcurrency' -count=1
go test -race ./controller -run 'ChannelUserConcurrency|ResponsesWebSocket|RelayTask|ClaudeCountTokens' -count=1
go vet ./model ./service ./middleware ./controller ./relay ./types
```

### 4.2 前端

```bash
cd web/default
bun run i18n:sync
bun run typecheck
bun run lint
bun run build
```

### 4.3 全仓

```bash
go test ./... -count=1
go vet ./...
git diff --check
git diff --stat
```

## 5. 发布验证

- [ ] 部署后确认数据库出现 `channels.user_concurrency_limit`，旧渠道均按不限处理。
- [ ] 确认生产 Redis 可用并观察租约 key 的 member 数与 TTL 会随请求创建、续期和释放。
- [ ] 将渠道 `80` 配置为 `4`，读取管理 API 确认缓存和数据库一致。
- [ ] 同一用户并发保持 4 个请求，第 5 个返回 `429 channel_user_concurrency_exceeded`，上游只收到 4 个。
- [ ] 另一用户可同时进入；不同 Token 仍共享用户 4 个名额。
- [ ] 临时阻断 Redis 后，新请求返回 `503 channel_user_concurrency_unavailable`，恢复后租约计数正常。
- [ ] 验证超限不会禁用渠道 `80`，也不会产生消费扣费。

## 6. 风险与回滚点

| 风险 | 检查 | 回滚 |
| --- | --- | --- |
| 更新为 `0` 未落库 | Model/Controller 与前端 round-trip 测试 | 通过显式 map 更新修复；禁止依赖零值 struct 更新。 |
| Redis 原子脚本竞态 | 并发测试和真实 Redis 验证 | 暂时把配置改为 `0`，不删除数据库列。 |
| 心跳 goroutine 泄漏 | 断开、取消、Release 幂等和 race 测试 | 关闭限制后回滚 service/controller 接入。 |
| 重试旧租约未释放 | 记录渠道切换计数测试 | 回滚尝试级接入并配置 `0`。 |
| 特殊协议错误体回归 | 各入口定向测试 | 保留 service，撤销对应入口接入。 |
| 前端无法清除限制 | 显式 `0` payload 测试 | 管理 API 直接更新为 `0`。 |

## 7. 完成标准

- [ ] PRD 每个验收项均有自动测试、命令输出或生产验证证据。
- [ ] 新逻辑主要位于独立 service/controller 文件，原有 Relay 热点仅保留必要接入。
- [ ] 对 `git diff --stat` 和逐文件 diff 做薄层复核：每个旧文件改动均可说明必要性，无无关重构、移动、重命名或格式化。
- [ ] `trellis-check-all` 完整检查通过，修复后重新执行受影响测试。
- [ ] 使用 `trellis-update-spec` 记录渠道并发、错误码、Redis 故障和生命周期契约。
- [ ] 使用 `trellis-release` 生成包含渠道 `80=4` 的上线操作单，完成后再进入提交与归档。
