# 数据看板：支持 API Key 维度查询

## Goal

在数据看板的搜索条件中增加 API Key（token_name）维度，使管理员能够按 API Key 筛选和查看数据统计。

## 背景

当前 `quota_data` 表仅按 `user_id + username + model_name + created_at(小时)` 维度聚合，无法按 API Key 粒度查看某个 key 的消耗情况。管理员需要知道哪个 key 产生了多少消耗。

## 现状分析

### 数据模型层（需改造）

- **`QuotaData` 结构体**（`model/usedata.go`）：缺少 `token_name` 字段
- **`LogQuotaData` 函数**：入参没有 `tokenName`，缓存 key 格式为 `{userId}-{username}-{modelName}-{createdAt}`
- **`RecordConsumeLog`**（`model/log.go:195-198`）：调用 `LogQuotaData` 时未传递 `tokenName`，但 `params` 中应可获取到
- **Log 表**：已有 `TokenName` 字段可参考

### 后端 API 层（需改造）

- **路由**：`GET /api/data/`（管理员）、`GET /api/data/self`（用户）
- **Controller**（`controller/usedata.go`）：仅接收 `start_timestamp`、`end_timestamp`、`username` 参数
- **查询方法**：`GetAllQuotaDates`、`GetQuotaDataByUserId` 均无 `token_name` 过滤

### 前端层（部分就绪）

- **inputs 状态**（`useDashboardData.js:42-50`）：已预留 `token_name` 字段（空字符串），但未接入 UI
- **SearchModal**（`modals/SearchModal.jsx`）：仅有起始时间、结束时间、时间粒度、用户名称四个搜索条件
- **API 调用**（`useDashboardData.js:159-194`）：URL 拼接未包含 `token_name`

## Requirements

### 后端

1. **数据模型改造**：`QuotaData` 结构体增加 `TokenName string` 字段，含索引
2. **数据写入改造**：
   - `LogQuotaData` 增加 `tokenName` 参数
   - 缓存 key 格式改为 `{userId}-{username}-{modelName}-{tokenName}-{createdAt}`
   - `RecordConsumeLog` 调用时传递 `tokenName`
3. **查询 API 改造**：
   - Controller 接收 `token_name` 查询参数
   - Model 查询方法支持按 `token_name` 过滤
   - 管理员和用户端接口均需支持
4. **数据库迁移**：确保 SQLite / MySQL / PostgreSQL 三种数据库均兼容

### 前端

5. **SearchModal 增加 API Key 搜索字段**：
   - 在"用户名称"下方（或旁边）增加"API Key"输入框
   - 管理员和普通用户均可见（普通用户按自己的 key 筛选）
6. **API 调用增加 token_name 参数**：
   - `loadQuotaData` URL 拼接包含 `token_name`

## Acceptance Criteria

- [ ] `quota_data` 表包含 `token_name` 字段
- [ ] 新产生的统计数据正确写入 `token_name`
- [ ] 管理员搜索条件弹窗中可输入 API Key 名称进行过滤
- [ ] 普通用户搜索条件弹窗中可输入 API Key 名称进行过滤
- [ ] 输入 API Key 后，统计卡片和图表仅展示该 key 的数据
- [ ] 不输入 API Key 时，行为与现有逻辑一致（向后兼容）
- [ ] SQLite / MySQL / PostgreSQL 三种数据库均通过
- [ ] 代码通过 lint 检查

## Definition of Done

- 代码通过 lint / build
- 三种数据库迁移兼容
- 手动测试通过：搜索条件输入 key 后数据正确过滤

## Decision

- **历史数据**：已有的 `quota_data` 记录 `token_name` 为空，可以接受，不做回填
- **导出最大时间范围限制**：暂不限制
- **普通用户导出**：暂不开放（仅管理员）

## Out of Scope

- API Key 下拉选择（本期使用文本输入）
- 历史数据回填（已有的 quota_data 记录 token_name 为空，不回填）
- 导出功能（在任务 2 中实现）

## Technical Notes

- 相关后端文件：`model/usedata.go`、`model/log.go`、`controller/usedata.go`
- 相关前端文件：`web/src/components/dashboard/modals/SearchModal.jsx`、`web/src/hooks/dashboard/useDashboardData.js`
- 参考 Log 表的 `TokenName` 字段定义：`model/log.go` 第 26 行
- 注意：`RecordConsumeLog` 的 `params` 结构中应包含 `TokenName`，需确认取值来源
