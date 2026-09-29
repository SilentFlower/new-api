# new-api 渠道预算 v2 / 周期策略 UI/UX 代码审计（2026-09-13）

路径前缀均为 `web/src/features/channels/`，简写：`panel` = `components/dialogs/channel-period-policy-panel.tsx`，`limits` = `components/dialogs/channel-user-limits-dialog.tsx`，`usage` = `channel-budget-usage.tsx`，`progress` = `channel-budget-progress.tsx`，`metrics` = `channel-period-metrics.tsx`，`override` = `channel-period-override-editor.tsx`，`amount` = `channel-period-amount.tsx`。

## 1. 入口与页面结构

- 唯一入口：渠道表行操作下拉菜单「用户限制状态」（`components/data-table-row-actions.tsx:147-150, 345-350`）→ `setOpen('user-limits')` → `components/channels-dialogs.tsx:74-78` 渲染 `ChannelUserLimitsDialog`。没有从渠道编辑抽屉进入的路径。
- `ChannelUserLimitsDialog`（`limits:530-542`）：项目封装 `Dialog`（`@/components/dialog`），`sm:max-w-6xl`、固定 `contentHeight='72vh'`，标题「用户限制状态」。内部 `Tabs` 四页签（`limits:548-559`），顺序为 周期策略 → 预算用量 → 当前并发 → 个人覆盖，但默认页签是第二个 `budget-usage`（`limits:261, 285`）。各页签内容按 `activeTab` 条件挂载（`limits:562, 571`）。
  - 周期策略页签 → `ChannelPeriodPolicyPanel`（`panel:89-146`）→ `ChannelPeriodPolicyForm`（`panel:149-1058`）：顶部说明三段 + 错误行（276-297）→「持续累计模型用量」开关（299-321）→ 时段区，每个时段一张展开卡片（323-492）→ 预算表 10 列（493-618）→ 预算编辑器 `Sheet`（620-878，右侧 `w-full sm:max-w-2xl`，无关闭 X）→「新增预算」按钮（879-901）→ 默认超限动作区（903-999）→「预览生效限制」提交按钮（1000-1002）→ 行内用量面板（1004-1012）→ 预览结果区 + 「确认并保存」（1013-1054）。
  - 预算用量页签 → `ChannelBudgetUsageTab`（`usage:266-331`）：`NativeSelect` 选行后渲染 `ChannelBudgetUsagePanel`（`usage:42-263`）。
  - 当前并发页签（`limits:579-667`）：5s 轮询表。
  - 个人覆盖页签（`limits:668-864`）：搜索表单 → 搜索结果 → 并发覆盖表 → 预算提额表，三段各带刷新。
- 个人提额弹窗：`AlertDialog`（`limits:868-978`，`sm:max-w-2xl`，`max-h-[90dvh]`）叠在 Dialog 之上，内含并发 `OverrideEditor`（983-1038）+ `ChannelPeriodOverrideEditor`（`override:44-219`，内含 `ChannelPeriodMetrics` 指标卡）。
- 嵌套层级：Dialog → Tab → Sheet（预算编辑）；Dialog → AlertDialog（提额）。

## 2. 数据模型（前端视角）

`period-types.ts`（zod schema 即协议）：
- `ChannelPeriodConfig`（61-74）：`schema_version: 2`、`model_usage_tracking_enabled?`、`default_on_exceed`、`schedules[]≤64`、`budgets[]≤128`。
- 预算行（47-59）：`id`（新行 `new-` 前缀，`panel:81-86`）、`name`(1-80)、`enabled`、`scope: user|pool`、`window: daily|weekly|occurrence`、`schedule_id`、`models[]≤64`、`limit`（quota 整数，0=不限，上限 `MAX_PERIOD_QUOTA`）、`on_exceed: {mode: inherit|reject|fallback, channel_id, model}`（`channel_id=0` 表示同渠道换模型）、`counter_source?`。
- 时段（31-45）：`kind: date_range|weekly`；date_range 用 `start_local/end_local`，weekly 用 `start_weekday/end_weekday + start_time/end_time`。保存后不能改 kind、不能删除。
- 用量 `ChannelBudgetUsageView`（152-170）：窗口起止、`tracking_since`、`storage_mode`、`used_quota` + 分页 `items[user_id, used_quota]`。
- 指标 `ChannelPeriodMetric`（116-132）：`limit/used/remaining/reset_at/source/enforced/base_limit/override_limit/coverage`。
- 金额：内部 quota 整数；展示 `formatQuota`；输入 `ChannelPeriodAmount`（`amount:27-66`）：`type=number step=any`，标签 `Limit (USD|CNY|Tokens)`，`quotaUnitsToDollars` ↔ `parseQuotaFromDollars`，空=NaN 或 null，负数→ -1 哨兵。
- CAS：`getChannelPeriodPolicy` 返回 `revision`；preview/save 都携带 `expected_revision`（`period-api.ts:87-116`）；409 → 「策略已变更，请重新加载」并锁定（`period-api.ts:45-58`, `panel:219-228`）。

## 3. 关键操作流程

- (a) 新增预算行：「Add budget」（`panel:879-901`）→ 追加默认行（pool/daily/inherit/limit 0）并直接打开 Sheet 编辑器。
- (b) 新增时段：「Add schedule」（468-491）→ 追加空的 date_range 卡片，内联填写；无独立编辑器。
- (c) 编辑上限/模型：表行「Edit」（576-586）→ Sheet；模型是逗号/换行分隔文本框（718-732），失焦无校验；上限用 `ChannelPeriodAmount`（735-741）；「Done」关闭（868-874），草稿留在 RHF 表单。
- (d) 调整用量：表行「Usage」（587-596，仅已保存行）或「预算用量」页签选行 → `ChannelBudgetUsagePanel`；pool 显示「Pool used」+「Set usage」（`usage:121-139`），user 列出用户 +「Set usage」（145-185）→ 内联金额输入 →「Confirm」PUT（218-246）。
- (e) 个人提额：个人覆盖页签搜索用户 →「Temporarily increase」（`limits:497-526`）→ AlertDialog → `ChannelPeriodOverrideEditor`：选预算（仅 `scope=user && limit>0`，`override:62-65`）→ 金额 + 到期 →「Save budget override」/「Revoke selected override」（182-198）。校验仅在提交时（79-92）。
- (f) 超限动作：行级在 Sheet 内 `When exceeded` 三选一，fallback 时再选目标渠道/模型（742-861）；默认动作在表下独立区块（903-999），只有 reject/fallback。
- (g) 预览：表单 submit = 「Preview effective limits」（1000）→ POST preview → 纯文本行列表（1021-1038）。
- (h) 保存：仅当草稿 JSON 与预览快照一致时才显示「Confirm and save policy」（1013, 271-273, 245）；PUT 成功 → toast + `setQueryData` 重挂载；409 → 锁定 fieldset，只能「Reload」。
- 用量展示：「Used / Remaining」列每行挂 `ChannelBudgetProgress`（`progress:31-130`）：pool 取 `used_quota`；user 取第一页第一个用户再查其 `user-limit-status` 取 metric，显示「Highest usage: X」+ `Progress`。

## 4. UX 问题清单

结构性（信息架构）
1. 切换页签即丢草稿：`limits:562` 按 `activeTab` 条件挂载，未保存编辑静默销毁；关闭 Dialog、「Reload」（`panel:115-125`）同样无确认；`form.formState.isDirty` 未用。
2. 保存按钮藏在预览里：主按钮是「预览」，「确认并保存」只在预览区末尾且草稿一改即消失（`panel:1013`），无提示。
3. 模态套模态：Dialog → Sheet（`panel:620`）；Dialog → AlertDialog（`limits:868`）。AlertDialog 被当成完整编辑表单（两套互不相关的保存/撤销按钮：`override:182-198` 与 `limits:938-976`）。
4. 默认页签不是第一个页签（`limits:261/549`），「预算用量」页签与表行「Usage」两条路径打开同一面板；行内面板渲染在整个长表单最底部（`panel:1004`）。
5. 10 列宽表（`panel:501-518`）塞进 6xl Dialog；「Used / Remaining」列在 user 行显示"用量最高的单个用户"，列头语义不符（`progress:106-121`）。
6. N+1 请求：每个已保存行独立发 1~2 个请求（`progress:36-67`），128 行上限时数百请求，且每格显示裸文本 `Loading...`。
7. 时段全部展开（`panel:325-467`），无折叠/列表；看不到"哪个预算用了哪个时段"。
8. 已保存行/时段不可删除、类型不可改（`panel:345, 370, 587-609`）仅靠一行小字说明。
9. 提额入口标签错位：「Temporarily increase」（`limits:846`）打开并发+预算混合弹窗；渠道无并发上限时并发输入禁用且错误文案永远显示（`limits:931-937`）。
10. 保存后 0 上限的行无法提额（`override:64` 过滤 `limit>0`），UI 无解释。

校验与反馈
11. zod 字段级错误从不渲染，任何校验失败都是一句「Check schedules, overlapping budgets, and quota values.」（`panel:241`）；400 同样映射为该文案（`period-api.ts:53-55`），后端 message 被丢弃。
12. 「整段」必须选时段仅靠 `required`（`panel:696`），Sheet 关闭后输入卸载，约束失效直到服务端 400。
13. 撤销/清零无二次确认：「Revoke override」（`limits:946`）、「Revoke selected override」（`override:190-197`）、「Set usage」设 0（`usage:238`）；项目已有 `ConfirmDialog`。
14. 同渠道换模型语义可疑：选择"本渠道"时写入 `selfTarget.id` 而非 0（`panel:776-779`），摘要与模型过滤都以 `channel_id===0` 判断（`panel:264, 846`）。需与后端归一化确认。

打磨（polish）
15. 模型输入是逗号文本框（`panel:718`），渠道抽屉已用 `MultiSelect`（`channel-mutate-drawer.tsx:3239`）；`selfTarget.models` 可作候选。
16. 术语："Weekly"同时是窗口与时段类型；「整段」「作用域」无解释。
17. 时区：顶部写「服务器时区」，`datetime-local` 无时区标注（`panel:387-405`），提额处又写「(local time)」（`override:171`）。
18. 单位：`Limit (USD)` vs 表格 `formatQuota` 缩写；"0 = 不限"只在脚注（`panel:863-867`）；`amount` 无前缀/步进/千分位。
19. 表格行状态不可见：禁用行不变灰，超限/未生效无颜色；`Enabled` 开关 aria-label 无行上下文（535）。
20. 一个 `<label>` 包两个控件（`panel:410-432`）。
21. 加载/空态不一致：`<p role=status>Loading...</p>`（`panel:128`）、原生 `<table>`（`usage:146`），而其他页签用 `LoadingState/Empty/Table`。
22. 预览结果是纯文本 `<p>` 串（`panel:1021-1038`），无法对照原表。
23. 移动端：TabsList 2 列、10 列表格、Sheet 全宽、AlertDialog 90dvh 内再滚动。
24. i18n：`{t('Used')} / {t('Remaining')}`、`fallback · #id · model` 等拼接串不可整体翻译（`panel:512, 268`）。

## 5. 现有组件体系

- 已用：`Dialog` 封装（`components/dialog.tsx`）、`ui/sheet`、`ui/tabs`、`ui/table`、`ui/switch`、`ui/field`、`ui/native-select`、`ui/progress`、`ui/alert`、`ui/alert-dialog`、`ui/empty`、`ui/tooltip`、`sonner`；表单 RHF + zod（`panel:158-171`，未用 `ui/form` 的 `FormField/FormMessage`）。
- 可用未用：`ui/form`、`ui/select`、`ui/combobox`、`ui/badge`、`ui/skeleton`、`ui/spinner`、`ui/card`、`ui/accordion`/`collapsible`、`ui/pagination`、`components/multi-select`、`tag-input`、`datetime-picker`、`date-picker`、`confirm-dialog`、`empty-state`/`error-state`/`loading-state`、`auto-skeleton`、`data-table/`、`status-badge`、`channels/components/numeric-spinner-input.tsx`。
- 应遵循：`channel-mutate-drawer.tsx:1827-1911` 的 Sheet + `sideDrawer*ClassName`（`components/drawer-layout.ts`）+ `Form/FormField` + `MultiSelect`；`limits` 中的 `LoadingState/ErrorState/Pagination/RefreshButton` 与 `Empty` 空态。

## 6. 测试约束

`__tests__/channel-period-policy-panel.test.tsx`（按钮 `textContent` 与 `querySelector` 定位）：
- 按钮文案：`Add budget`、`Edit`、`Done`、`Usage`、`Set usage`、`Confirm`、`Retry`、`Reload`、`Confirm and save policy`（预览前必须不存在）。
- 选择器：`form` submit 触发预览；`[aria-label="Budget editor"]` 且 `[role="dialog"]`；`input[name="budgets.N.name"]`、`select[name="budgets.N.schedule_id"]`；`[aria-label="Budget usage"]`；`[aria-label="Default action when exceeded"]` 且 DOM 顺序在 `table` 之后；`[role="progressbar"]` 的 `aria-valuenow`；`[role="switch"][aria-label="Continuously track model usage"]`；`[role="alert"]` 含 `policy has changed`；`fieldset.disabled`。
- 契约：preview POST 带 `expected_revision`，save PUT body 与 preview 完全相等；新行 id `new-` 前缀且不请求 usage；文本含 `Pool used`、`Pool daily`、`Holiday`。

`__tests__/channel-user-limits-dialog.test.tsx`：
- 页签文案 `Personal overrides`、`Current concurrency`；`Set usage`（无权限时 disabled）、`Confirm`、`Search`、`Next`、`Temporarily increase`、`Save override`；文本 `Set current usage for Alice.`、`Page 1 of 2`、`User daily`。
- 选择器：`[aria-label="Budget usage"]` 内 `input[type="number"]`、`button[aria-label="Refresh"]`、`input[aria-label="Search users"]`、`#personal-concurrency` 存在且 `#personal-daily` 必须为 null；并发轮询仅在该页签开启。
