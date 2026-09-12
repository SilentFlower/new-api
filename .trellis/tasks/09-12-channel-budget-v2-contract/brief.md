# Brief — 预算 v2 契约：配置/覆盖/用量接口、迁移废弃旧列、new-api 前端预算表

## Goal

- 对外暴露预算列表契约（`schema_version=2`），管理员在一张预算表里配置渠道级、时段、按模型的日/周/整段额度与超限动作；个人覆盖与用量调整按行操作；启动迁移把 v1 策略、渠道列日/周与两张覆盖表并入 v2 并废弃旧列、旧接口。

## Scope

- 契约：`ChannelPeriodPolicyConfig{schema_version: 2, default_on_exceed, schedules[], budgets[]}` 替换 v1 结构；`PUT/GET /period-policy` 与 preview 的 body 升 v2（URL 不变），严格键集合、revision CAS 沿用；`GET /period-policy/targets` 含本渠道并标记 `self`。
- 归一化与校验：行 ≤ 128、时段 ≤ 64（含停用）；`limit` 为 `0..MaxPeriodQuota`；`models` 去空白排序去重；同 `(scope, window, models)` 键内同类型时段相交拒绝；`occurrence` 必须引用存在时段；行级同渠道降级仅限 `models` 非空且目标不在本行内；策略级降级禁止指向本渠道。
- 内核：`buildChannelBudgetPlan` 改为直接消费 v2；删除 `resolveChannelPeriodSources` v1 投影；`ChannelPeriodMetric` 新增 `budget_id`/`budget_name`/`models`/`schedule_id`/`schedule_name`，`period` 改 `occurrence`；`ChannelLimitFallbackInfo` 新增 `budget_id`/`models`。
- 统一个人覆盖：新表 `channel_user_budget_overrides(channel_id, user_id, budget_id, limit, expires_at, updated_by, created_at, updated_at)`，唯一 `(channel_id, user_id, budget_id)`；`PUT/DELETE /api/channel/:id/budgets/:budget_id/user-overrides/:user_id`、`GET /api/channel/:id/budget-user-overrides`；只对 `scope=user` 行有效，替换语义且必须高于该行基础额度。
- 统一用量：`GET /api/channel/:id/budgets/:budget_id/usage?scope=user|pool&page&page_size` 列当前窗口用户已用，按用量降序、同值按用户 ID 升序；`PUT /api/channel/:id/budgets/:budget_id/usage {scope, user_id, used_quota}` 直接设置该行当前窗口计数，不改 revision，审计 before/after，审计前值读取失败则停止写入。
- 迁移与废弃：启动时逐渠道把 v1 策略 + 渠道列日/周（>0）+ 两张覆盖表转为 v2（派生 id 与阶段一一致，Redis key 沿用不归零），v2 已存在则跳过，单渠道失败记日志继续；渠道表日/周列与旧覆盖表日/周字段 `json:"-"` 不再读写，渠道创建/更新不再校验；下线 `user-daily-quota*`、`user-weekly-quota*`、`period-rules/*` 接口，`user-limit-overrides` 只接受并发。
- 阶段一推迟项（R7）：删除 `relay/channel_user_{daily,weekly}_quota.go` 独立检查、降级预检直接调用、`CheckSelectedChannelPeriodLimits` 的 revision=0 早返回与上下文键回写、`RelayInfo` 日/周字段与 `vision_assist` 上下文键复制。
- new-api 前端：周期策略面板重写为预算表（名称、范围、周期、时段、模型、上限、已用/剩余进度、超限动作、启用）+ 时段列表 + 表下方默认动作；行编辑走抽屉，关闭与 Esc 保留草稿；池子行显示汇总，个人行标注最高用量用户并使用其有效提额；用量列表/调整与个人特批按行操作；用户限制对话框日/周页签改按行；渠道抽屉删除日/周字段；zod 升 v2；七个语言文件同步。
- 产出 v2 fixture 至 `research/channel-period-contract.v2.json` 供 ai-fund 子任务。

## Non-Goals

- ai-fund 改动（子任务 `09-12-channel-budget-ai-fund`）。
- 废弃列与旧表的物理删除。
- 并发限制进入预算表；模型通配；monthly 窗口；多跳降级；个人覆盖降额。

## Key Decisions

- 旧列与旧覆盖表迁移并废弃（用户已定）：一次性启动迁移，列与表物理保留一版；回滚 = 回退代码 + 恢复三表备份。
- 派生 id 与计数身份沿用阶段一（`legacy-*`、`rule-<id>-*`），迁移后已用值不归零。
- `on_exceed.mode=inherit` 让大多数行沿用策略级默认动作，只有个别模型行配置同渠道换模型。
- `period-policy` URL 不变，只升 body；错误码规则沿用阶段一（渠道级个人日/周行保留旧错误码）。
- 阶段一保留的旧检查在本任务删除，所有请求走统一判定；迁移未执行或失败、尚无策略记录时，revision=0 视图仍从旧列派生约束。
- 撤销提额保留零额度非生效记录，防止重启重新导入；未知/缺失版本或空正文的已存储策略拒绝读取与迁移覆盖；模型等计数身份变化按修改时间重置统计起点。

## Key Context

- 父任务 `design.md` §6-§8（v2 契约、覆盖与用量接口、迁移）、§4-§5（判定与降级）；本任务 `design.md` 文件计划。
- 阶段一内核：`service/channel_budget_plan.go`、`channel_budget_usage.go`、`channel_budget_guard.go`、`channel_limit_fallback_select.go`；规范 `.trellis/spec/backend/channel-period-budget-fallback.md` §3 预算行内核契约。
- 旧接口与控制器：`router/channel-router.go:46-61`、`controller/channel_user_limits.go`、`controller/channel_user_daily_quota.go`、`channel_user_weekly_quota.go`、`controller/channel_period_policy.go`；渠道列校验 `controller/channel.go:475-478,975-976,1134-1137`。
- 迁移模式：`model/main.go:253,330` 的 `migrateDB` / `migrateDBFast`；三库测试模式 `model/channel_period_policy_database_test.go`。
- 前端：`web/src/features/channels/period-types.ts`、`period-api.ts`、`components/dialogs/channel-period-policy-panel.tsx`、`channel-user-limits-dialog.tsx`、`channel-period-override-editor.tsx`、`drawers/sections/channel-user-{daily,weekly}-quota-limit-field.tsx`、`types.ts`、`constants.ts`。
- 约束：JSON 走 `common.*`；三库只用 GORM，`lockForUpdate`；testify + miniredis；DTO 零值规则；build 分支最薄接入（上游文件 `controller/channel.go`、`relay/common/relay_info.go`、`relay/vision_assist.go` 只做删除/一行改动）；relaykit 独立可构建。

## Risks / Deferred

- 单向迁移：旧代码读到 v2 策略会判定无效并返回 503；上线前必须备份 `channel_period_policies`、`channel_user_limit_overrides`、`channel_user_period_overrides` 并演练回滚（AC10）。
- 与 ai-fund 必须同版本部署：本任务删除旧接口后，旧 ai-fund 的用量/覆盖功能立即失效；new-api 先上、ai-fund 紧随。
- 删除 `relay` 旧检查后，降级目标侧的个人日/周拦截完全依赖统一判定，需全链路测试覆盖同渠道换模型场景（AC11）。
- MySQL/PostgreSQL 迁移测试依赖专用 DSN，本地缺失时标记部分验证。

## Acceptance

- AC2：整体池子日限 100、`gpt-6-astra` 池子日限 20 → 该模型累计 20 后 429/降级，其他模型可用至 100，整体耗尽后全部 429；周限、个人范围同理。
- AC4：模型行 500、整体 600、同渠道降级 `gpt-6-mini` → 改用目标并按其计费，日志含触发行；整体到 600 走策略级；目标自身耗尽 429 不二跳。
- AC5：迁移后每渠道策略为 v2 且与 v1 + 渠道列语义等价；重复启动 revision 不变；未知/缺字段 400；冲突 409。
- AC6：按行覆盖/用量只影响该行该用户；revision 不变；审计 before/after；越界/不存在行/池子行个人覆盖 400。
- AC8：预算表增删改回读一致；渠道抽屉无日/周字段；旧接口 404；typecheck/lint/前端测试/i18n:sync 无 diff。
- AC10：回滚演练记录在 `research/rollback-drill.md`。
- AC11：旧检查删除后降级目标侧个人日/周仍被统一判定拦截；`relay` 包不再引用日/周上下文键。

## Next Step

- 当前任务为 in_progress。本轮 CHK-004 至 CHK-007、FBK-002/003 均已修复，inline Full Check-All 重检通过；Go 整包回归、三库迁移、旧版 SQLite 回滚及前端 39 项渠道测试通过，详见 `implement.md` 与回滚记录。下一步进入规范同步，再准备提交计划；ai-fund 子任务仍未启动，联动发布尚待其完成。
