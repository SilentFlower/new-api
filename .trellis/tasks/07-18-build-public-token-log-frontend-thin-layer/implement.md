# Implement — 公共 Token 日志前端薄层化

## Checklist

1. 读取 `web/default/src/features/token-logs/index.tsx`、`api.ts`、`lib.ts`、`types.ts`。
2. 读取 `web/classic/src/pages/LogViewer/index.jsx`。
3. Default：
   - 先拆纯函数和类型依赖，再拆组件，最后拆 hooks。
   - 保持 `useTranslation()` 在需要响应语言切换的组件内使用。
   - 不新增文案；如必须新增，按 i18n skill 脚本流程补 locale。
4. Classic：
   - 先拆工具函数，再拆展示组件，再拆数据/筛选 hook。
   - 保留原页面路由默认导出。
5. 只格式化涉及文件，不全仓格式化。
6. 执行 Default typecheck/build 和 Classic build。

## Validation

- `cd web/default && bun run typecheck && bun run build`
- `cd web/classic && bun run build`
- 如改动 Default i18n key：
  - `cd web/default && bun run i18n:sync`
  - 检查 `_sync-report.json`
- `git diff --check`

## Risk

- 风险点：拆分组件后闭包状态、分页请求参数或语言切换响应失效。
- 控制：先按原行为移动，不顺手优化；Default 子组件需要文案时自行 `useTranslation()`，保持 React 响应式语言切换。
