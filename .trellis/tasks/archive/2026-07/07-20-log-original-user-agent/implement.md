# 记录请求原始 User-Agent - 实施计划

## Implementation Steps

1. 新建 `model/log_user_agent.go`，完整实现 UA 读取与 `admin_info` 合并。
2. 在 `RecordConsumeLog` 与 `RecordErrorLog` 序列化 `Other` 前各加入一个薄调用点。
3. 新建 `model/log_user_agent_test.go`，覆盖消费日志、错误日志、原值保持、空 UA 省略和普通用户剥离。
4. Default：
   - 新建 `request-user-agent-detail.tsx` 承载管理员 UA 展示。
   - 在 `LogOtherData.admin_info` 增加可选 `user_agent` 类型。
   - 在 `details-dialog.tsx` 只增加导入与组件入口，复用 `t('User Agent')`。
5. Classic：
   - 新建 `UserAgentDetail.jsx` 承载管理员 UA 展开详情构造。
   - 在 `useUsageLogsData.jsx` 只增加导入与结果追加入口。
   - 检查 `User Agent` 的 Classic locale 覆盖；缺失时通过现有 i18n 工具链补齐所有支持语言。
6. 复核 `web/classic/src/pages/LogViewer/index.jsx` 保持不变，确认公共 Token 日志不会接收管理员专属字段。

## Validation

- `gofmt` 格式化涉及的 Go 文件。
- `go test ./model ./controller -count=1`
- `go test ./service -count=1`
- `cd web/default && bun run typecheck`
- `cd web/default && bun run lint`
- `cd web/default && bun run build`
- `cd web/default && bun run i18n:sync`
- `cd web/classic && bun run eslint -- <涉及 JS/JSX 文件>`
- `cd web/classic && bun run lint -- <涉及文件>`，按 Prettier 脚本能力调整为等价定向检查。
- `cd web/classic && bun run i18n:sync`
- `cd web/classic && bun run build`
- `git diff --check`

## Risk and Rollback Points

- `Other` 可能为 `nil`，实现必须按需创建 map，不能引发 panic。
- 已有 `admin_info` 可能包含计费、渠道和饱和审计信息，合并时不得覆盖整个对象。
- 前端只在管理员上下文且字段非空时展示，避免旧日志出现空行。
- 不触碰日志表结构，回滚不需要数据库操作。
- 原有上游文件不得承载 UA 核心逻辑，不做无关格式化或重构。
- Classic 公共 Token 日志页不接入管理员字段，避免突破既有权限边界。

## Final Checks Before Start

- 确认任务 brief 与上述范围一致。
- 实施前加载 backend/frontend 对应 Trellis 规范和相关实体、DTO、组件定义。
- 完成实现后执行假设校验，确认管理员/普通用户数据流与旧日志兼容。
- 完成后检查 `git diff --stat`，逐个解释原有文件的必要薄接入点。
