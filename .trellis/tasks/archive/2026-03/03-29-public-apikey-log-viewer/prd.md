# 公共 API Key 使用日志查看页面

## Goal

提供一个无需登录的公共页面，用户输入 API Key 后可以查看该 Key 的使用日志和数据看板统计。解决用户在没有账号密码或不想登录的情况下，仍能查询自己 API Key 的使用情况。

## Requirements

### 页面结构
- 公共页面（路由 `/log`），无需登录即可访问
- 顶部：API Key 输入框 + 查询按钮
- 中部：数据看板（统计卡片 + 图表）
- 下部：使用日志表格（复用现有 UsageLogsTable 交互）

### 安全要求
- API Key 输入验证，错误 Key 返回错误提示，不展示任何数据
- 查询永远限定在该 Key 自身的记录，无法越权
- 频率限制防暴力枚举
- 隐藏敏感列（channel、channel_name、username、IP、retry 等管理员字段）
- 隐藏日志 `other` 字段中的 `admin_info`、`reject_reason`

### 数据看板（标准版）
**统计卡片（4 个）：**
- 使用次数（总请求数）
- 消耗额度（总 quota）
- Token 用量（总 prompt + completion tokens）
- RPM / TPM（每分钟请求数 / Token 数）

**图表（2 个）：**
- 模型调用分布饼图（各模型的调用次数占比）
- 消耗趋势折线图（按时间的额度消耗趋势）

### 日志表格
- 复用现有 UsageLogsTable 的交互模式（过滤器、分页、详情展开）
- 可用过滤器：时间范围、模型名称、日志类型、请求 ID
- 不展示的过滤器：token_name（只有一个 Key）、channel、username、group
- 分页：与现有一致

### 功能开关
- 不需要管理员开关，默认启用

## Acceptance Criteria

- [ ] 访问 `/log` 无需登录
- [ ] 未输入或输入错误 API Key 时，提示错误，不展示任何数据
- [ ] 输入正确 API Key 后，展示该 Key 的统计卡片、图表、日志列表
- [ ] 无法通过任何方式查看其他 Key 或其他用户的日志
- [ ] 敏感字段（channel、username、IP、retry）不在前端展示
- [ ] 有频率限制，防止暴力枚举
- [ ] 统计卡片展示：使用次数、消耗额度、Token 用量、RPM/TPM
- [ ] 模型分布饼图和消耗趋势折线图正常展示
- [ ] 前端构建通过，无 lint 错误

## Definition of Done

- Lint / typecheck / build 通过
- 手动测试通过（正确 Key、错误 Key、无 Key 场景）
- 无安全漏洞（越权、信息泄露）

## Technical Approach

### 后端
- 复用 `TokenAuthReadOnly` 中间件进行 Key 验证
- 新增 3 个端点（均挂在 Token 验证下）：
  - `GET /api/log/token` — 已有，日志查询（需确认是否需要扩展过滤参数）
  - `GET /api/log/token/stat` — 新增，统计数据（卡片 + 图表聚合数据）
  - 或复用/扩展已有端点
- 日志查询结果需过滤敏感字段（复用 `GetUserLogs` 的脱敏逻辑）

### 前端
- 新增页面 `web/src/pages/LogViewer/index.jsx`
- 新增公共路由 `/log`（不包裹 PrivateRoute）
- 复用 `UsageLogsTable` 组件，通过 props 控制：
  - 隐藏管理员列
  - 调整可用过滤器
  - 传入不同的 API 端点
- 复用 Dashboard 的统计卡片和图表组件/hooks

### 安全
- API Key 通过 `Authorization: Bearer sk-xxx` header 传递
- `TokenAuthReadOnly` 中间件验证 Key 有效性并设置 token_id
- 所有查询均限定 `token_id`，无法越权
- 加 `CriticalRateLimit` 防暴力枚举

## Out of Scope

- 管理员开关（不需要）
- 导出功能
- 日志删除操作
- 模型调用排行柱状图（MVP 不做）
- 模型消耗分布堆叠柱状图（MVP 不做）

## Technical Notes

- 现有 `TokenAuthReadOnly` 中间件已验证安全模型
- 现有 `GetLogByKey` controller 已实现基础日志查询
- 前端 `UsageLogsTable` 和 Dashboard hooks 结构良好，可配置化复用
- 公共路由前端只需不包裹 `PrivateRoute`，后端用 `TokenAuthReadOnly` 代替 `UserAuth`
