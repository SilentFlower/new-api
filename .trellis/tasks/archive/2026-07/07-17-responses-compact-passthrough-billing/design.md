# 技术设计 — Responses Compact 透传与基础模型计费

## 1. 设计目标

在不重构现有 Relay 主链路的前提下，为所有 Responses Compact 模式建立一条独立透传路径：现有分发器只负责按基础模型和亲和性选定渠道，新模块在渠道确定后执行能力门禁，随后使用原始请求和基础模型计费。普通 Responses 继续走原有模型映射与转换路径。

## 2. 架构边界

### 2.1 新建后端模块

| 新文件 | 职责 |
| --- | --- |
| `relay/responses_compact_passthrough.go` | HTTP Compact 能力门禁、基础模型快照、出站 `RelayInfo` 视图、原始 BodyStorage 转发、路径策略、响应原样转发、usage/终态旁路观测和结算/退款。 |
| `relay/responses_compact_passthrough_test.go` | 覆盖门禁、路径、原始 body、JSON/SSE 原样响应、usage、退款和普通 Responses 隔离。 |
| `controller/responses_compact_passthrough_websocket.go` | WebSocket Compact turn 的能力门禁、基础模型 RelayInfo 准备、原始 frame 透传和预扣入口。 |
| `controller/responses_compact_passthrough_websocket_test.go` | 覆盖首轮/后续 turn、亲和渠道不重选、原始 frame、基础模型计费和能力失败不重试。 |

新模块使用现有包而不是新增通用框架，避免导入环和不必要抽象。HTTP 协议与上游调用归 `relay`，WebSocket turn 状态与私有类型留在 `controller` 同包新文件中。

### 2.2 新建前端组件

| 新文件 | 职责 |
| --- | --- |
| `web/default/src/features/channels/components/drawers/sections/responses-compact-passthrough-field.tsx` | 使用现有 `FormField`、`Switch` 和 `useTranslation()` 渲染 Default 渠道开关。 |
| `web/classic/src/components/table/channels/modals/ResponsesCompactPassthroughSetting.jsx` | 使用 Classic 现有 `Form.Switch` 渲染同一开关。 |

两个组件只负责 UI 展示和字段交互；JSON 兼容读写继续由各主题现有渠道表单边界负责。

### 2.3 原有文件最薄接入

| 原有文件 | 最薄改动 | 必要性 |
| --- | --- | --- |
| `middleware/distributor.go` | 保留 Compact 检测和上下文写入，删除 V1/bridge 的 `-openai-compact` 选择模型改写。 | 只有分发层能保证 Token 权限、亲和性和第一次渠道选择使用基础模型。 |
| `controller/relay.go` | Compact 时跳过旧 SSE bridge；在模型映射前调用新门禁准备函数；在 Responses 分派处调用新 HTTP handler。 | 这是渠道已选定、预扣尚未发生的唯一公共边界。 |
| `controller/responses_websocket.go` | `prepareResponsesWebSocketTurnAttempt` 检测 Compact 后委托新文件，普通 turn 保持原逻辑。 | WebSocket 私有 turn 生命周期只能从该准备边界分派。 |
| `dto/channel_settings.go` | 在 `ChannelSettings` 增加 `ResponsesCompactPassthroughEnabled bool` JSON 字段。 | 渠道设置反序列化需要强类型字段，且无需数据库迁移。 |
| `web/default/src/features/channels/types.ts` | 增加 ChannelSettings 类型字段。 | 保证读取渠道 JSON 时类型完整。 |
| `web/default/src/features/channels/lib/channel-form.ts` | 在 schema、默认值、旧值解析和 JSON 序列化中接入布尔字段。 | 防止编辑渠道时开关丢失。 |
| `web/default/src/features/channels/components/drawers/channel-mutate-drawer.tsx` | 只挂载新字段组件，并把字段纳入高级设置可见性判断。 | 现有大抽屉是唯一渠道编辑入口。 |
| `web/classic/src/components/table/channels/modals/EditChannelModal.jsx` | 在默认值、解析、保存、清理中接入字段并挂载新组件。 | Classic 仍会编辑同一渠道 JSON，必须避免丢字段。 |

Default locale JSON 仅通过临时 `web/default/scripts/add-missing-keys.mjs` 写入，再执行 `bun run i18n:sync`；脚本完成后删除，不手工编辑 locale 文件。

## 3. HTTP 数据流

```text
下游 Compact 请求，model=M
  -> Distribute 使用 M 执行 Token 权限、分组、亲和性和渠道选择
  -> controller 已获得渠道上下文
  -> 新模块 InitChannelMeta 并检查 responses_compact_passthrough_enabled
       -> 关闭：返回专用 503 + skipRetry，不预扣、不 processChannelError
       -> 开启：OriginModelName/UpstreamModelName 固定为 M，不执行 ModelMappedHelper
  -> prepareMainRelayBilling 按 M 预扣
  -> 新 HTTP handler 从 BodyStorage 读取原始 body
  -> 构造仅用于出站的 RelayInfo 浅拷贝并调用所选 adaptor 的 DoRequest
  -> 原样返回 JSON/SSE，同时旁路提取合法 usage 和终态
  -> 合法 usage 按 M 结算；否则退款
```

### 3.1 出站 RelayInfo 视图

不修改 `RelayInfo` 公共结构，也不改变 `ResponsesCompactMode` 的审计事实。新模块为 `DoRequest` 构造局部浅拷贝：

- V1 path：保留 `V1Path` 和 Compact relay mode，使 adaptor 生成 `/responses/compact`。
- V2 HTTP：保留 `V2HTTP` 和 `/responses`。
- 历史 body bridge：仅在局部出站视图中使用普通 Responses 路径语义，使 adaptor 生成 `/responses`；原始 `RelayInfo` 仍保留 `V1BodyBridge` 供审计。
- 所有模式的局部视图均保持 `IsResponsesCompact()` 为真，以复用安全的 Responses metadata header allowlist。
- `OriginModelName`、`UpstreamModelName`、请求 DTO model 和原始 body model 均为 M；不读取渠道 `model_mapping`。

`DoRequest` 返回后把安全的 `UpstreamRequestURLPath` 写回原始 `RelayInfo`，用于管理员审计；不复制 URL query。

### 3.2 请求路径矩阵

| 模式 | 下游路径 | sub2api 入站路径 | 响应形态 |
| --- | --- | --- | --- |
| V1 path | `/v1/responses/compact` | Codex `/backend-api/codex/responses/compact`；其他 adaptor 既有 Compact 路径 | JSON |
| 历史 body bridge | `/v1/responses` | `/responses` | 按 sub2api 契约返回 JSON 或 SSE；stream 客户端为 SSE |
| V2 HTTP | `/v1/responses` | `/responses` | SSE |
| V2 WebSocket | `GET /v1/responses` | Responses WebSocket `/responses` | WebSocket frame |

### 3.3 响应观测

- JSON：读取原始字节，仅用 `common.Unmarshal` 解析最小 usage/错误视图；写回时使用原始字节和允许响应头，不重新 marshal。
- SSE：按读取到的原始字节顺序写入并及时 flush；旁路 observer 只解析完整 SSE `data` 事件，不改变事件名、字段、空白、未知 JSON 字段或密文。
- WebSocket：沿用现有先转发原始 payload、再解析终态和 usage 的顺序。
- 只有成功终态和完整数字 usage 才调用 `service.PostTextConsumeQuota`；不得使用输出文本估算或零值补齐替代真实 usage。
- 缺失/非法 usage、失败、取消、断连或不完整流调用 `BillingSession.Refund`，记录请求相关但不含正文的告警与 Compact 审计结局。

## 4. WebSocket 数据流

`parseResponsesWebSocketTurn` 已将 `selectionModel` 固定为基础模型。新准备函数在每个 Compact turn 中：

1. 清理上一轮 Compact 审计。
2. 解析并执行现有请求边界校验，但不重组 frame。
3. 初始化或重置 `RelayInfo`，将 `OriginModelName`、`UpstreamModelName` 和请求 model 固定为基础模型。
4. 检查当前已选渠道的透传开关；关闭时返回专用 `skipRetry` 错误。
5. 跳过 `ModelMappedHelper`、adaptor request conversion、disabled fields 和 Param Override。
6. 按基础模型调用 `prepareMainRelayBilling`，返回原始 frame 副本。

首轮渠道仍由 `SelectAndSetupChannel` 按亲和性选择；连接内后续 turn 继续使用当前渠道。Compact 开关关闭时不触发 `responsesWebSocketTurnConnector` 的换渠逻辑。只有能力门禁已通过后发生的真实上游错误，才继续服从现有 affinity/retry 规则。

## 5. 错误契约

新模块内定义专用 `types.ErrorCode` 值，例如 `responses_compact_passthrough_disabled`，不修改公共 `types/error.go` 常量表。错误使用：

- HTTP 状态：`503 Service Unavailable`。
- `skipRetry=true`。
- `noRecordErrorLog=true`，避免把本地能力配置门禁写成上游错误日志。
- 消息明确指出“已选渠道未开启 Responses Compact 透传”。
- 不使用 `channel:` 前缀，避免 `types.IsChannelError` 将配置门禁误判为真实渠道故障。
- 门禁失败发生在预扣前；HTTP 主循环不调用 `processChannelError`，WebSocket connector 也不自动禁用或重选。

真实上游网络、状态码和业务错误继续使用现有错误映射、状态码映射和重试机制。

## 6. 计费契约

- 门禁通过前 `BillingSession` 不 Reserve。
- 新准备函数不设置 `IsModelMapped`，因此 `ResolveBillingModelName()` 返回基础 `OriginModelName`；渠道的 `use_upstream_model_for_billing` 对 Compact 透传不生效。
- `ModelPriceHelper`、group ratio、cache ratio 和 quota checked helper 保持现有实现。
- 成功使用冻结的基础模型价格快照结算；sub2api 内部映射不可回写计费模型。
- 跨真实上游重试复用当前主请求 BillingSession；最终失败或无合法 usage 时退款。

## 7. 配置兼容

- `ChannelSettings` 新字段使用 `omitempty`，旧记录缺失字段时自然为 `false`。
- 保存时沿用现有 JSON 合并策略，保留未知字段。
- 不新增迁移，不修改 `Channel` 表结构。
- 旧 `*-openai-compact` 配置继续保留但不再参与新透传分支。

## 8. 冲突面与回滚

### 8.1 预期冲突面

- 高概率同步热点：`controller/relay.go`、`controller/responses_websocket.go`、两个渠道大表单。
- 控制方式：每个热点只保留一个条件分派或一个组件挂载；不移动邻近代码，不改普通路径。
- 低概率冲突：`middleware/distributor.go` 删除两行后缀改写；`dto/channel_settings.go` 增加一个字段。

### 8.2 回滚方式

1. 删除新建的 HTTP/WS 模块、测试和前端组件。
2. 撤销 `controller` 的三处薄分派与旧 bridge 条件。
3. 恢复 `middleware/distributor.go` 的后缀改写。
4. 移除 ChannelSettings 与两个前端表单字段。
5. Locale 通过同一脚本删除本任务新增键并重新执行 `bun run i18n:sync`。

不需要数据库回滚，也不修改 sub2api。

## 9. 上游同步后复核点

- `controller/relay.go` 的“选定渠道后、模型映射前、预扣前”边界是否仍存在。
- `responses_websocket.go` 是否仍以 `prepareResponsesWebSocketTurnAttempt` 作为每 turn 准备入口。
- Adaptor `GetRequestURL`、`SetupRequestHeader` 和 `DoRequest` 签名是否变化。
- `ChannelSettings` JSON 解析和前端渠道表单合并策略是否变化。
- Responses SSE/WS 终态字段或 sub2api bridge 契约是否变化。

## 10. 设计取舍

- 不把 Compact 作为分发能力标签：否则必须在亲和性前筛选渠道，会改变用户已确认的“不重选、跟随亲和性”语义。
- 不复用旧 Compact handler 主流程：旧流程会执行后缀模型、请求重组和本地 bridge，与新契约冲突。
- 不修改通用 ModelMappedHelper、Responses handler 或 BillingSession 语义：独立旁路更容易回滚，也减少上游同步冲突。
- 不新增工具计费：合法 usage 已能按基础模型完成现有文本计费，额外价格体系没有业务价值。
