# Brief — 补全 Alpha Search 与远程压缩上游透传

## Goal

- 新增 `/v1/alpha/search` 上游转发和按次计费，并修复现有 `/v1/responses/compact` 遗漏三个官方字段的问题。

## Scope

- Alpha Search 新增 RelayFormat、RelayMode、路由和最小请求校验，复用 Token 鉴权、模型限流与 `Distribute`。
- Alpha Search 从原始 body 透传未知字段和显式零值，只写入模型映射与 Param Override；保留 query。
- Alpha Search 不设渠道类型白名单；Codex 使用内部路径，Advanced Custom 使用 route，其它渠道沿用 Base URL 约定。
- Alpha Search 成功响应复制原始 JSON 和安全响应头；非 `2xx` 在提交前进入现有错误处理与重试。
- Alpha Search 请求前按 1 次现有 `web_search` 单价预扣，仅最终 `2xx` 结算，不收 token；失败退款。
- Compact 只在现有请求构造中补复制 `tools`、`reasoning`、`text`，并增加回归测试。

## Non-Goals

- 不修改 Compact 渠道 API 类型白名单、Base URL、原始 JSON 透传架构、响应解析、usage 或计费。
- 不修改普通 Responses、Responses hosted WebSearch 或 Claude WebSearch。
- 不新增 Alpha Search token 计费、独立价格配置、无 `/v1` 别名或特定中转服务逻辑。
- 不修改数据库、前端或配置格式。

## Key Context

- issue #6114 的直接原因是 `/v1/alpha/search` 未注册。
- Compact 核心链路已经实现；确定缺陷仅是 DTO 已解析的 `tools`、`reasoning`、`text` 没有复制到上游请求对象。
- Alpha Search schema 会演进，因此只解析调度、上限和计费所需字段。
- Alpha Search 是纯工具按次计费，不能伪造 token 或借用无 usage 的文本结算。
- 主要风险是 Alpha Search 响应提交、重试、预扣/退款；Compact 必须保持最小 diff。

## Acceptance

- `/v1/alpha/search` 能按模型选择渠道并转发，保留未知字段、query 和显式零值，且不泄露客户端凭证。
- Alpha Search 仅最终 `2xx` 按 1 次 WebSearch 收费，失败不收费且不虚构 token。
- Compact 将 `tools`、`reasoning`、`text` 与原有字段一并发送上游，原有渠道限制、URL、usage 和计费不变。
- 普通 Responses 和两种现有 WebSearch 链路不回归。
- 定向测试、`go test ./...`、`go vet ./...`、`git diff --check` 通过；提交标题携带 `[build]`。

## Next Step

- 用户确认 planning artifacts 与本 brief 后，运行 `task.py start`，再通过 `trellis-route(implement)` 进入实现。
