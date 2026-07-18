# Brief — 公共 Token 日志前端薄层化

## Goal

- 拆薄 Default 和 Classic 公共 Token 日志大页面入口，保持现有功能不变。

## Scope

- Default：在 `web/default/src/features/token-logs/` 下拆 `components/`、`hooks/`、`lib/`，`index.tsx` 保留布局和编排。
- Classic：在 `web/classic/src/pages/LogViewer/` 下拆组件、hook、工具，`index.jsx` 保留默认导出和页面编排。
- 保持 API、筛选、分页、统计、表格、key 切换和文案行为。

## Non-Goals

- 不做 UI 改版，不改后端 API，不新增依赖，不删除 Classic。

## Key Context

- 当前厚点：`web/default/src/features/token-logs/index.tsx` 约 1057 行；`web/classic/src/pages/LogViewer/index.jsx` 约 1301 行。
- Default 必须使用 Bun、TypeScript typecheck、React i18n。
- 如新增或调整 locale，必须通过 i18n 脚本，不能手工编辑 JSON。

## Acceptance

- `cd web/default && bun run typecheck && bun run build` 通过。
- `cd web/classic && bun run build` 通过。
- 两个入口文件明显变薄，用户行为保持不变。

## Next Step

- 先拆 Default 纯逻辑和组件，再拆 Classic 页面，最后运行前端构建验证。
