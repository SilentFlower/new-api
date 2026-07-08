# 迁移 build-bak 旧 UI 独有功能到新 UI实现计划

## 前置检查

1. 确认工作区状态，保护当前未提交 classic 修复：
   - `web/classic/src/hooks/channels/useChannelsData.jsx`
   - `web/classic/src/components/table/channels/modals/EditChannelModal.jsx`
2. 阅读本任务 `prd.md`、`design.md`。
3. 使用 `trellis-before-dev` 读取 default 前端和后端 API 相关规范。
4. 实现前按 Trellis route gate 选择 implement 路由。
5. 实现前快速浏览相关 default 页面和共享组件，确认当前布局、按钮、弹窗、筛选器、表格和状态组件的使用模式。

## 实现顺序

### 0. 新 UI 风格基线梳理

- 读取并对照 `web/default/components.json`，确认当前 shadcn/base 配置、Tailwind token 和 icon library。
- 复核 API Keys、Dashboard、Usage Logs 当前页面结构和 action 区布局。
- 复核可复用组件：`Dialog`、`MultiSelect`、`EmptyState`、`ErrorState`、`LoadingState`、DataTable bulk actions、Button、Tooltip。
- 后续新增 UI 必须先尝试复用上述组件；只有现有组件无法表达功能时才新增小组件，并保持同一主题变量和布局密度。

### 1. 渠道 parity 快速复核

- 复核 `transformFormDataToUpdatePayload()` 不发送 `status`。
- 复核渠道启用/禁用、批量启用/禁用均走专用状态接口。
- 复核 Vision Assist、WebSearch、上游模型计费开关在 default 中读写字段完整。
- 若只发现小缺口，先补齐，避免后续实现期间遗忘。

### 2. API Keys 迁移入口

- 在 `web/default/src/features/keys/api.ts` 增加 `/api/token/migrate` 封装。
- 在 `types.ts` 增加迁移结果类型。
- 新增迁移 Dialog。
- 在 `DataTableBulkActions` 中加入 Root-only 迁移按钮。
- 成功后刷新列表、清空选择。

### 3. Dashboard 筛选与导出

- 扩展 `DashboardFilters`、查询参数构造和 API 封装。
- 增加分组/令牌选项加载 helper。
- 修改 `ModelsFilter`，增加分组多选、令牌多选和联动清理。
- 将筛选条件传给 Model Analytics 统计卡片、图表、User Analytics 可映射查询。
- 新增导出 Dialog 和 blob 下载逻辑。
- 确认 URLSearchParams 发送重复 key。

### 4. 公共 API Key LogViewer

- 新增 `/log` route 和公共 LogViewer feature。
- 实现 API Key 验证、状态管理、切换 Key。
- 接入 token stat、token logs、self quota 数据。
- 复用或抽取 usage logs 的安全渲染工具；公共列不得包含管理员专用敏感字段。
- 处理 401、403、429 和普通错误提示。

### 5. i18n

- 对所有新增 default 文案使用英文 key。
- 运行 `web/default` i18n 同步流程。
- 补齐 `en`、`zh`、`fr`、`ja`、`ru`、`vi`。

## 验证计划

```bash
cd web/default && bun run build
```

若环境没有 Bun，可使用仓库已有本地构建入口并记录原因：

```bash
cd web/default && ./node_modules/.bin/rsbuild build
```

后端契约或 API 参数变更时运行：

```bash
go test ./controller ./model
```

Dashboard 多值参数重点检查：

- `groups=a&groups=b`
- `groups[]=a&groups[]=b`
- 旧 `group=a`
- `token_names=a&token_names=b`
- 旧 `token_name=a`

## 回归场景

- Root 用户勾选 2 个 API Key 迁移，结果页显示逐项成功/失败。
- 非 Root 用户看不到迁移按钮。
- Dashboard 未选择任何筛选时原图表可加载。
- Dashboard 选择分组后令牌选项收窄，已选非法令牌被清理。
- Dashboard 同时选择分组和令牌时后端请求包含重复 key。
- Dashboard 导出下载文件，后端响应失败时展示错误。
- `/log` 未登录输入有效 API Key 后能查询；输入无效 Key 显示错误。
- `/log` 切换 Key 后清空旧数据。
- 渠道禁用后编辑保存，不会变启用，不会出现“无效的参数”。
- 新增迁移弹窗、Dashboard 筛选/导出、公共 `/log` 页面与 default 现有页面风格一致，不出现 classic/Semi Design 样式痕迹、布局割裂、移动端溢出或控件重叠。

## 风险文件

- `web/default/src/features/keys/components/data-table-bulk-actions.tsx`
- `web/default/src/features/keys/api.ts`
- `web/default/src/features/keys/types.ts`
- `web/default/src/features/dashboard/api.ts`
- `web/default/src/features/dashboard/types.ts`
- `web/default/src/features/dashboard/components/models/models-filter-dialog.tsx`
- `web/default/src/features/dashboard/index.tsx`
- `web/default/src/routes/log.tsx` 或等价路由文件
- `web/default/src/features/usage-logs/*`（只在复用日志渲染时触碰）

## 提交策略

- 推荐拆成 3 个实现提交：
  1. `feat(default-keys): 迁移令牌独立账号入口`
  2. `feat(default-dashboard): 补齐分组令牌筛选与导出`
  3. `feat(default-log): 增加公共 API Key 日志查看器`
- 当前 classic 状态修复可单独提交，避免和本迁移任务混在一起。
