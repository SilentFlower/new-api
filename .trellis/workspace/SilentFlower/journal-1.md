# Journal - SilentFlower (Part 1)

> AI development session journal
> Started: 2026-03-28

---



## Session 1: 完成后端开发规范填写

**Date**: 2026-03-28
**Task**: 完成后端开发规范填写
**Branch**: `build`

### Summary

(Add summary)

### Main Changes

基于 5 个并行研究 Agent 对代码库的深度分析，完成 `.trellis/spec/backend/` 下全部规范文件的中文填写。

| 文件 | 内容概要 |
|------|---------|
| `directory-structure.md` | 完整目录布局、模块组织规则、命名规范、路由模式 |
| `database-guidelines.md` | GORM 模型定义、查询模式、多数据库兼容变量、事务模式、迁移 |
| `error-handling.md` | NewAPIError 架构、错误传播流程、两种响应格式、重试逻辑 |
| `logging-guidelines.md` | 双层日志系统、日志级别指南、数据库审计日志、敏感数据处理 |
| `quality-guidelines.md` | 6 条禁止规则、Gin 模式、测试规范、构建流程、代码审查清单 |
| `index.md` | 更新为中文，添加开发前必读清单和核心规则速查 |

**关键特点**：所有内容基于实际代码库分析，包含真实文件路径和代码示例。


### Git Commits

| Hash | Message |
|------|---------|
| `22484f15` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 2: GitHub CI: build 分支 Docker 镜像构建工作流

**Date**: 2026-03-28
**Task**: GitHub CI: build 分支 Docker 镜像构建工作流
**Branch**: `build`

### Summary

(Add summary)

### Main Changes

## 完成内容

创建 `.github/workflows/docker-image-build.yml`，为 `build` 分支提供专用的 Docker 镜像 CI 流水线。

| 配置项 | 值 |
|--------|-----|
| 触发条件 | `build` 分支 push + `workflow_dispatch` |
| 架构 | amd64 (ubuntu-latest) + arm64 (ubuntu-24.04-arm) |
| Registry | 仅 GHCR |
| Tag 策略 | `build-{sha}` + `build-latest` |
| 签名 | 无 cosign |

**基于 `docker-image-alpha.yml` 模板**，保持 Action 版本 pin、Runner 配置、Cache 策略完全一致。

**新增文件**:
- `.github/workflows/docker-image-build.yml`


### Git Commits

| Hash | Message |
|------|---------|
| `f3d67f6a` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 3: 数据看板 API Key 维度查询 + check-impl 命令

**Date**: 2026-03-29
**Task**: 数据看板 API Key 维度查询 + check-impl 命令
**Branch**: `feature/dashboard-enhancement`

### Summary

(Add summary)

### Main Changes

## 完成内容

| 内容 | 说明 |
|------|------|
| API Key 维度查询 | quota_data 表增加 token_name 字段，查询/写入全链路支持 |
| 令牌下拉选择 | SearchModal 从文本输入改为可搜索的 Select 下拉框 |
| 历史数据查询 | 指定 token_name 时从 logs 表聚合查询，支持历史数据 |
| Modal 状态保持 | keepDOM 防止关闭时条件丢失 |
| CI 分支构建 | commit message 含 [build] 时触发任意分支构建，按分支名区分 tag |
| /trellis:check-impl | 新增实现后假设验证命令（5 个维度 + 反模式） |

## Bug 修复记录

| Bug | 原因 | 修复 |
|-----|------|------|
| 令牌下拉无数据 | API 响应是分页结构 `data.items`，误当数组解析 | 修正为 `data.items.map()` |
| 搜索条件丢失 | Semi UI Modal 默认关闭时销毁子组件 | 添加 `keepDOM={true}` |
| 令牌筛选数据为 0 | 历史 quota_data 无 token_name | 改为从 logs 表聚合查询 |
| 分页参数错误 | `p=0` 应为 `p=1` | 修正起始页码 |

## 变更文件

**后端**: `model/usedata.go`, `model/log.go`, `controller/usedata.go`
**前端**: `SearchModal.jsx`, `useDashboardData.js`, `dashboard/index.jsx`
**CI**: `.github/workflows/docker-image-build.yml`
**命令**: `.claude/commands/trellis/check-impl.md`, `.agents/skills/check-impl/SKILL.md`

## 待办

- 任务 2（导出 Excel 报表）尚未开始，PRD 已就绪
- 推送后需要部署验证修复效果


### Git Commits

| Hash | Message |
|------|---------|
| `8b3be9d0` | (see git log) |
| `c6c8b28c` | (see git log) |
| `6648b10a` | (see git log) |
| `12cf0515` | (see git log) |
| `de00c8eb` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 4: 数据看板导出 Excel 报表

**Date**: 2026-03-29
**Task**: 数据看板导出 Excel 报表
**Branch**: `feature/dashboard-enhancement`

### Summary

(Add summary)

### Main Changes

## 功能概述
实现数据看板导出 Excel 报表功能，管理员可通过弹窗选择时间范围后导出包含三个 Sheet 的 Excel 文件。

## 变更内容

| 模块 | 变更 |
|------|------|
| 后端 Model | 新增 `GetLogSummaryByKey`、`GetLogDetailByKeyModel`、`GetLogsForExport` 三个 logs 表聚合查询方法 |
| 后端 Controller | 新增 `ExportQuotaDataExcel`，生成 3-Sheet Excel（excelize 库） |
| 后端 Router | `GET /api/data/export` + AdminAuth |
| 前端 Modal | 新建 `ExportModal.jsx`，时间选择弹窗 |
| 前端 Header | 导出按钮（仅管理员可见，Download 图标） |
| 前端 Hook | `exportExcel` 方法 + blob 下载 + loading 状态 |
| 依赖 | 新增 `github.com/xuri/excelize/v2` |
| i18n | 7 个语言文件新增"导出失败"、"导出报表"翻译 |

## Excel 结构
- **Sheet 1 汇总统计**：按 API Key 聚合（请求次数、Token数、额度）
- **Sheet 2 模型明细**：按 Key 分组，每组列出模型明细 + 小计行
- **Sheet 3 请求日志**：逐条日志明细（时间、Key、模型、Tokens、额度等）

## 关键决策
- 三个 Sheet 数据均从 `logs` 表查询（而非 `quota_data`），因为 `quota_data.token_name` 是后加字段，历史数据不完整
- 日志导出设 10 万行上限防 OOM
- 导出弹窗独立于搜索弹窗，仅选择起止时间
- 所有 Sheet 移除"所属用户"列（同一用户不同 Key 场景）


### Git Commits

| Hash | Message |
|------|---------|
| `8adbf2fc` | (see git log) |
| `337270fc` | (see git log) |
| `51d28926` | (see git log) |
| `d937581d` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 5: 公共 API Key 日志查看页面

**Date**: 2026-03-29
**Task**: 公共 API Key 日志查看页面
**Branch**: `feature/dashboard-enhancement`

### Summary

(Add summary)

### Main Changes

## 功能概述

新增公共页面 `/log`，用户无需登录，输入 API Key 即可查看使用日志和数据看板。

| 模块 | 变更说明 |
|------|----------|
| 后端 Model | 新增 `GetTokenLogStat`、`GetTokenModelStats`、`GetTokenQuotaData`、`GetLogsByTokenId`、`formatTokenPublicLogs` |
| 后端 Controller | 新增 `GetLogByKeyPaged`、`GetLogStatByKey`、`GetLogChartDataByKey` |
| 后端 Router | 注册 `/api/log/token`（升级）、`/api/log/token/stat`、`/api/log/token/data` |
| 前端页面 | 新建 `web/src/pages/LogViewer/index.jsx` — API Key 输入、统计卡片、图表、日志表格 |
| 前端路由 | `/log` 公共路由（不包裹 PrivateRoute） |
| 前端布局 | `PageLayout.jsx` — `/log` 隐藏 header/footer/sidebar |

## 关键设计决策

- **安全模型**：复用 `TokenAuthReadOnly` 中间件，所有查询限定 `token_id`，敏感字段脱敏
- **频率限制**：从 CriticalRateLimit(20次/20分钟) 改为 GlobalAPIRateLimit(180次/3分钟)
- **全局时间选择器**：今天/7天/30天/自定义，统计卡片+图表+日志表格联动
- **无管理员开关**：默认启用

## 修复的问题

- RPM/TPM Scan 使用独立变量避免覆盖统计结果
- `formatTokenPublicLogs` 处理空 `Other` 字段的 nil map
- 429 Too Many Requests — 频率限制过严
- 顶部导航栏在公共页面不应显示


### Git Commits

| Hash | Message |
|------|---------|
| `d7b5f364` | (see git log) |
| `09aa2b21` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete
