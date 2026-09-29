# ai-fund 号池限制 / 预算 v2 管理界面 UX 代码审计（2026-09-13）

仓库 `/root/project/ai-fund`，路径前缀 `frontend/src/components/`。

## 1. 入口与页面结构

两处挂载，同一组件两种壳：
- `frontend/src/views/Pools.vue:106-112` 号池卡片右上角齿轮按钮（`.pool-gear`，仅 `isAdmin`）→ `openLimitAdmin(section.id)`（`:583`）→ `:256-265` 以 AppModal 弹窗（maxWidth 1040px）打开，带 `initialPoolId`。
- `frontend/src/views/Admin.vue:28-37` 管理页 tab「号池限制」（`:916`）→ `embedded` 模式，`shellComponent` 变为 `<section>`（`PoolLimitAdminModal.vue:449-458`）。

弹窗内部结构（`PoolLimitAdminModal.vue`）：

| 区域 | 行范围 | 说明 |
|---|---|---|
| 常驻头部（sticky） | 14-31 | 主 Tab (`AppTabs`) + 号池切换（Claude/OpenAI，自定义 `PoolSelector` 也是 AppTabs）+ 当前渠道名/状态 + 三条 AppAlert |
| Tab `period` 预算策略（默认页，`:482`） | 39-46 | 整页交给 `PoolPeriodPolicyEditor`，`v-show` 保留草稿 |
| Tab `mapping` 渠道映射 | 48-83 | 两个 DropSelect，保存需确认 |
| Tab `limits` 并发限制 | 85-108 | 仅一个字段「个人并发上限」 |
| Tab `concurrency` 当前并发 | 110-172 | 只读表格 + 分页 |
| Tab `overrides` 个人覆盖 | 174-331 | 用户搜索 → 选中用户编辑区（并发状态 + `PoolPeriodOverrideEditor` + 并发覆盖表单）→ 「预算个人提额」表 → 「并发个人覆盖」表 |
| 5 个 AppConfirm | 341-402 | 映射/限制/覆盖/切池丢弃/撤销覆盖 |

Tab 顺序：`mapping, limits, period, concurrency, overrides`（`:461-467`），但默认落在第 3 个 `period`。

`PoolPeriodPolicyEditor.vue`（`:1-45`）：标题+重新加载 → 两段说明 → 「持续累计模型用量」开关 → 默认超限动作 → 预算表（6 列宽表，min-width 1050px）→ 小字说明 → 可复用时段（article 卡片）→ 说明 → 「预览生效结果」→ 预览区（含真正的保存按钮）→ 底部 `PoolBudgetUsageEditor`（点「查看用量」后出现）。

## 2. 数据模型（前端视角）

- policy view（`getAdminPoolPeriodPolicy`）：`{ revision, timezone, now, channel_id, config, quota_per_unit }`；`config = { schema_version, model_usage_tracking_enabled?, model_usage_tracking?, default_on_exceed, schedules[], budgets[] }`。
- budget row（`worker/src/newapi_period_policy.js:11-13` + `pool_period_limits.js:73`）：`id, name, enabled, scope, window, schedule_id, models[], on_exceed, created_at, limit_display, counter_source?`。新建行 `id=''`（`PolicyEditor.vue:109`）。
- schedule（`newapi_period_policy.js:6-9`）：`id, name, enabled, kind, start_local, end_local, start_at, end_at, start_weekday, end_weekday, start_time, end_time, created_at`；新建 `id='new-<uuid>'`（`:104`）。
- on_exceed / default_on_exceed：`{ mode: 'inherit'|'reject'|'fallback', channel_id, model }`；`channel_id=0` 表示本渠道（`PoolBudgetActionEditor.vue:4`）。
- usage（`getAdminPoolBudgetUsage`）：`{ channel_id, budget_id, scope, page, page_size, total, items[{user_id, username, display_name, used_quota_display}], used_quota_display, window_start, window_end, tracking_since, storage_mode }`。
- budget override 列表项：`{ budget_id, budget_name, user{}, base_limit_display, limit_display, expires_at }`（`pool_period_limits.js:189`）。
- 用户状态 metrics（`PoolPeriodMetrics.vue`/`PoolLimitPanel.vue`）：`budget_id, budget_name, scope, period, models, limit_display, used_display, remaining_display, reset_at, tracking_since, coverage, source{kind, schedule_name, expires_at}, enforced, base_limit`。
- 金额换算：worker 用 `quota_per_unit`（500000）把 quota 除成 `*_display`，前端当 USD 显示。格式不统一：`PolicyEditor` 裸数字，`PoolPeriodMetrics.vue:31` 最多 6 位小数，`PoolLimitPanel.vue:246` 固定 2 位小数。

## 3. 关键操作流程

- (a) 新增预算行：「新增预算」（`PolicyEditor.vue:15`，上限 128）→ 表格末尾空行，填名称/范围/窗口/时段/模型/金额/超限动作 →「预览生效结果」→ 预览区「确认保存此策略」。新行「查看用量」禁用直到保存（`:29`）。
- (b) 新增时段：「新增时段」（`:35`，上限 64）→ article 卡片 → 可立即在预算行「关联时段」下拉选到 `new-*` id。已保存时段 `kind` 锁定（`:38`）。
- (c) 编辑上限：直接改表格 `limit_display` 数字框（`:26`），`0`=不限制；并发上限在「并发限制」tab（`AdminModal.vue:87-97`）→ 确认框。
- (d) 调整用量：保存后的行 →「查看用量」→ 页面底部出现 `PoolBudgetUsageEditor`（`PolicyEditor.vue:54`，仅 `!dirty && !stale`）→ 提交 → AppConfirm → 写后重读。
- (e) 个人提额：「个人覆盖」tab → 搜索用户 → 点击结果 → 编辑区内嵌 `PoolPeriodOverrideEditor`（`AdminModal.vue:244-250`）：选预算行（仅已保存、scope=user、limit>0，`OverrideEditor.vue:44`）→ 特批金额（须 > 基础额）→ 到期 → 「保存预算提额」（无确认）/「撤销所选特批」（无确认）。
- (f) 默认超限动作：策略页顶部 `PoolBudgetActionEditor`（`PolicyEditor.vue:13`）。
- (g) 保存与 CAS：`submit(previewOnly)`（`PolicyEditor.vue:114-140`）带 `expected_channel_id + expected_revision`；`previewSnapshot===snapshot` 才显示保存按钮（`:47`）；任何非预览失败或 409 → `stale=true` 锁死 fieldset，必须「重新加载」（`:137`）。
- 终端用户（`PoolLimitPanel.vue`，嵌在 `Pools.vue:149`）：「我的额度」组 + 「号池额度」组，每卡数值/上限、进度条（60%/85% 变色）、剩余、相对重置时间；顶部降级提示条；未 enforced 的预算不显示。

## 4. UX 问题清单

结构性
1. 策略编辑页承载过重（`PolicyEditor.vue:1-56`）：一个 `<form>` 里同时放全局开关、默认动作、预算宽表、时段卡片、预览区、用量编辑器；弹窗模式叠加 `max-height: min(74vh, 40rem)` 滚动（`AdminModal.vue:1252`）。
2. 预算表是「表格里的表单」（`PolicyEditor.vue:18-31`）：每格 1-3 个控件，`min-width:1050px`，1040px 弹窗内必然横向滚动；fallback 时高度膨胀成 3 个 select。
3. 保存动线反直觉：主按钮「预览生效结果」（`:44`），「确认保存此策略」藏在预览区最底部（`:50`），草稿一改预览即消失；无 sticky 保存栏。
4. 「个人覆盖」tab 混合三种概念（`AdminModal.vue:174-331`）：并发覆盖 + 预算提额 + 两张分页表共用同一 `page`（`:1011-1014`）；提额编辑器嵌在并发编辑区中间（`:244-250`），「保存个人覆盖」按钮（`:277`）只保存并发。
5. 用量入口埋在表格里（`PolicyEditor.vue:29`）：内容出现在整页最底部（`:54`），有草稿时禁用而无说明；同一时间只能看一行；无「所有预算当前用量总览」。
6. Tab 顺序与默认页不一致（`AdminModal.vue:461-467, 482`）；`mapping` tab 下隐藏号池切换（`:16`），切 tab 时头部跳动。
7. 重复的用户状态展示：`PoolPeriodMetrics` 与 `PoolLimitPanel` 不同实现/不同格式。
8. 无 revision/时区/生效状态常驻展示：只在预览时显示「版本」（`:48`）；行是否「当前生效/超限」在编辑表中不可见。

细节
9. 无行内校验反馈：依赖原生校验（`PolicyEditor.vue:19,26`）；后端错误只在顶部 AppAlert。
10. 危险操作缺确认：「停用预算/移除预算」「停用时段/移除时段」直接 `splice`（`:29, :36`）；「撤销所选特批」无确认（`OverrideEditor.vue:15`）。
11. 「停用」语义误导：按钮叫「停用预算」，行为是从列表消失（`:29`）；界面无处列出停用行，无法重新启用。
12. 说明文字过密、分散：至少 6 段灰字（`:7-8, 12, 32, 41, 43`），内容偏后端口径。
13. 时段卡片不显示被哪些预算引用；删除后引用悬空无提示。
14. 模型输入是逗号分隔纯文本（`:24`），有 `MultiSelect.vue` 未使用。
15. 加载态不统一：`AppSkeleton`/`AppEmptyState` 均未使用，空状态是 `<td colspan>` 文本。
16. 成功反馈不一致：`AdminModal` 4s 自动消隐，`PolicyEditor`/`OverrideEditor`/`UsageEditor` 常驻 AppAlert。
17. 金额格式三套；输入框无 `$` 前缀；并发覆盖占位「留空不覆盖」但校验要求必填（`:576-578`）。
18. 默认超限动作放在开关下面（`:11-13`）无分组标题；默认动作无法选本渠道换模型，无解释（`ActionEditor.vue:4`）。
19. 头部 sticky 遮挡：三条 AppAlert 都在 sticky 头部（`AdminModal.vue:25-29`）。
20. 用户搜索需回车/点按钮（`:186-191`），输入变化即清空结果（`:1191`），无「无匹配」空态。
21. OverrideEditor 预算下拉 option 内容过长（`OverrideEditor.vue:9`）。
22. PoolLimitPanel：`.is-custom` 占满整行（`:391`）；不显示未 enforced 但即将生效的预算（`:126`）；`detail` 行模型名可能溢出（`:133, :430`）。
23. 移动端：`.budget-table` 仅横向滚动；弹窗 `max-height 78vh`（`:1643`）。

## 5. 现有样式体系

- Tailwind 3.4 + 自定义 CSS 变量主题（`main.css:13-80`，暗/亮双套）。组件多用 scoped CSS。
- 无第三方组件库。共享组件 `frontend/src/components/ui/`：`AppModal`、`AppConfirm`、`AppTabs`、`AppButton`、`AppAlert`、`AppPagination`、`AppEmptyState`、`AppErrorState`、`AppSkeleton`；根级 `DropSelect`、`MultiSelect`、`DatePicker`。
- 全局类：`.input-field`（`main.css:361`）、`.glass-card`、`.loading-pulse`、`.admin-panel`、`.admin-button`。没有 Drawer、Table、Switch、Field、Tooltip、Badge 组件。

## 6. 测试约束

`frontend/tests/pool-period-policy.spec.js` 与 `pool-budget-management.spec.js` 锁定：
- 按钮文案：`预算策略`、`个人覆盖`、`重新加载`、`确认保存此策略`、`新增预算`、`新增时段`、`查看用量`、`确认调整`、`撤销所选特批`。
- label 包含匹配：`预算金额`、`特批金额`、`用户 ID`、`调整后的已用金额`；模型输入 placeholder 含「多个用逗号」。
- DOM：`.budget-table tbody tr`（`[4]` 是模型预算行；行内 `select` `[1]`=window、`[2]`=schedule、最后=fallback 目标模型）；`article.period-rule`；`[aria-label="策略保存预览"]`；`[role="switch"]`；`form` submit = 预览；`fieldset[disabled]` = stale；`h4` 文本判断 tab。
- 行为：先 preview=true，改草稿后保存按钮消失；409 → fieldset 禁用；切 tab 保留草稿（`v-show`）；池子用量不显示「用户 ID」；提额金额 ≤ 基础额不发请求；旧响应不污染新目标；文本 `全体用户合计：$12.5`、`预算提额已更新`。
