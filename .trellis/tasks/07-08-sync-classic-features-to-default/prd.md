# 迁移 build-bak 旧 UI 独有功能到新 UI

## Goal

把 `build-bak` 分支中已经在 `web/classic` 落地、但 `web/default` 缺失或实现不完整的用户功能迁移到新 UI。迁移后，用户使用默认新 UI 时不应因为切换前端主题而丢失 build-bak 的核心能力。

## Background

- 当前分支：`build-bak`。
- 对比基线：`main..build-bak`。
- 用户明确要求优先关注“我改的在 build-bak 里面的一些特性”，不是泛泛同步所有旧 UI 代码。
- 本任务只规划新 UI 迁移，不处理 Trellis/Claude/Codex 配置文件迁移。
- 当前工作区另有未提交 classic 修复：
  - `web/classic/src/hooks/channels/useChannelsData.jsx`：渠道启用/禁用改走 `POST /api/channel/:id/status`。
  - `web/classic/src/components/table/channels/modals/EditChannelModal.jsx`：编辑保存前删除 `status`，避免禁用渠道保存后被隐式开启。

## Confirmed Facts

### build-bak 已提交特性审计

| 特性 | build-bak 来源 | classic 状态 | default 状态 | 任务结论 |
| --- | --- | --- | --- | --- |
| 令牌迁移到独立账号 | `9a9f838e`、`ed5602b7` | 已有批量按钮、二步确认/结果弹窗 | 未发现入口；`ApiKeysDialogType` 无 migrate，API 无 `/api/token/migrate` 封装 | 需要迁移 |
| 数据看板按分组/令牌筛选 | `cc6882bc`、`0e72fabe`、`be0c6d42` | 搜索弹窗支持快捷时间、分组多选、令牌多选；导出弹窗支持分组/令牌筛选 | Model Analytics 仅支持时间、粒度、用户名；无 `groups` / `token_names`；未发现导出报表入口 | 需要迁移 |
| 公共 API Key 日志查看器 | `93a238a7`、`8471e8a6` | `/log` 无需登录，输入 API Key 后查看该 Key 统计、图表、日志 | `/console/log` 直接重定向到登录后的 `/usage-logs`；未发现公开 `/log` 页面 | 需要迁移 |
| 渠道视觉辅助配置 | `329e77fe`、`fae993f3` | 已有配置项 | default 已有 `vision_assist_*` 表单、类型与 i18n | 仅复核 |
| 渠道 WebSearch 配置 | `e280416c`、`17ba4cc3` | 已补齐 classic 配置 | default 已有 `web_search_*` 表单、类型与 i18n | 仅复核 |
| 映射后按上游模型计费 | `8106a70c` | classic 已有 `use_upstream_model_for_billing` | default 已有字段、表单和保存逻辑 | 仅复核 |
| 渠道禁用/保存状态修复 | 当前未提交 classic 修复 | 已修复本地未提交 | default 已有专用状态接口；编辑 update payload 当前未发送 `status` | 复核并必要时微调 |

### 后端契约

- `/api/token/migrate` 已由后端实现，Root 权限、逐令牌结果、响应不返回密码。
- Dashboard 后端已经支持：
  - 多值 `token_names` / `groups`，并兼容旧单值 `token_name` / `group`。
  - `/api/data/`、`/api/data/self`、`/api/data/users`、`/api/log/stat`、`/api/log/self/stat`、`/api/data/export` 的相关过滤路径。
- 多值查询参数项目规范要求优先使用重复 key，例如 `token_names=a&token_names=b`。
- 公共 API Key LogViewer 使用 Bearer API Key 调用：
  - `/api/log/token/stat`
  - `/api/log/token`
  - `/api/data/self`

## Requirements

### R0：新 UI 风格一致性

- 迁移功能必须适配 `web/default` 的现有视觉和交互体系，不能把 classic/Semi Design 的布局、控件样式或交互习惯直接搬进新 UI。
- 新增页面、弹窗、表格操作、筛选器、空状态、错误状态、加载状态应优先复用 `web/default/src/components` 和 `web/default/src/components/ui` 下已有组件，例如 Dialog、Drawer、DataTable、MultiSelect、EmptyState、ErrorState、LoadingState、Button、Tooltip。
- 新增批量操作按钮应延续现有 API Keys 表格模式：紧凑 icon button、tooltip、清晰 aria-label，不额外插入割裂的大按钮区。
- Dashboard 新增筛选和导出入口应融入现有 Model Analytics action 区和过滤弹窗，不新增与当前布局密度不一致的独立大面板。
- 公共 `/log` 页面应使用 default 的布局、卡片密度、表格和状态组件；可以参考 classic 功能契约，但不能照搬 classic 的视觉结构。
- 不引入新的 UI 组件库，不引入 Semi Design，不新增与 default token/主题变量冲突的 CSS 体系。

### R1：新 UI 令牌迁移入口

- 在 `web/default` 的 API Keys 页面为 Root 用户提供“迁移到独立账号”批量操作。
- 入口只对 Root 用户可见或可用；后端仍以 RootAuth 为最终权限边界。
- 迁移流程需要二步：
  1. 确认页：展示所选令牌名称和密钥展示值，提示会为每个令牌创建新用户，Key 和 Group 保持不变。
  2. 结果页：展示每个令牌的 `token_name`、`new_username`、`status`、`error`。
- 成功关闭结果页后刷新 API Keys 列表并清空选择。
- 不在前端展示或记录新用户密码。

### R2：新 UI Dashboard 分组/令牌筛选与导出

- Model Analytics 过滤弹窗支持：
  - 快捷时间范围保持现有新 UI 体验。
  - 管理员可多选分组。
  - 可多选令牌。
  - 选择分组后令牌选项按分组联动收窄。
- 查询请求需要把 `groups` 和 `token_names` 传给后端，并使用重复 key 序列化。
- 统计卡片、模型图表、管理员用户分析中与 classic 对应的数据范围应使用同一组过滤条件。
- Dashboard 新 UI 需要提供导出报表入口，调用 `/api/data/export`，并支持时间、分组、令牌过滤。
- 未选择分组和令牌时保持现有默认行为。

### R3：新 UI 公共 API Key 日志查看器

- 新增或恢复公开 `/log` 路由，不要求登录。
- 用户输入 API Key 后，使用该 Key 作为 Bearer token 查询该 Key 自己的统计、图表和日志。
- 页面必须与后台使用日志区分：不能把 `/console/log` 继续等同为公开 API Key 查看器。
- 支持切换 Key、时间范围、日志类型/模型/请求 ID 等 classic 已有核心过滤能力。
- 隐私边界：公共查看器不得展示管理员专用敏感列；沿用 classic 的公共模式脱敏策略。

### R4：渠道功能 parity 复核

- 复核 default 渠道表单能读写以下 build-bak 渠道配置：
  - `setting.vision_assist.*`
  - `setting.web_search.*`
  - `setting.use_upstream_model_for_billing`
  - `settings.allow_service_tier`、`disable_store`、`allow_safety_identifier`、`allow_include_obfuscation`、`allow_inference_geo`、`allow_speed`、`claude_beta_query`
- 复核 default 渠道启用/禁用只走专用状态接口；编辑保存不应把 `status` 带到 `PUT /api/channel/`。
- 如果复核发现缺口，作为本任务的小修补齐。

## Acceptance Criteria

- [ ] 新 UI API Keys 页面 Root 用户能选择多个令牌并执行“迁移到独立账号”。
- [ ] 非 Root 用户在新 UI 看不到或无法执行迁移入口。
- [ ] 新 UI 迁移弹窗展示逐令牌结果，且不展示密码。
- [ ] 迁移成功后 API Keys 列表刷新，已迁移令牌不再停留在原 Root 用户列表中。
- [ ] 新增迁移弹窗、Dashboard 筛选/导出和公共 `/log` 页面符合 `web/default` 现有视觉与交互风格，没有引入 classic/Semi Design 风格或新的 UI 组件库。
- [ ] 新增批量操作、筛选器、导出入口、状态展示复用 default 现有组件和主题 token，桌面和移动端均不出现明显割裂、溢出或重叠。
- [ ] 新 UI Dashboard Model Analytics 可按多个分组、多个令牌筛选，分组与令牌取交集。
- [ ] 新 UI Dashboard 的统计卡片、模型图表、用户分析与导出报表使用一致的筛选条件。
- [ ] 新 UI Dashboard 导出报表未选择分组/令牌时保持原全量导出行为。
- [ ] 新 UI `/log` 无需登录即可输入 API Key 查询自身日志和统计。
- [ ] 新 UI `/console/log` 的兼容跳转策略不破坏公开 `/log`。
- [ ] default 渠道编辑保存不会触发“禁用渠道保存后变启用”或后端“无效参数”。
- [ ] default 渠道视觉辅助、WebSearch、上游模型计费开关读写与 classic/build-bak 后端契约一致。
- [ ] `web/default` 新增文案完成 `en`、`zh`、`fr`、`ja`、`ru`、`vi` 翻译。
- [ ] 相关前端构建通过；涉及后端契约时相关 Go 测试通过。

## Out of Scope

- 不迁移 Trellis、Claude、Codex、CI 等工程配置文件。
- 不重写后端 token migrate、Dashboard 多值筛选、WebSearch、Vision Assist 的核心逻辑，除非新 UI 接入时暴露契约 bug。
- 不改变 Excel Sheet 结构、字段含义或后端导出文件格式。
- 不做公共 LogViewer 的全新产品设计；优先迁移 classic 已有功能到 default 风格。
- 不回填历史数据，不新增数据库字段。

## Decision

- 已确认一次性迁移 R1、R2、R3，并只对 R4 做复核/必要小修。这样覆盖 build-bak 中 classic 明确独有的三类用户可见功能，同时避免重复实现 default 已有的渠道大功能。
