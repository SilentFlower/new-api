# Brief — 规范化现有 Build 特有 Feature

## Goal

- 盘点当前 build 特有功能，并以 Responses Compact / Alpha Search 为首批，在严格保持现有行为的前提下降低上游核心文件的同步冲突面。

## Scope

- 保留 `research.md` 中当前有效 build Feature 清单，后续按独立批次治理。
- 恢复 `middleware.Distribute` 接近首批功能接入前的顺序式 HTTP 分发结构，只保留 Compact detector 的窄接入。
- 新建 Responses WebSocket 专用渠道选择文件，独立承载模型权限、亲和性、首次选渠、当前渠道能力和 context 初始化。
- 将 Relay 请求快照、attempt 状态、普通计费准备/收尾移入 `controller/relay_attempt.go`。
- 将 Alpha Search 冻结工具计费准备移入 `controller/relay_alpha_search.go`。
- 先固化可观察行为测试，再执行结构迁移和完整回归。

## Non-Goals

- 不修复审计中发现的既有行为缺陷，不修改 API、选渠、计费、重试、日志或 HTTP/SSE/WS payload 语义。
- 不重写现有 `relay/responses_compact_passthrough.go`、`relay/alpha_search_handler.go` 等独立协议实现。
- 不在本批治理视觉辅助、Claude WebSearch、Dashboard、Token 迁移或其他 Feature。
- 不合并新的 `main` 提交，不修改数据库、配置格式、前端文案或受保护项目信息。
- 真实 OpenAI/sub2api 联调不是阻塞门槛。

## Key Context

- build 定制必须遵循 `.trellis/spec/guides/build-upstream-friendly-customization.md`：独立实现优先，原有上游文件只保留最薄接入。
- `middleware/distributor.go` 当前因 Responses WebSocket 共享抽取相对上游约为 `+178/-122`，是首批最大冲突面。
- `controller/relay.go` 当前包含 build 专用 attempt 与 Alpha Search 计费实现；主循环只允许删除迁出函数体和保留窄分派，不做顺手重构。
- 规划阶段相关包基线测试已全部通过，覆盖 Alpha Search、Compact HTTP/SSE/WS、渠道测试、计费、退款、审计和路由。
- 允许为减少上游冲突保留由行为测试保护的局部重复；后续同步 `main` 时必须显式复核 HTTP 与 WS 渠道选择语义。

## Acceptance

- `middleware.Distribute` 不再因 WS 复用而大面积抽取和重排，Compact 仍按基础模型检测与选渠。
- Responses WebSocket 独立渠道选择保持 Token 权限、指定渠道、亲和性、auto group、Advanced Custom 和 failover 行为。
- `controller/relay.go` 中 attempt 与 Alpha Search 计费实现迁入独立文件，核心只保留必要调用。
- 现有 Compact/Alpha 独立协议文件不被重写，所有可观察功能行为保持不变。
- 相关包回归、定向 race、`go test ./...`、`go vet ./...` 和 `git diff --check` 通过。
- 未执行真实外部联调时在交付说明中明确记录。

## Next Step

- 用户确认 planning artifacts 和本摘要后，启动任务并进入 `trellis-route(implement)` 选择实施执行方式。
