# ai-fund 共享渠道限制影响调查

## 调查范围与授权

- 用户询问共享限制方案是否需要改 ai-fund，并指定路径 `/root/project/ai-fund`。
- 本轮仅只读检查，没有修改 ai-fund 文件；问题本身不代表已授权增加该仓库实现范围。

## 现有契约

- `worker/src/pool_limits.js:33` 的 `POOL_LIMIT_IDS` 固定为 claude/openai。
- `.trellis/spec/backend/newapi-pool-limits.md` 规定 D1 只保存号池到渠道的映射，NewAPI 为策略、用量和覆盖的唯一权威，BFF 只做鉴权、字段裁剪、金额换算及审计。
- `worker/src/newapi_client.js:1360` 通过 `/api/channel/:id/user-limit-status/:userId` 读取用户权威状态；预算配置与用量同样通过渠道 ID 下的现有端点读取。
- `worker/src/newapi_client.js:1461` 的 `updateNewApiChannelUserLimits` 使用 `PUT /api/channel/`，正文仅 `{id,user_concurrency_limit}`。
- `worker/src/pool_limits.js:665` 的 `updatePoolLimitChannelLimits` 从单渠道读取旧值，再更新该渠道并按渠道写审计。`buildAdminConfig`、并发覆盖列表也使用渠道字段作为基础上限。
- `worker/src/pool_period_limits.js` 的预算保存使用 `expected_channel_id`、策略 revision 和两次映射核对；白名单转换不自动保留未来新增的共享信息。
- `frontend/src/components/PoolLimitAdminModal.vue` 的设置页签按号池的单渠道维护并发，确认和迟到响应隔离也绑定 pool/channel。
- `frontend/src/components/PoolLimitPanel.vue:168` 的标题显示单渠道名称，未描述跨渠道共享；本人卡片消费上游统一状态，而非自己计算并发或金额。

## 当前方案与影响判断

- 用户提出 B 关联父渠道 A，并确认 B 的限制只读，不反向写 A；当前规划采用这一方案满足 ai-fund 不改源码的目标。
- A 继续是原有实际渠道和限制权威，ID、渠道字段、状态及预算管理接口保持原合同。ai-fund 固定映射 A，原有读写仍作用于同一权威对象。
- B 在 new-api 运行时使用 A 的限制及计数，B 的新增正向预算用量归入 A；并发及用量查询 A 能包含关联子渠道，从而 ai-fund 无须另作汇总。
- B 的限制面板显示继承来源且只读，new-api 后端拒绝通过 B 的限制写入修改 A；管理员经 A 原有入口维护共享规则。
- ai-fund 当前若映射 B，应在现有设置里改为 A；仅需配置调整，不需要把 D1 映射改成数组或新对象。
- 子渠道关联与解除在 new-api 管理，ai-fund 不需要知道成员列表；父渠道状态查询继续返回 A 的 ID，避免现有严格身份校验失败。
- 核心限流和金额限制全部由 new-api 执行，ai-fund 不增加第二套计数。实际账单、消费日志和上游配置仍保留物理渠道身份。
- 不承诺 ai-fund 映射任意子渠道时能够管理父渠道；这是避免反向写入并保持不改源码的边界。

## 实施前必须验证

- 旧未分组渠道的请求、状态、预算及写入合同保持可用。
- 使用真实 new-api 父渠道响应验证现有 BFF 字段裁剪、金额换算、channel_id 校验和版本冲突；兼容性证据保存在本任务内，不为验证擅自修改 ai-fund 源码。
- ai-fund 只展示共享并发/金额一次，不把多个成员的共享结果再次求和。
- A 保持顶层父渠道，不允许在管理对象未变化的情况下静默改为另一个父渠道；B 的继承写接口返回明确拒绝，不自动重定向写 A。
