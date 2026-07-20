# Brief — 记录请求原始 User-Agent

## Goal

- 在 API 消费日志和错误日志中保存应用实际收到的原始 `User-Agent`，供管理员排查调用来源，并避免向普通用户暴露客户端指纹信息。

## Scope

- 在 `model.RecordConsumeLog` 与 `model.RecordErrorLog` 序列化日志附加信息前读取入站 `User-Agent`。
- 将非空 UA 合并到 `other.admin_info.user_agent`，保留已有管理员审计字段。
- Default 与 Classic 两套管理员日志详情都展示该字段，并使用各自 i18n。
- 定制核心逻辑进入新文件，既有后端和两套前端文件只保留必要的导入、调用或组件注册点。
- 增加覆盖原值保持、空值省略、消费/错误日志写入和普通用户剥离的回归测试。

## Non-Goals

- 不新增数据库列、迁移或 UA 索引。
- 不解析、标准化、裁剪或截断 UA。
- 不增加按 UA 搜索/筛选，不写入 Gin 文本访问日志。
- 不改变登录、管理操作审计及异步任务后续结算日志的既有 UA 行为。
- 不修改 Classic 公共 Token 日志页，因为普通用户接口不会返回管理员专属 `admin_info`。

## Key Context

- 后端入口位于 `model/log.go`，`Log.Other` 是现有 JSON 扩展字段。
- `formatUserLogs` 会删除整个 `admin_info`，因此该字段天然仅管理员可见。
- “原始”以 Go `net/http` 完成协议解析后提供给应用的请求头值为准；应用代码不再做任何处理。
- 旧日志没有该可选字段，前端必须兼容缺失值；数据库结构保持不变，兼容 SQLite、MySQL、PostgreSQL 和既有 ClickHouse 日志存储。
- 遵循 build 分支薄层规范：后端新建 UA 模块，Default/Classic 新建独立展示模块；`model/log.go`、Default 详情入口和 Classic 日志 hook 只做薄接入。

## Acceptance

- 消费日志和错误日志中的 `other.admin_info.user_agent` 与自定义入站请求头值完全一致。
- 空 UA 不生成字段且不影响日志写入。
- Default 与 Classic 管理员日志详情均可见 UA，普通用户日志响应中不可见。
- Go 测试、Default 检查/构建、Classic 检查/构建、i18n 检查和 `git diff --check` 通过。

## Next Step

- 用户确认本 brief 后运行 `task.py start`，加载 backend/frontend 开发规范并进入实现与质量检查。
