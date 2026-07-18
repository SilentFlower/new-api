# Design — 公共 Token 日志前端薄层化

## Default Frontend Boundary

目标目录：`web/default/src/features/token-logs/`

建议拆分：

- `components/`
  - 统计卡、模型分布/排行、筛选工具栏、表格、空态或局部展示组件。
- `hooks/`
  - 公共 token 解析、日志查询、分页/筛选状态、切换 key 行为。
- `lib/`
  - 保留或扩展纯格式化、查询参数、数值展示、时间范围处理。
- `index.tsx`
  - 保留 `PublicLayout`、`PublicTokenLogsContent`、页面级 client 初始化和组件挂载。

Default 不引入新依赖，不改路由。若只移动现有文案和 `t()` 调用，不需要改 locale；若产生新 key，必须按 i18n skill 通过 `add-missing-keys.mjs` 和 `bun run i18n:sync` 写入。

## Classic Frontend Boundary

目标目录：`web/classic/src/pages/LogViewer/`

建议拆分：

- `components/`：统计区、筛选区、表格区、弹窗或详情展示。
- `hooks/`：日志查询、分页/筛选、public token key 状态。
- `utils/` 或 `lib/`：格式化、参数构造、导出辅助。
- `index.jsx`：保留页面级状态编排和组件挂载。

Classic 没有 TypeScript typecheck，主要靠 build 和现有 lint/build 约束回归。

## Contracts

- API 层：
  - Default 继续复用 `api.ts`、`types.ts`。
  - 不改变公共日志接口参数和响应处理。
- UI 层：
  - 文案、表格列、筛选默认值、分页和统计展示保持不变。
  - 不调整 CSS class 的视觉语义，除非移动组件所必需。
- 性能：
  - 拆分组件时避免把所有状态下沉到全局 store。
  - 对大列表/表格保持现有渲染机制，不引入额外请求瀑布。

## Rollback

删除新增前端文件，把拆出的组件/hook/工具恢复到原 `index` 文件；不涉及后端和数据库。

## Upstream Sync Review Point

上游若更新公共日志页面，优先复核页面入口和拆分组件的职责映射，不为消除少量重复而重新合并或全量重写。
