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
| [API 契约规范](./api-contracts.md) | 管理 API 查询参数、跨层契约和边界行为 | 已完成 |
| [Relay 视觉辅助契约](./relay-vision-assist.md) | 视觉辅助请求构造、端点模式、Gemini Native 边界行为 | 已完成 |
| [Relay Alpha Search 与 Responses Compact 契约](./relay-alpha-search-compact.md) | standalone Search 透明转发，以及 Compact 基础模型选渠、原始透传、Responses WebSocket、管理端渠道测试、计费与日志隔离 | 已完成 |
| [Relay WebSearch 模拟契约](./relay-websearch-emulation.md) | Claude Code 纯 WebSearch 渠道级模拟、provider、密钥脱敏和请求体稳定性 | 已完成 |
| [Relay 计费模型快照契约](./relay-billing-model.md) | 模型映射后的计费模型选择、冻结、重试清理、任务历史兼容和日志一致性 | 已完成 |
| [Relay 非流式 JSON 响应保活契约](./relay-nonstream-keepalive.md) | JSON 空白心跳、响应提交、重试、错误体、响应头和并发 writer 生命周期 | 已完成 |
| [渠道单用户并发限制契约](./channel-user-concurrency.md) | 渠道配置、Redis/内存租约、HTTP/WebSocket/任务生命周期、429/503 错误与取消传播 | 已完成 |
| [错误处理](./error-handling.md) | 错误类型、传播流程、API 响应格式 | 已完成 |
| [代码质量标准](./quality-guidelines.md) | 禁止模式、必需模式、测试、构建流程 | 已完成 |
| [日志规范](./logging-guidelines.md) | 日志层级、格式、敏感数据处理 | 已完成 |
| [远程令牌运维 SOP](./remote-token-operations-sop.md) | 远程批量令牌创建、IP 记录修正、Cloudflare D1 同步流程 | 已完成 |

---

## 开发前必读清单

根据任务类型，在编码前**必须**阅读以下文件：

### 所有后端任务

- [代码质量标准](./quality-guidelines.md) — 禁止模式和必需模式
- [目录结构](./directory-structure.md) — 文件放置规则

### 涉及数据库操作

- [数据库规范](./database-guidelines.md) — 多数据库兼容性是核心约束

### 涉及 API 端点

- [API 契约规范](./api-contracts.md) — 查询参数、跨层传递和兼容行为
- [错误处理](./error-handling.md) — 管理 API vs 转发 API 的响应格式差异
- [日志规范](./logging-guidelines.md) — 日志级别选择和敏感数据保护

### 涉及远程令牌运维或外部 Key 同步

- [远程令牌运维 SOP](./remote-token-operations-sop.md) — 批量创建 token、quota 换算、迁移后 IP 记录、D1 同步

### 涉及 Relay 视觉辅助

- [Relay 视觉辅助契约](./relay-vision-assist.md) — 辅助请求构造、端点模式、Gemini Native 转换边界

### 涉及 Alpha Search、Responses Compact 或 Responses WebSocket

- [Relay Alpha Search 与 Responses Compact 契约](./relay-alpha-search-compact.md) — standalone Search 上游路径，以及 Compact 检测、基础模型与路径、能力门禁、Responses WebSocket、管理端渠道测试、计费和日志隔离

### 涉及 Claude Code WebSearch 模拟

- [Relay WebSearch 模拟契约](./relay-websearch-emulation.md) — 纯 WebSearch 识别、渠道配置、provider 调用、密钥脱敏、请求体稳定性和计费

### 涉及模型映射计费、预扣、结算、任务或消费日志

- [Relay 计费模型快照契约](./relay-billing-model.md) — 计费模型选择、价格阶段冻结、跨重试清理、历史任务回退和统一消费方接口

### 涉及 Relay 非流式响应保活

- [Relay 非流式 JSON 响应保活契约](./relay-nonstream-keepalive.md) — JSON 允许列表、Flush、状态码提交、渠道重试、响应头和 writer 并发清理

### 涉及渠道单用户并发、Redis 租约或 Relay 取消传播

- [渠道单用户并发限制契约](./channel-user-concurrency.md) — 配置边界、租约存储、各协议生命周期、本地错误隔离和实际网络请求 context 传播

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
