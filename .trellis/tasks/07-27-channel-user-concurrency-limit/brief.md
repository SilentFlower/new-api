# 任务 Brief — 渠道单用户并发限制

## 目标

- 为渠道增加按 `channel_id + user_id` 统计的单用户最大并发配置，防止单个用户通过多个 Token 占满同一上游渠道。
- 功能上线后将生产渠道 `80` 的限制配置为 `4`。

## 实施范围

- 在 `model.Channel` 增加可空整数配置；`nil` 和 `0` 表示不限，合法范围为 `0..1000`，并接入迁移、缓存、管理 API 校验和权限字段分类。
- 新增 Redis/内存双模式租约服务。Redis 使用 Sorted Set、UUID 租约、Lua 原子获取/续租/释放、120 秒 TTL 和 30 秒心跳；未配置 Redis 时使用进程内实现。
- Redis 已配置但获取失败时返回 `503 channel_user_concurrency_unavailable`；达到上限时返回 `429 channel_user_concurrency_exceeded`。两者均不重试、不自动禁用渠道、不调用上游、不扣费。
- 将限制接入主 Relay、流式响应、Realtime、Responses WebSocket、异步任务实际 HTTP 调用、Midjourney、Claude `count_tokens` 和视觉辅助。
- WebSocket 按整个连接占用一个名额，同一连接内多轮请求不重复计数；空闲但未关闭的连接仍占用名额。
- 重试换渠前释放旧渠道租约，再按新渠道和当前用户获取新租约。
- 保持薄层设计：并发核心逻辑集中在新 service/controller 领域文件，原有 Relay、WebSocket、任务和特殊入口只保留窄生命周期调用。
- 在 `web/default` 渠道高级设置中增加数值输入，保留显式 `0`，补齐 `en`、`zh`、`fr`、`ja`、`ru`、`vi` 六种翻译和表单测试。
- 完成后生成上线操作单，将渠道 `80` 配置为 `4` 并验证第 5 个同用户并发请求被拒绝。

## 不做范围

- 不实现等待队列、请求排队超时或优先级队列。
- 不实现用户全局、模型级、Token 级或渠道总并发限制。
- 不限制异步任务提交后的后台运行时长或在途任务总数。
- 不新增并发趋势图、实时并发管理页，也不改动管理端渠道测试行为。
- `web/classic` 不新增配置控件；未携带新字段的旧表单更新必须保留已有配置。

## 关键约束

- 配置字段使用 `*int`，确保历史 `NULL` 兼容且显式 `0` 可以持久化。
- 初选和重试统一通过 `middleware.SetupContextForSelectedChannel` 刷新限制。
- 限制必须在实际上游调用前、计费预扣前获取；本地拒绝不得进入 `processChannelError`。
- 错误码不能使用 `channel:` 前缀，避免被识别为渠道错误后触发换渠或自动禁用。
- 续租失败或租约丢失时取消仍在运行的上游请求或关闭 WebSocket；释放失败只告警并依赖 TTL 回收，不改写已完成响应。
- 所有导出类型和方法使用中文 GoDoc，并按项目规范包含 `@param`、`@return`。
- 数据库实现必须兼容 SQLite、MySQL 和 PostgreSQL；JSON 操作继续使用 `common` 封装。
- 不为复用本功能而重构上游主链路；旧文件不得包含 Redis key、Lua、TTL、内存计数或重复错误转换，也不得产生无关格式化、移动或重命名。
- 每个原有文件的修改必须能说明唯一必要性；删除新领域模块并撤销窄调用后应可完整回滚功能。
- 保留现有 `docker-compose.yml` 和其他任务目录中的用户改动。

## 验收重点

- 新建、编辑、正数改 `0`、历史空值、非法整数边界均正确处理，保存后数据库和缓存一致。
- 同一渠道同一用户达到 `N` 后，第 `N+1` 个请求返回 `429`，上游调用数和扣费均不增加；多个 Token 共享限制，不同用户和渠道互不影响。
- 流式、取消、断开、错误、panic、换渠重试和租约过期路径均不泄漏或串用名额。
- Redis 模式跨实例原子限制；Redis 未配置时单进程准确；Redis 已配置但不可用时失败关闭为 `503`。
- WebSocket 整个连接只计一次；异步任务仅在每次实际上游 HTTP 调用期间计数。
- OpenAI、Claude 及特殊 Relay 的错误格式保留稳定机器可读错误码，且超限不触发重试或渠道禁用。
- 薄层复核通过：核心逻辑位于独立领域文件，热点旧文件只有生命周期接入，`git diff` 无无关重构或格式化。
- Go 定向测试、race 测试、`go test ./...`、`go vet ./...`、前端 i18n/typecheck/lint/build 和 `git diff --check` 通过。
- 生产渠道 `80` 配置为 `4` 后，同一用户保持 4 个请求时第 5 个请求被拒绝，另一用户仍可进入。

## 下一步

- 用户确认本 brief 及 `prd.md`、`design.md`、`implement.md` 后，运行 `task.py start` 将任务切换为 `in_progress`。
- 进入 Phase 2 后通过 `trellis-route(target=implement)` 执行实现，再通过统一检查流程验证并修复。
