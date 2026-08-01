# Brief — 修复 Claude 消息审计会话归并

## Goal

- 修复 Claude Messages 连续请求因瞬态元数据或等价内容表示变化而被错误拆分会话的问题，同时保持实际审计正文、Relay 行为和其他协议会话语义不变。

## Scope

- 保持 `CaptureMessageAudit` 在请求校验后、渠道覆盖前的现有捕获时序。
- 新建 Claude 专用 service 文件，为会话推断派生不落库的语义指纹副本。
- 指纹副本移除 `cache_control`、删除 billing header 中的瞬态 `cch=` 参数，并统一字符串与纯单 `text` block 表示。
- Claude 的 exact/prefix 使用语义指纹，compressed 锚点继续使用实际保存条目的 Blob HMAC。
- `service/message_audit.go` 只增加指纹副本与 Claude 指纹构建的薄接入。
- 新增生产形态回归测试，并运行 service/model 相关测试与 diff 检查。

## Non-Goals

- 不回填、解密重算或重写上线前已拆分的历史审计记录。
- 不移动审计捕获到请求体覆盖、请求头覆盖或上游发送之后。
- 不修改 Controller、Model、DTO、数据库结构、前端、AI 重审协议、Relay 请求、渠道覆盖、计费或部署配置。
- 不按用户、时间、模型或首条文本做模糊会话归并。

## Key Context

- 生产样本显示连续 Claude 请求仅追加历史，但动态 `cch`、移动的 `cache_control` 和字符串/单文本块切换会改变当前滚动 HMAC。
- 实际审计条目必须保持原始安全过滤内容，用于加密详情、Blob 去重、消息计数和工具计数；语义规范化只作用于独立副本。
- `model/message_audit.go` 的 compressed 匹配会用 `SessionAnchorHMACs` 查询真实 Blob，因此规范化 HMAC 不能作为压缩锚点。
- 新逻辑集中在 `service/message_audit_claude_session.go` 和对应测试；原有 `service/message_audit.go` 仅保留窄分派。
- JSON 操作必须使用 `common.Marshal` / `common.Unmarshal`，测试使用 `testify/require` 与 `testify/assert`。
- 不做历史回填意味着部署后的第一条 Claude 请求可能新建一次会话，之后的新请求应稳定归并。

## Acceptance

- 连续请求仅追加消息时，即使 `cch` 变化、`cache_control` 移动或内容表示切换，仍归入同一会话并标记为 `prefix`。
- 语义完全相同的请求能够判定为 `exact`。
- 稳定 system、可见消息、Tool 输入或 Tool 结果变化时，指纹必须变化，不能错误归并。
- 实际加密记录和详情仍保留本次请求的原始安全过滤内容，Claude 压缩锚点仍能解析到实际 Blob HMAC。
- OpenAI Chat、OpenAI Responses、Gemini 和图片协议行为不变，现有 exact/prefix/compressed/new 测试与新增回归测试通过。
- 不新增数据库、前端、渠道覆盖时序或历史数据修复改动；原有文件 diff 保持最小。

## Next Step

- planning review 确认后运行 `task.py start`，进入实现路由，按 `implement.md` 完成代码、测试和质量检查。
