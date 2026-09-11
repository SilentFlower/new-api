# 两仓联动证据与实施注意点

## 检查范围与基线

- 本地代码与规范读取，未请求线上 NewAPI、D1、Redis 或供应商接口。
- new-api 当前分支 `build-bak`，任务创建时 HEAD 为 `fa4263ad885dc6172e7dffdd43b71775ae6a01b9`；初始无业务代码差异，仅有 `.codex/hooks/__pycache__/`。
- ai-fund 当前分支 `master`，本轮读取 HEAD 为 `ea54b12bfe341c1b39641497e6ede2d71ba5cccc`；初始仅有 `.claude/hooks/__pycache__/`。
- 原会话 `01a08df7-9345-7fc0-afbf-8a5913bc6b18` 的工作目录为 `~/project/sub2api`；其代码判断不作为本任务事实依据。

## 现有限额与结算

- `model/channel.go` 保存 `UserConcurrencyLimit`、`UserDailyQuotaLimit`、`UserWeeklyQuotaLimit`，无新池子日周预算字段。
- `model/channel_user_limit_override.go` 是唯一 `(channel_id,user_id)` 的个人提额记录，三个可空上限、统一到期时间。当前 `service/channel_user_limit_override.go` 只允许提额并用 `max(base,override)` 读取；时间规则加入后必须明确区分旧行为和逐项优先级，不能把读取错误回落默认值无条件用于降额规则。
- `service/channel_user_daily_quota.go` 和 `service/channel_user_weekly_quota.go` 使用 Redis Hash 或带锁内存，以服务器自然日和周一零点自然周为周期。Redis 已启用但不可用时不切到内存。
- `service/channel_user_quota_usage.go` 是日周正向累计协调入口。调用方包括 `service/text_quota.go`、`service/quota.go`、`service/task_billing.go`、`service/tool_billing.go`、`service/violation_fee.go`、`relay/mjproxy_handler.go`。异步正差额使用实际结算时刻；负差额不减少已用额度。
- 现有每日／每周管理写 API 设置的是指定用户当前周期已用值；因此不能把这些可人为修改的字段之和作为新增池子真实总累计。

## 主请求与额外上游调用

- `controller/relay.go` 中顺序为：克隆请求、选渠、请求准备、并发租约、计费准备、调用上游。`prepareMainRelayBilling` 的限额错误会直接终止循环，不能仅去掉 `skipRetry` 实现降级。
- `relay/vision_assist.go:57` 的 `PrepareRequestForSelectedChannel` 不只是纯模型映射，还可能通过 `service.ApplyVisionAssist` 调用辅助上游。限额路由需在这个外部副作用之前进行预检，并在正式预扣前复检；若已经产生任何上游副作用，不允许切换主模型后重复执行整个请求。
- `controller/relay_attempt.go:65` 的 `cloneRelayRequest` 对部分类型返回原对象。新增降级必须给 Responses 请求增加真实深拷贝；不能把第一次映射后的结构当作原始输入重放。
- `relay/common/billing_model.go` 与 `.trellis/spec/backend/relay-billing-model.md` 定义原始模型和冻结价格语义。新增路由模型应独立承载，最终价格仍通过既有冻结接口统一供预扣、结算和日志使用。
- 非流式／SSE 保活可能在模型输出前提交响应头；有响应提交不等于已经有上游副作用，两者需分别检查，不能借保活已启动中途变更响应协议。

## 协议边界

- `relay/channel/openai/adaptor.go:56` 可将 Claude 请求转换为 OpenAI Chat；`relay/channel/claude/adaptor.go:95` 支持反方向 Chat 转换，但 `:115` 的 Responses 转换尚未实现。
- `relay/channel/newapi/adaptor.go` 对 Chat、Claude、Responses 等 DTO 有各自处理，不能仅按渠道类型或厂商推断完整能力。
- `middleware/distributor.go` 处理 Token 模型权限、指定渠道、分组、模型 Ability 与亲和性。降级指定目标必须重新应用必要检查，不能仅调用 `GetChannelById` 后直接发送。
- `controller/responses_websocket.go` 和 `relay/responses_websocket.go` 有独立连接流程；`relay/channel/adapter.go` 的 `TaskAdaptor` 也是独立契约。用户已明确首版这些入口只限额，不自动降级。

## ai-fund 边界

- `worker/src/settings.js:43` 的默认映射为 Claude→80、OpenAI→52；线上实际映射未查询。
- `worker/src/newapi_client.js:1062` 的统一状态归一化与 `worker/src/pool_limits.js:351` 的公开状态转换都执行字段白名单，新字段需要贯穿两层。
- `resolvePoolChannelForWrite` 每次写入重新读取 D1，验证 `expected_channel_id`。D1 读取失败返回 503；映射冲突返回 409；不向错误渠道发写请求。
- 老 `updateNewApiChannelUserLimits` 出站只含 `id` 和三个个人默认限制。新策略采用独立端点，保持这个旧出站合同。
- `frontend/src/components/PoolLimitAdminModal.vue` 被 `/pools` 和 `/admin` 复用，`PoolLimitPanel.vue` 展示本人日周及并发；新业务拆出独立子组件接入这些现有壳，不复制整个管理工作区。

## 实际验证命令与规范取舍

- new-api 前端为 React 19、Rsbuild、Bun，`web/package.json` 的实际命令是 `typecheck`、`lint`（oxlint）、`build`、`format:check`、`i18n:sync`，不是旧规范中的 Vite／Semi／ESLint 命令。
- 现有渠道组件测试使用 `bun:test`、`happy-dom` 与真实 React 渲染，可用 `bun test` 定向运行；不存在 `bun run test` 脚本。
- ai-fund Worker 使用 `node:test`，运行 `node --test src/*.test.js`；Worker package.json 没有 test 脚本。前端存在 `build`，可用 `bun run build`。
- `model/main.go` 存在普通迁移与另一组模型注册路径，新增表必须覆盖两处。数据库规范中的 GORM v1 行锁示例已过时，以 AGENTS.md 的 `lockForUpdate(tx)` 为准。
- 不为文档规划运行业务测试；实现阶段必须验证本文件中标出的克隆、上游副作用、价格冻结、协议能力与 BFF 回读假设。

## 适用规范

- new-api：`AGENTS.md`、`web/AGENTS.md`，`.trellis/spec/guides/build-upstream-friendly-customization.md`，后端 `channel-user-daily-quota.md`、`channel-user-weekly-quota-and-overrides.md`、`channel-user-concurrency.md`、`relay-billing-model.md`、`database-guidelines.md`、`api-contracts.md`，前端 `base-ui-composition.md`、`i18n-merge-guidelines.md`。
- ai-fund（以 `/root/project/ai-fund` 为根）：`AGENTS.md`，`.trellis/spec/backend/newapi-pool-limits.md`、`quality-guidelines.md`、`error-handling.md`、`database-guidelines.md`，`.trellis/spec/frontend/component-guidelines.md`、`state-management.md`、`quality-guidelines.md`、`hook-guidelines.md`。
