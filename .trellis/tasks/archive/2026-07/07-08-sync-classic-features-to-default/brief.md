# Brief — 迁移旧 UI 独有功能到新 UI

## Goal

- 把 `build-bak` 分支中已经在 `web/classic` 落地、但 `web/default` 缺失或实现不完整的用户功能迁移到新 UI，确保切换到默认新 UI 后不丢失 build-bak 的核心能力。

## Scope

- R0：所有迁移功能必须适配 `web/default` 现有 `base-nova`、Base UI/shadcn、Tailwind token 和组件体系；迁移功能契约，不照搬 classic/Semi Design 视觉。
- R1：在新 UI API Keys 页面为 Root 用户增加“迁移到独立账号”批量入口，接入 `/api/token/migrate`，提供确认页、逐令牌结果页、成功后刷新列表并清空选择，且不展示新用户密码。
- R2：在新 UI Dashboard Model Analytics 增加管理员分组多选、令牌多选、分组与令牌联动筛选，并让统计卡片、模型图表、用户分析和导出报表使用一致过滤条件。
- R3：新增公开 `/log` 页面，允许未登录用户输入 API Key 后用 Bearer API Key 查询自身统计、图表和日志；页面与登录后台 Usage Logs 区分，公共模式不得展示管理员敏感列。
- R4：复核新 UI 渠道配置 parity，包括 Vision Assist、WebSearch、映射后按上游模型计费、额外渠道设置，以及启用/禁用和编辑保存状态路径；发现小缺口则在本任务中补齐。
- 新增 default 前端文案需要完成 `en`、`zh`、`fr`、`ja`、`ru`、`vi` 翻译。

## Non-Goals

- 不迁移 Trellis、Claude、Codex、CI 等工程配置文件。
- 不重写后端 token migrate、Dashboard 多值筛选、WebSearch、Vision Assist 的核心逻辑，除非新 UI 接入时暴露契约 bug。
- 不改变 Excel Sheet 结构、字段含义或后端导出文件格式。
- 不做公共 LogViewer 的全新产品设计；优先迁移 classic 已有功能到 default 风格。
- 不引入新的 UI 组件库，不引入 Semi Design，不新增与 default 主题变量冲突的 CSS 体系。
- 不回填历史数据，不新增数据库字段。

## Key Context

- 当前分支是 `build-bak`，对比基线是 `main..build-bak`。
- 当前工作区已有未提交 classic 修复，需要保留且不要误回滚：
  - `web/classic/src/hooks/channels/useChannelsData.jsx`
  - `web/classic/src/components/table/channels/modals/EditChannelModal.jsx`
- default API Keys 相关入口：
  - `web/default/src/features/keys/api.ts`
  - `web/default/src/features/keys/types.ts`
  - `web/default/src/features/keys/components/data-table-bulk-actions.tsx`
  - `web/default/src/features/keys/components/api-keys-provider.tsx`
- default Dashboard 相关入口：
  - `web/default/src/features/dashboard/api.ts`
  - `web/default/src/features/dashboard/types.ts`
  - `web/default/src/features/dashboard/lib/filters.ts`
  - `web/default/src/features/dashboard/components/models/models-filter-dialog.tsx`
  - `web/default/src/features/dashboard/index.tsx`
- public LogViewer 路由预计新增：
  - `web/default/src/routes/log.tsx`
  - 可复用或参考 `web/default/src/features/usage-logs/*`
- Dashboard 多值查询参数必须使用重复 key，例如 `groups=a&groups=b&token_names=x&token_names=y`，不能依赖默认数组序列化。
- 公共 LogViewer 需要本地 API client，使用用户输入的 API Key 作为 Bearer token，不依赖登录态全局 client。
- 新增 UI 应优先复用 `Dialog`、`MultiSelect`、`EmptyState`、`ErrorState`、`LoadingState`、DataTable bulk actions、Button、Tooltip 等 default 现有组件。
- API Keys 批量迁移入口应延续当前批量工具栏的紧凑 icon button + Tooltip 模式；Dashboard 筛选/导出应并入现有 Model Analytics action 区和过滤弹窗；公共 `/log` 应使用 default 的页面布局、统计卡片、过滤条和表格密度。
- 风险：`token_names` 是按名称过滤，不是 token id；同名令牌会一起命中，前端应在选项标签里展示 username/group 降低误选风险。
- 风险：公共 LogViewer 复用后台日志组件时可能泄露管理员列或引入登录态依赖，列定义需要专门检查。
- 风险：新增 TanStack route 后需要通过既有生成/构建流程确认路由生效。

## Acceptance

- 新 UI API Keys 页面 Root 用户能选择多个令牌并执行“迁移到独立账号”；非 Root 用户看不到或无法执行迁移入口。
- 迁移弹窗展示逐令牌结果，不展示密码；成功关闭后刷新 API Keys 列表并清空选择。
- 新增迁移弹窗、Dashboard 筛选/导出和公共 `/log` 页面符合 default 现有视觉和交互风格，没有 classic/Semi Design 样式痕迹或新的 UI 组件库。
- 新增批量操作、筛选器、导出入口、状态展示复用 default 现有组件和主题 token，桌面和移动端均不出现明显割裂、溢出或重叠。
- 新 UI Dashboard Model Analytics 可按多个分组、多个令牌筛选，分组与令牌取交集；未选择分组和令牌时保持现有默认行为。
- Dashboard 统计卡片、模型图表、用户分析与导出报表使用一致的筛选条件，导出失败时展示错误。
- 新 UI `/log` 无需登录即可输入 API Key 查询自身日志和统计；切换 Key 后清空旧数据。
- 新 UI `/console/log` 的兼容跳转策略不破坏公开 `/log`。
- default 渠道编辑保存不会触发“禁用渠道保存后变启用”或后端“无效的参数”。
- default 渠道视觉辅助、WebSearch、上游模型计费开关读写与 classic/build-bak 后端契约一致。
- `web/default` 新增文案完成 `en`、`zh`、`fr`、`ja`、`ru`、`vi` 翻译。
- `web/default` 前端构建通过；如涉及后端契约变更，相关 Go 测试通过。

## Current Implementation Status

- 已实现 R1：新 UI API Keys Root-only 批量迁移入口、二步确认/结果弹窗、`/api/token/migrate` 封装、成功后刷新与清空选择。
- 已实现 R2：Dashboard 分组/API Key 多选筛选、重复 key 查询序列化、分组与 API Key 联动、管理员用户分析共享筛选、Dashboard 导出弹窗。
- 已实现 R3：公开 `/log` 路由和 default 风格公共 API Key 日志查看器，使用本地 Bearer API Key client，不依赖全局登录态。
- 已复核 R4：default 渠道状态切换走专用状态接口，编辑保存 payload 不带 `status`；Vision Assist、WebSearch、上游模型计费及扩展设置已有读写路径。
- 已补齐 default 前端 i18n：`en`、`zh`、`zh-TW`、`fr`、`ja`、`ru`、`vi` 同步报告均为 0 missing / 0 extras / 0 untranslated。

## Verification Notes

- `git diff --check` 通过。
- `go test ./controller ./model` 通过。
- TanStack route tree 已通过 `npx @tanstack/router-cli@1.167.18 generate` 更新，`/log` 已进入 `web/default/src/routeTree.gen.ts`。
- `./node_modules/.bin/tsc -b --pretty false | rg "features/(token-logs|dashboard|keys)|routes/log|routeTree.gen"` 没有本轮文件相关错误。
- 全量 `tsc -b` 和 `rsbuild build` 当前受本地依赖环境阻塞：`web/default/node_modules` 缺少 `@rsbuild/plugin-tailwindcss`、部分 CodeMirror/markdown 依赖；`package.json` 的 `typecheck` 脚本还依赖当前环境未安装的 `tsgo`。
