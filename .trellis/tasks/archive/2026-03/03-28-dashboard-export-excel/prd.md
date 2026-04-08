# 数据看板：导出 Excel 报表

## Goal

在数据看板搜索按钮旁增加一个"导出"按钮，支持导出指定时间范围内以 API Key 维度的 Excel 报表，包含三个 Sheet：按 Key 汇总、按 Key+Model 明细、请求日志明细。

## 背景

管理员需要将数据看板的统计数据导出为 Excel 文件，用于线下分析和报表。导出内容以 API Key 为主维度，包含汇总统计、模型明细、请求日志三部分。

## 前置依赖

- 无。三个 Sheet 均从 `logs` 表查询，`logs` 表一直有 `token_name` 字段。

## Requirements

### 后端

1. **新增导出 API 端点**：
   - 路由：`GET /api/data/export`（管理员）
   - 查询参数：`start_timestamp`、`end_timestamp`、`username`（可选）、`token_name`（可选）
   - 返回：Excel 文件流（Content-Type: `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`）

2. **Excel 内容结构**：

   **Sheet 1：汇总统计（按 API Key）**
   - 数据来源：`logs` 表（`type = 2`），按 `token_name` 分组聚合

   | 列 | 说明 |
   |---|------|
   | API Key 名称 | token_name |
   | 所属用户 | username |
   | 请求次数 | 该 key 在时间范围内的总 count |
   | 请求 Token 数 | 该 key 的总 token_used |
   | 请求额度 | 该 key 的总 quota |

   **Sheet 2：模型明细（按 API Key + Model）**
   - 数据来源：`logs` 表（`type = 2`），按 `token_name + model_name` 分组聚合

   | 列 | 说明 |
   |---|------|
   | API Key 名称 | token_name |
   | 所属用户 | username |
   | 模型名称 | model_name |
   | 请求次数 | count |
   | 请求 Token 数 | token_used |
   | 请求额度 | quota |

   **Sheet 3：请求日志明细**
   - 数据来源：`logs` 表，`type = 2`（消费类型），按 `created_at` 升序排列

   | 列 | 说明 |
   |---|------|
   | 时间 | created_at（格式化为可读时间） |
   | 用户名 | username |
   | API Key | token_name |
   | 模型 | model_name |
   | 提示 Tokens | prompt_tokens |
   | 完成 Tokens | completion_tokens |
   | 额度消耗 | quota |
   | 耗时(s) | use_time |
   | 是否流式 | is_stream |
   | 渠道 ID | channel_id |
   | 请求 ID | request_id |

3. **Excel 生成库**：使用 `excelize`（Go 生态最成熟的 Excel 库）

### 前端

4. **导出按钮**：
   - 位置：搜索按钮（放大镜图标）旁边，刷新按钮之前或之后
   - 图标：下载图标（Download）
   - 点击行为：使用当前搜索条件（时间范围、用户名、API Key）发起导出请求
   - 下载方式：浏览器直接下载文件

5. **用户体验**：
   - 导出时按钮显示 loading 状态
   - 导出完成后自动下载
   - 导出失败时显示错误提示
   - 仅管理员可见导出按钮

## Acceptance Criteria

- [ ] 搜索按钮旁出现导出按钮（仅管理员可见）
- [ ] 点击导出按钮，下载 Excel 文件
- [ ] Excel Sheet 1 包含按 API Key 维度的汇总统计（请求次数、Token 数、额度）
- [ ] Excel Sheet 2 包含按 API Key + Model 维度的明细数据（请求次数、Token 数、额度）
- [ ] Excel Sheet 3 包含按时间升序排列的请求日志明细（含提示/完成 Tokens、耗时、流式标记等）
- [ ] 支持时间范围、用户名、API Key 筛选条件
- [ ] 导出过程中按钮显示 loading
- [ ] 文件名包含导出时间范围信息
- [ ] 代码通过 lint 检查
- [ ] SQLite / MySQL / PostgreSQL 兼容

## Definition of Done

- 代码通过 lint / build
- 手动测试：导出文件可用 Excel/WPS 正常打开
- 数据正确性验证：导出数据与看板显示一致

## Out of Scope

- 普通用户导出（本期仅管理员）
- 导出 CSV 格式（仅 Excel）
- 定时自动导出
- 导出数据量限制/分页（后续按需优化）
- 导出最大时间范围限制（暂不限制）

## Technical Notes

- 推荐 Go Excel 库：`github.com/xuri/excelize/v2`
- 后端文件：新增 controller 方法 + model 查询方法
- 前端文件：`DashboardHeader.jsx`（加导出按钮）、`useDashboardData.js`（加导出方法）
- 路由文件：`router/api-router.go`（加导出路由）
- **三个 Sheet 数据均来源于 `logs` 表（`type = 2`）**，`logs` 表一直有 `token_name` 字段，历史数据完整
- 不使用 `quota_data` 表，因为该表的 `token_name` 字段是后加的，历史记录为空
- 日志查询需注意：`logs` 表数据量大，查询需带时间范围索引，聚合查询使用 `LOG_DB`
