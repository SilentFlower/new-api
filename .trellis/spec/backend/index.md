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
| [部署与 CI 契约](./deployment-ci.md) | GitLab V3、Dockerfile 服务、ACK 发布、运行时环境变量和 K8s Secret 注入 | 已完成 |
| [数据库规范](./database-guidelines.md) | GORM 用法、查询模式、迁移、多数据库兼容性 | 已完成 |
| [API 契约规范](./api-contracts.md) | 管理 API 查询参数、跨层契约和边界行为 | 已完成 |
| [管理员用户账务批量摘要契约](./admin-user-billing-summary.md) | 用户钱包、订阅剩余额度、远端状态角色、统一排序与周期投影边界 | 已完成 |
| [Relay 视觉辅助契约](./relay-vision-assist.md) | 视觉辅助请求构造、端点模式、Gemini Native 边界行为 | 已完成 |
| [Relay Alpha Search 与 Responses Compact 契约](./relay-alpha-search-compact.md) | standalone Search 透明转发，以及 Compact 基础模型选渠、原始透传、Responses WebSocket、管理端渠道测试、计费与日志隔离 | 已完成 |
| [Relay WebSearch 模拟契约](./relay-websearch-emulation.md) | Claude Messages 与 Chat Completions 纯 WebSearch 渠道级模拟、provider、密钥脱敏、请求体稳定性和单次计费 | 已完成 |
| [Relay 计费模型快照契约](./relay-billing-model.md) | 模型映射后的计费模型选择、冻结、重试清理、任务历史兼容和日志一致性 | 已完成 |
| [Relay 非流式 JSON 响应保活契约](./relay-nonstream-keepalive.md) | JSON 空白心跳、响应提交、重试、错误体、响应头和并发 writer 生命周期 | 已完成 |
| [渠道单用户并发限制契约](./channel-user-concurrency.md) | 渠道配置、Redis/内存租约、HTTP/WebSocket/任务生命周期、429/503 错误与取消传播 | 已完成 |
| [渠道单用户每日额度契约](./channel-user-daily-quota.md) | 渠道每日软上限、自然日 Redis/内存状态、正向记账、管理 API、个人目标值调整与可视化 | 已完成 |
| [渠道单用户每周额度与个人覆盖契约](./channel-user-weekly-quota-and-overrides.md) | 自然周软上限、日周统一记账、并发/日限/周限个人提额、统一管理 API、缓存与 ai-fund BFF 边界 | 已完成 |
| [消息审计控制面契约](./message-audit-control-plane.md) | 消息审计跨数据库快速清空、保留水位、AI 重审 Tool 降级与上游上下文边界 | 已完成 |
| [GitHub 用户 Key 公开泄露扫描契约](./token-leak-scan.md) | 用户 Key 的公开代码搜索、精确确认、任务互斥、通知幂等、敏感信息边界和处置流程 | 已完成 |
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

### 涉及部署、CI、K8s 或运行时环境变量

- [部署与 CI 契约](./deployment-ci.md) — GitLab V3、Dockerfile 服务、ACK、RDS/Redis Secret 注入、探针和验证要求

### 涉及数据库操作

- [数据库规范](./database-guidelines.md) — 多数据库兼容性是核心约束

### 涉及 API 端点

- [API 契约规范](./api-contracts.md) — 查询参数、跨层传递和兼容行为
- [管理员用户账务批量摘要契约](./admin-user-billing-summary.md) — 批量边界、统一排序、远端事实字段和共享订阅周期投影
- [错误处理](./error-handling.md) — 管理 API vs 转发 API 的响应格式差异
- [日志规范](./logging-guidelines.md) — 日志级别选择和敏感数据保护

### 涉及远程令牌运维或外部 Key 同步

- [远程令牌运维 SOP](./remote-token-operations-sop.md) — 批量创建 token、quota 换算、迁移后 IP 记录、D1 同步

### 涉及 Relay 视觉辅助

- [Relay 视觉辅助契约](./relay-vision-assist.md) — 辅助请求构造、端点模式、Gemini Native 转换边界

### 涉及 Alpha Search、Responses Compact 或 Responses WebSocket

- [Relay Alpha Search 与 Responses Compact 契约](./relay-alpha-search-compact.md) — standalone Search 上游路径，以及 Compact 检测、基础模型与路径、能力门禁、Responses WebSocket、管理端渠道测试、计费和日志隔离

### 涉及 Claude Messages 或 Chat Completions WebSearch 模拟

- [Relay WebSearch 模拟契约](./relay-websearch-emulation.md) — 两种协议的纯 WebSearch 识别、渠道配置、provider 调用、密钥脱敏、请求体稳定性和单次计费

### 涉及模型映射计费、预扣、结算、任务或消费日志

- [Relay 计费模型快照契约](./relay-billing-model.md) — 计费模型选择、价格阶段冻结、跨重试清理、历史任务回退和统一消费方接口

### 涉及 Relay 非流式响应保活

- [Relay 非流式 JSON 响应保活契约](./relay-nonstream-keepalive.md) — JSON 允许列表、Flush、状态码提交、渠道重试、响应头和 writer 并发清理

### 涉及渠道单用户并发、Redis 租约或 Relay 取消传播

- [渠道单用户并发限制契约](./channel-user-concurrency.md) — 配置边界、租约存储、各协议生命周期、本地错误隔离和实际网络请求 context 传播

### 涉及渠道单用户每日额度、正向渠道用量或用户限制状态管理

- [渠道单用户每日额度契约](./channel-user-daily-quota.md) — 软上限、自然日状态、最终渠道快照、正向累计、个人目标值调整和渠道 Dialog 契约

### 涉及渠道单用户每周额度、个人覆盖或外部限制管理

- [渠道单用户每周额度与个人覆盖契约](./channel-user-weekly-quota-and-overrides.md) — 自然周刷新、日周统一记账、临时/永久提额、有效限制缓存、统一管理 API 和 ai-fund BFF 权威边界

### 涉及消息审计清空、AI 重审或审计 Tool

- [消息审计控制面契约](./message-audit-control-plane.md) — 跨数据库清空原子性、保留水位、固定读取范围、Tool 降级和上游上下文错误归类

### 涉及用户 Key 公开泄露扫描、GitHub Code Search 或泄露告警

- [GitHub 用户 Key 公开泄露扫描契约](./token-leak-scan.md) — 扫描范围、HMAC 锚点、公开仓库过滤、持久化状态、通知代次、Root API 和敏感信息边界

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
| 7 | ACK 生产发布配置必须通过 GitLab V3 `.ci` 声明和 K8s Secret 注入敏感运行时变量 | [部署与 CI 契约](./deployment-ci.md) |

---

**语言**: 所有文档使用**中文**编写。
