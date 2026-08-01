# Implement — Claude 消息审计会话归并

## 实施清单

1. 新建 `service/message_audit_claude_session.go`。
   - 实现 Claude 指纹条目派生，非 Claude 原样返回现有条目。
   - 深拷贝 Claude 条目并递归移除协议对象上的 `cache_control`，保留 Tool `input` 内同名业务字段。
   - 仅在 system 内容中删除 billing header 的 `cch=` 参数。
   - 规范化字符串与纯单 `text` block 表示。
   - 联合生成语义前缀指纹与实际保存锚点 HMAC。
2. 薄改 `service/message_audit.go`。
   - 在 `normalizeRequest` 的正常、缩减和 metadata-only 返回路径接入指纹副本派生。
   - 在 `CaptureMessageAudit` 中仅为 Claude 调用专用指纹构建，其他协议路径保持原样。
3. 新建 `service/message_audit_claude_session_test.go`。
   - 使用生产形态连续 Claude 请求验证动态 `cch`、移动/消失的 `cache_control` 和字符串/单文本块切换仍形成 prefix。
   - 验证规范化后完全相同的请求形成相同最终指纹，可由现有 model 规则判定 exact。
   - 验证稳定 system、可见消息、Tool 输入和 Tool 结果变化会改变指纹。
   - 验证实际保存条目仍包含原始 billing header、`cache_control` 和内容表示。
   - 验证 Claude 压缩锚点使用实际保存条目的 Blob HMAC。
   - 验证 OpenAI Responses 等非 Claude 请求的指纹条目和会话指纹不变。
4. 运行格式化、定向回归和完整相关包测试，检查 diff 只包含计划文件。

## 预期改动

- 新建：`service/message_audit_claude_session.go`
- 新建：`service/message_audit_claude_session_test.go`
- 修改：`service/message_audit.go`

不修改 Controller、Model、DTO、数据库、前端、配置和部署文件。

## 验证命令

- `gofmt -w service/message_audit_claude_session.go service/message_audit_claude_session_test.go service/message_audit.go`
- `go test ./service -run 'TestMessageAuditClaude|TestMessageAuditSessionFingerprint' -count=1`
- `go test ./service ./model -count=1`
- `git diff --check`
- `git diff --stat`

若相关包测试暴露跨包回归，再补跑 `go test ./... -count=1`；不以无关历史失败掩盖本次定向结果。

## 完成判定

- 生产形态连续请求的前一完整语义指纹出现在后一请求的前缀集合中。
- 等价请求最终指纹一致，真实语义变化最终指纹不同。
- 实际加密记录和详情源条目未被指纹规范化改写。
- Claude 锚点可在实际 Blob HMAC 集合中解析，现有 compressed 合同保持有效。
- 非 Claude 协议的现有测试和会话指纹结果不变。
- 原有文件只保留必要薄接入，无无关格式化或重构。

## 回滚

删除两个 Claude 专用新文件，撤销 `service/message_audit.go` 中的指纹副本与 Claude 分派调用即可。无数据库迁移、历史数据或前端资源需要回滚。

## 上游同步复核点

同步上游后检查 `normalizeRequest` 的三个结果分支、`CaptureMessageAudit` 的指纹调用以及会话角色过滤函数是否变化；如有变化，只调整新模块入口参数和薄接入，不扩散 Claude 规则到上游主流程。
