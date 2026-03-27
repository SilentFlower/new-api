# 后端开发规范

> 本项目后端开发的最佳实践和编码规范。

---

## 概述

本项目是一个 Go 语言构建的 AI API 网关/代理，采用 Gin + GORM 技术栈，支持 40+ AI 供应商。
架构分层：Router -> Controller -> Service -> Model。

---

## 规范索引

| 规范 | 描述 | 状态 |
|------|------|------|
| [目录结构](./directory-structure.md) | 模块组织、文件布局和命名规范 | 已完成 |
| [数据库规范](./database-guidelines.md) | GORM 用法、查询模式、迁移、多数据库兼容性 | 已完成 |
| [错误处理](./error-handling.md) | 错误类型、传播流程、API 响应格式 | 已完成 |
| [代码质量标准](./quality-guidelines.md) | 禁止模式、必需模式、测试、构建流程 | 已完成 |
| [日志规范](./logging-guidelines.md) | 日志层级、格式、敏感数据处理 | 已完成 |

---

## 开发前必读清单

根据任务类型，在编码前**必须**阅读以下文件：

### 所有后端任务

- [代码质量标准](./quality-guidelines.md) — 禁止模式和必需模式
- [目录结构](./directory-structure.md) — 文件放置规则

### 涉及数据库操作

- [数据库规范](./database-guidelines.md) — 多数据库兼容性是核心约束

### 涉及 API 端点

- [错误处理](./error-handling.md) — 管理 API vs 转发 API 的响应格式差异
- [日志规范](./logging-guidelines.md) — 日志级别选择和敏感数据保护

### 涉及新渠道适配器

- [目录结构](./directory-structure.md) — `relay/channel/` 适配器组织方式
- [错误处理](./error-handling.md) — `Adaptor.DoResponse()` 错误返回规范
- [代码质量标准](./quality-guidelines.md) — StreamOptions 支持检查（规则 4）

---

## 核心规则速查

| # | 规则 | 详见 |
|---|------|------|
| 1 | JSON 操作必须用 `common.Marshal/Unmarshal` | [质量标准](./quality-guidelines.md) |
| 2 | 数据库代码必须兼容 SQLite + MySQL + PostgreSQL | [数据库规范](./database-guidelines.md) |
| 3 | 前端包管理器使用 Bun | [质量标准](./quality-guidelines.md) |
| 4 | 新渠道检查 StreamOptions 支持 | [质量标准](./quality-guidelines.md) |
| 5 | 受保护项目信息禁止修改 | [质量标准](./quality-guidelines.md) |
| 6 | 转发 DTO 可选字段用指针类型 | [质量标准](./quality-guidelines.md) |

---

**语言**: 所有文档使用**中文**编写。
