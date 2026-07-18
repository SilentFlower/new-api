# 公共 Token 日志前端薄层化

## Goal

把 Default 和 Classic 的公共 Token 日志大页面拆成更薄的页面入口、hook、组件和工具文件，保持现有查询、筛选、统计、表格、切换 key、i18n 和构建行为不变。

## Background

- `web/default/src/features/token-logs/index.tsx` 当前约 1057 行，内部同时包含格式化函数、统计卡、排行榜、筛选工具栏、表格、数据获取和公共布局入口。
- `web/default/src/features/token-logs/api.ts`、`lib.ts`、`types.ts` 已存在，可继续作为稳定边界。
- `web/classic/src/pages/LogViewer/index.jsx` 当前约 1301 行，页面入口同时承担数据请求、统计、筛选、表格和多个弹窗/交互。
- Default 前端必须遵循 `web/default/AGENTS.md`：React 19、TypeScript、Bun、i18n、文件组织和类型检查。
- i18n 约束：如新增或调整 Default UI 文案，不得手工编辑 locale JSON，必须通过脚本并运行 `bun run i18n:sync`。

## Requirements

- R1：Default 页面入口 `index.tsx` 应降为布局和编排层；复杂 UI 拆到 `components/`，请求和状态拆到 `hooks/`，格式化/查询参数等纯逻辑放到 `lib/`。
- R2：Classic 页面入口同样拆分到 `web/classic/src/pages/LogViewer/` 下的领域文件；不改变路由和已有版权头。
- R3：保持 Default 的 i18n 使用方式，所有用户可见文案继续通过 `useTranslation()` / `t()` 渲染。
- R4：不新增依赖，不替换组件库，不做 UI 视觉改版。
- R5：不改变 API 请求参数、分页、排序、日期范围、统计口径、表格列、导出/刷新和 key 切换行为。
- R6：拆分后避免循环依赖，组件 props 使用明确类型；Default TS/TSX 改动必须通过 typecheck。

## Acceptance Criteria

- [ ] `web/default/src/features/token-logs/index.tsx` 只保留公共布局、client 初始化和页面级编排。
- [ ] Default 新增 `components/`、`hooks/` 或 `lib/` 文件承载原页面内部职责，类型定义复用 `types.ts`。
- [ ] `web/classic/src/pages/LogViewer/index.jsx` 明显变薄，新增同目录组件/hook/工具文件承载原内部职责。
- [ ] 用户可见行为与文案不变；如新增 i18n key，六个 locale 均通过脚本补齐并同步。
- [ ] `cd web/default && bun run typecheck && bun run build` 通过。
- [ ] `cd web/classic && bun run build` 通过。

## Out of Scope

- 不重做公共日志产品体验。
- 不调整后端日志 API。
- 不删除 Classic 页面。
- 不进行全站样式或组件体系迁移。
