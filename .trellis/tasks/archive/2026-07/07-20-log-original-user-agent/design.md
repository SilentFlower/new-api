# 记录请求原始 User-Agent - 技术设计

## Architecture and Boundaries

改动保持在现有数据库审计日志链路内：

1. 新建独立后端文件承载读取与合并 UA 的完整逻辑。
2. `model.RecordConsumeLog` 和 `model.RecordErrorLog` 在序列化 `Other` 前各保留一个薄调用点。
3. `Log.Other` 继续以 JSON 字符串写入日志库，不新增字段或迁移。
4. Default 与 Classic 各自通过独立展示模块读取 `other.admin_info.user_agent`；原有详情文件仅注册该展示入口。
5. 普通用户查询仍由 `formatUserLogs` 删除整个 `admin_info`。

## Thin-Layer File Boundaries

### New Files

- `model/log_user_agent.go`
  - 完整负责空上下文保护、读取入站 UA、创建/合并 `admin_info` 和空值省略。
- `model/log_user_agent_test.go`
  - 独立覆盖消费/错误日志持久化、原值保持、空值省略和普通用户剥离。
- `web/default/src/features/usage-logs/components/dialogs/request-user-agent-detail.tsx`
  - 完整负责 Default 管理员 UA 展示、缺失值隐藏和国际化标签。
- `web/classic/src/components/table/usage-logs/components/UserAgentDetail.jsx`
  - 完整负责构造 Classic 管理员日志的 UA 展开详情，缺失或非管理员时不返回条目。

### Existing Files and Minimal Entry Points

- `model/log.go`
  - 在消费日志与错误日志序列化 `Other` 前各增加一次 UA 合并调用；不改日志主流程语义。
- `web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx`
  - 增加独立组件导入和一个展示入口，不把 UA 业务判断铺进现有大组件。
- `web/default/src/features/usage-logs/types.ts`
  - 在既有 `admin_info` 契约中增加一个可选 `user_agent?: string`。
- `web/classic/src/hooks/usage-logs/useUsageLogsData.jsx`
  - 导入独立 UA 详情模块，并把非空结果追加到既有展开详情数组。
- Classic locale 文件
  - 仅在现有语言包缺少 `User Agent` 时通过 Classic i18n 工具链补齐，不修改其他键。

`web/classic/src/pages/LogViewer/index.jsx` 不接入该功能：它是普通用户的公共 Token 日志页，后端契约不会向其返回 `admin_info`。

## Data Contract

管理员日志响应中的新增结构：

```json
{
  "other": {
    "admin_info": {
      "user_agent": "原始入站 User-Agent"
    }
  }
}
```

- 字段类型为字符串。
- 空 UA 时字段缺失。
- 已有 `admin_info` 内容必须保留。
- 若调用方尚未创建 `admin_info`，记录逻辑负责创建该对象。

## Original Value Semantics

使用 Go HTTP 请求头读取到的 `User-Agent` 字符串，不调用 UA 解析库，不裁剪、不截断、不改写。这样保存的是应用实际收到并可用于业务判断的原始值；网络协议解析器对请求头名称和语法的基础处理无法由业务代码绕过。

## Compatibility

- 数据库存储仍使用既有 `TEXT`/字符串形式的 `Log.Other`，兼容 SQLite、MySQL 和 PostgreSQL。
- ClickHouse 日志库同样只接收既有 `Other` JSON，无需表结构变化。
- 普通用户日志兼容性由现有 `formatUserLogs` 保障。
- 旧日志没有该字段，前端通过可选字段处理，不影响展示。

## Security and Privacy

- UA 放在 `admin_info`，仅管理员日志视图可见。
- React 文本渲染沿用默认转义，不使用 HTML 注入。
- Classic React 节点同样只进行普通文本渲染，不使用 HTML 注入。
- UA 可伪造，仅用于辅助溯源。

## Rollback

删除新增的后端/前端独立模块并撤销原文件中的薄调用点即可回滚；历史日志中的 JSON 扩展字段不会影响旧代码读取。

## Upstream Sync Review Points

- 上游修改 `model.RecordConsumeLog` 或 `model.RecordErrorLog` 时，确认 UA 合并调用仍位于 `Other` 序列化之前。
- 上游修改 Default/Classic 日志详情结构时，只迁移独立 UA 展示入口，不把逻辑重新铺回大文件。
- 不为两套不同技术栈抽取跨前端共享运行时代码；它们只共享后端 JSON 数据契约和字段语义。
