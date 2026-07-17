# Brief — 重构 Responses Compact 透传与基础模型计费

## Goal

- 让 V1、历史 body bridge、V2 HTTP/SSE 和 V2 WebSocket Compact 请求统一使用下游基础模型完成现有渠道选择、亲和性和计费，并通过渠道显式能力开关原样透传给 sub2api，不再依赖 `-openai-compact` 虚拟模型或价格。

## Scope

- 分发层保留现有 Compact 检测，但所有模式均以基础模型执行 Token 权限、分组、亲和性、首次选路和现有策略允许的实际上游失败重试。
- 渠道选定后检查 `responses_compact_passthrough_enabled`；关闭时返回专用不可重试配置错误，不换渠、不清除亲和性、不自动禁用、不记录为上游故障且不预扣。
- 新建独立 HTTP 透传模块，跳过 new-api 模型映射、参数覆盖、disabled fields、DTO 重组和旧本地 SSE bridge；V1 使用 Compact 路径，V2 与历史 bridge 保持 `/responses`。
- 新建独立 WebSocket Compact turn 准备模块，保持原始 frame，按基础模型预扣与结算。
- V1 JSON、V2/bridge SSE 和 WebSocket payload 原样返回；只旁路观测合法 usage 和终态，缺失或非法 usage、失败、取消、断连、不完整流均退款，不猜测 token。
- 在现有渠道 JSON 设置中新增布尔开关，无数据库迁移；Default 与 Classic 渠道编辑界面均可读取和保存，Default 补齐六种语言。
- 核心逻辑放入新文件，原有 `middleware`、`controller`、DTO 和前端大表单只做最薄接入。

## Non-Goals

- 不修改 sub2api 的账号数据库、账号调度、Compact 探测或真实 OpenAI 模型映射策略。
- 不新增 Compact 工具价格、固定调用费或独立计费体系。
- 不按 Base URL、渠道名称或渠道类型自动识别 sub2api。
- 不自动删除旧 `*-openai-compact` 模型或价格配置。
- 不改变普通 Responses、Chat Completions via Responses、Alpha Search、视觉辅助和其他 Relay 模式的映射、转换、计费或重试语义。

## Key Context

- 当前阻断点位于 `middleware/distributor.go` 的 V1/bridge 选择后缀，以及 `relay/helper/model_mapped.go` 对所有 Compact 模式生成计费后缀；新分支必须在 `controller/relay.go` 的模型映射和预扣之前截获。
- 现有渠道亲和性先命中 preferred channel，未命中才按优先级和权重选择；Compact 能力不能前置参与筛选，否则会破坏“不重选、跟随亲和性”的已确认语义。
- 历史 body bridge 必须让 sub2api 收到裸 `/responses`，由 sub2api 完成内部提升和 SSE 合成；new-api 提前改为 `/responses/compact` 会造成 JSON/SSE 协议错位。
- HTTP 新模块计划位于 `relay/responses_compact_passthrough.go`；WebSocket 新模块计划位于 `controller/responses_compact_passthrough_websocket.go`。
- 能力错误计划使用专用 `503` ErrorCode，并设置 `skipRetry` 与 `noRecordErrorLog`，避免进入重试、auto-ban 或上游错误日志。
- `ChannelSettings` 新字段默认 false，旧记录自然兼容；前端保存必须保留未知 JSON 字段。
- Build 分支最高优先级约束见 `.trellis/spec/guides/build-upstream-friendly-customization.md`。

## Acceptance

- 四种 Compact 模式在未配置任何 `*-openai-compact` 模型或价格时，均可通过开启能力的渠道到达 sub2api，并以基础模型完成权限、亲和性、预扣和结算。
- 所选渠道关闭能力时不换渠、不重试能力错误、不清亲和性、不 auto-ban、不预扣，并返回与 `model_price_error` 可区分的错误。
- V1 发往 Compact 路径；V2、WebSocket 和历史 bridge 保持 `/responses`；请求 model、未知字段、显式零值和响应 payload 不被改写。
- 只有合法 usage 才结算；异常终态和缺失/非法 usage 全部退款，不增加工具费用。
- 普通 Responses 行为保持不变；Default/Classic 均能保存开关，Default 六语言完整。
- 业务逻辑主要位于新文件，原有文件只含可逐项解释的薄接入；完成定向测试、race、前端检查、全仓 Go 检查和真实 new-api 到 sub2api 联调。

## Next Step

- 用户确认本 brief 和最新 `prd.md`、`design.md`、`implement.md` 后，运行 `task.py start` 将任务切换为 `in_progress`，再进入 `trellis-route(implement)`；在此之前不修改业务代码。
