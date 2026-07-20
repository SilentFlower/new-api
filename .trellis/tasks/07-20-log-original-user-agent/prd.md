# 记录请求原始 User-Agent

## Goal

在 API 请求的消费日志和错误日志中持久化客户端原始 `User-Agent`，让管理员能够在日志详情中识别调用来源，同时避免向普通用户暴露额外的客户端指纹信息。

## Background

- 消费日志与错误日志最终分别通过 `model.RecordConsumeLog` 和 `model.RecordErrorLog` 写入 `logs` 表，两条链路都持有原始 `gin.Context`。
- `Log.Other` 已用于承载结构化附加信息；其中 `other.admin_info` 在普通用户查询时由 `formatUserLogs` 整体剥离，仅管理员可见。
- 登录审计日志已在 `other.user_agent` 中记录 UA，本任务不改变其既有格式或展示行为。
- Default 前端日志详情已存在 `User Agent` 国际化文案，可直接复用。
- Classic 管理员日志表通过 `web/classic/src/hooks/usage-logs/useUsageLogsData.jsx` 生成展开详情；公共 Token 日志页使用独立入口且普通用户响应已剥离 `admin_info`。
- 当前分支是长期跟随上游的 build 分支，本任务必须遵循 `.trellis/spec/guides/build-upstream-friendly-customization.md` 的薄层接入规则。

## Requirements

- API 消费日志和错误日志必须从当前入站 HTTP 请求读取 `User-Agent`。
- 应用层必须按请求头原值保存 UA，不进行浏览器/SDK 解析、大小写转换、空白裁剪、截断或其他标准化。
- UA 必须写入 `Log.Other` 的 `admin_info.user_agent`，不得新增数据库列或数据库迁移。
- 请求未携带 UA、请求上下文为空或请求对象为空时，不写入该字段，日志写入流程必须继续正常工作。
- Default 与 Classic 两套管理员日志详情都必须展示非空的 `admin_info.user_agent`。
- 两套 UI 的用户可见标签必须使用 i18n；Default 复用既有 `User Agent` 键，Classic 若缺少该键则通过其现有 i18n 工具链补齐全部已支持语言。
- 普通用户日志接口必须继续剥离整个 `admin_info`，因此不能看到新增 UA。
- 不修改既有登录日志、异步任务结算日志和管理操作审计日志的 UA 行为。
- 定制实现必须优先放入新文件；既有上游文件仅允许增加必要的导入、调用或展示入口，不得顺手重构、移动或格式化无关代码。

## Technical Notes

- “原始”指应用从 Go `net/http` 请求头获得的值；HTTP 服务端在协议解析阶段发生的规范化不属于应用可控制范围。
- UA 仅作为来源排查线索，不能作为可信身份或安全判定依据。

## Acceptance Criteria

- [ ] 带有自定义 `User-Agent` 的 API 消费请求写入日志后，`other.admin_info.user_agent` 与请求头值完全一致。
- [ ] 带有自定义 `User-Agent` 的 API 错误请求写入日志后，`other.admin_info.user_agent` 与请求头值完全一致。
- [ ] 空 UA 不会生成 `admin_info.user_agent`，也不会阻止日志写入。
- [ ] 管理员在 Default 与 Classic 日志详情中都能看到 `User Agent`；普通用户返回的日志中不包含该字段。
- [ ] SQLite、MySQL 和 PostgreSQL 无需模式变更即可兼容该字段。
- [ ] 新逻辑主要位于独立文件，`model/log.go`、Default 详情入口和 Classic 日志 hook 只保留最薄接入点。
- [ ] 相关 Go 测试、Default 类型检查/lint/build、Classic lint/build、i18n 检查和 `git diff --check` 通过。

## Out of Scope

- 按 UA 搜索、筛选或建立数据库索引。
- 解析 UA 得到浏览器、操作系统、设备或 SDK 名称。
- 将 UA 输出到 Gin 文本访问日志。
- 为异步任务的后续差额结算/退款日志跨任务生命周期保存 UA。
- 修改 Classic 公共 Token 日志页 `web/classic/src/pages/LogViewer/index.jsx`；该页面无法也不应读取管理员专属 UA。
