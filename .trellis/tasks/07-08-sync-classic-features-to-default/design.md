# 迁移 build-bak 旧 UI 独有功能到新 UI设计

## 设计原则

- 以 `build-bak` 功能契约为准，不按 classic 文件逐行移植。
- 新 UI 使用 `web/default` 现有技术栈：React、TypeScript、TanStack Router、React Query、Base UI/shadcn 风格组件、Tailwind。
- 复用现有 `web/default` 组件和数据表模式，避免引入 Semi Design。
- 新增用户可见文案必须通过 `useTranslation()` 和 locale 文件维护。

## 新 UI 风格契约

本任务迁移的是功能契约，不迁移 classic 的视觉实现。所有新增界面必须融入 `web/default` 当前的 `base-nova`、Tailwind token、Base UI/shadcn 组合风格。

### 组件复用

- 弹窗优先使用 `web/default/src/components/dialog.tsx` 或现有 `components/ui/dialog` 组合，不单独实现一套 modal 外观。
- 多选筛选优先使用 `web/default/src/components/multi-select.tsx`，保留 chip、搜索、紧凑高度、禁用态和主题样式。
- 表格批量操作延续 `web/default/src/components/data-table` 的 bulk actions 模式，按钮使用紧凑 icon button + Tooltip + aria-label。
- 空状态、错误状态、加载状态优先使用 `EmptyState`、`ErrorState`、`LoadingState`，不要新增与现有状态页割裂的插画或提示框。
- 新增按钮、输入框、Badge、Tabs、Popover 等基础控件优先从 `@/components/ui/*` 引入，不引入 Semi Design 或新的 UI 组件库。

### 布局和交互

- API Keys 迁移入口放在当前批量操作工具栏中，和复制、删除保持一致的尺寸、间距、图标化交互和 tooltip 说明。
- Dashboard 分组/令牌筛选并入现有 Model Analytics 筛选弹窗；导出入口放在现有 action 区，不新增独立大面板。
- 公共 `/log` 页面使用 default 的页面布局、统计卡片、过滤条、表格密度和响应式规则；仅参考 classic 的数据流和字段范围。
- 移动端需要保持筛选器、弹窗和表格不溢出；长 token 名、request id、错误信息使用截断、换行或滚动容器处理。
- 文案位置、按钮层级和危险操作颜色需要与 default 已有 API Keys、Dashboard、Usage Logs 页面一致。

## 功能一：API Keys 迁移到独立账号

### 数据流

```text
API Keys 表格选择行
  -> BulkActions 点击迁移
  -> 二步 Dialog 确认
  -> POST /api/token/migrate { token_ids: number[] }
  -> 展示 results
  -> 关闭结果页后刷新 API Keys query + 清空 rowSelection
```

### 前端结构

- `web/default/src/features/keys/api.ts`
  - 增加 `migrateApiKeysToAccounts(ids: number[])`。
- `web/default/src/features/keys/types.ts`
  - 增加迁移请求/结果类型。
  - `ApiKeysDialogType` 增加 `migrate-to-accounts`，或在 bulk action 内局部管理 Dialog 状态。
- `web/default/src/features/keys/components/data-table-bulk-actions.tsx`
  - 对 Root 用户显示迁移按钮。
  - 复用当前表格选中行。
- 新增 `api-keys-migrate-to-accounts-dialog.tsx`
  - Confirm step 和 Result step。
  - 结果行按 `success/failed` 区分视觉状态。

### 权限

- 前端通过 `useAuthStore().auth.user.role >= ROLE.ROOT` 控制入口。
- 后端 RootAuth 是最终边界，前端不能作为唯一权限控制。

## 功能二：Dashboard 分组/令牌筛选与导出

### 参数契约

多值参数使用重复 key：

```text
groups=default&groups=vip&token_names=a&token_names=b
```

前端不能依赖 axios 默认数组序列化；需要显式构造 `URLSearchParams` 或使用 `paramsSerializer` 保证重复 key。

### 数据源

- 分组选项：`GET /api/group/`，仅管理员需要。
- 管理员令牌选项：`GET /api/data/token-names`，使用 `name`、`username`、`group` 构造展示标签和分组联动。
- 普通用户令牌选项：`GET /api/token/?p=1&size=100`。

### 状态模型

`DashboardFilters` 扩展：

```ts
{
  start_timestamp?: Date
  end_timestamp?: Date
  time_granularity?: TimeGranularity
  username?: string
  groups?: string[]
  token_names?: string[]
}
```

令牌选项需要保留 `group` 元数据；选择分组后过滤可选令牌，同时清理已选但不再属于选中分组的令牌。

### 接入点

- `web/default/src/features/dashboard/api.ts`
  - `getUserQuotaDates`、`getUserQuotaDataByUsers` 支持 `groups` / `token_names`。
  - 新增 `exportDashboardReport`，以 blob 下载 `/api/data/export`。
- `web/default/src/features/dashboard/lib/filters.ts`
  - `buildQueryParams` 支持数组字段并保持空值清理。
- `models-filter-dialog.tsx`
  - 添加管理员分组多选和令牌多选。
- `dashboard/index.tsx`
  - Model Analytics action 区增加导出入口。
  - 将同一组筛选传给统计卡片、模型图表和用户分析中可映射的查询。

### 导出

新 UI 当前未发现 Dashboard 导出入口，因此本任务需要新增一个轻量导出 Dialog：

- 起止时间默认使用当前 Dashboard 过滤条件。
- 分组/令牌默认使用当前 Dashboard 过滤条件，但允许用户在导出弹窗内临时修改。
- 点击导出后下载后端返回的 Excel blob。

## 功能三：公共 API Key 日志查看器

### 路由

- 新增 `web/default/src/routes/log.tsx` 或等价 file route，路径为 `/log`。
- 保留 `/console/log` 当前兼容行为，但不要让它覆盖 `/log`。

### API 客户端

公共 LogViewer 不能依赖登录态全局 `api` 客户端。需要创建本地 axios/client：

```text
Authorization: Bearer <用户输入的 API Key>
Cache-Control: no-store
```

### 页面结构

- 未认证态：输入 API Key，调用 `/api/log/token/stat` 验证。
- 已认证态：
  - 顶部统计卡片。
  - 时间范围选择。
  - 模型/请求 ID/日志类型过滤。
  - 模型分布和趋势图。
  - 公共模式日志表格，隐藏或脱敏 channel、username、IP 等管理员字段。
  - “切换 Key” 清空本地状态。

### 复用策略

- 优先复用 `web/default/src/features/usage-logs` 的格式化、状态、模型 badge、日志内容渲染工具。
- 如果后台日志表格强依赖登录态/管理员列，可以为公共查看器新增单独组件，保持列定义更小。

## 功能四：渠道配置 parity 复核

### 已知状态

- default 已有 `updateChannelStatus(id, status)`，启用/禁用路径使用 `POST /api/channel/:id/status`。
- default `transformFormDataToUpdatePayload()` 当前未包含 `status`，编辑保存不会把状态带入普通更新接口。
- default `transformFormDataToCreatePayload()` 包含 `status`，创建渠道保留默认启用语义。

### 复核点

- 表单中如仍展示可编辑 `status` 字段，需要确认它是否只用于创建，或明确禁用编辑态状态修改，避免用户以为保存可改状态。
- WebSearch、Vision Assist、上游模型计费等字段必须在读取、编辑、保存后保持 JSON 未知字段不丢失。

## 风险

- Dashboard 的 `token_names` 按名称过滤，不是按 token id 过滤；同名令牌会一起命中。这是后端历史契约，前端必须通过标签显示 username/group 降低误选风险。
- 公共 LogViewer 若直接复用后台日志组件，可能泄露管理员列或引入登录态依赖，需要专门检查列定义。
- 新 UI route tree 可能是生成文件，新增 route 后需要运行项目既有生成/构建流程确认路由生效。
- 当前工作区已有未提交 classic 修复，提交时需要分开处理或明确同一提交范围，避免新 UI 任务混入未确认修复。
