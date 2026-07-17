# Responses Compact 透传边界研究

## 结论

本任务应新增独立 Compact 透传实现，并在现有 HTTP/WS 主链路中加入最薄分派。渠道选择继续使用请求基础模型和现有亲和性；选定渠道后检查渠道级透传开关。开启时跳过 new-api 模型映射，但继续按请求基础模型预扣和结算。

## new-api 当前链路事实

### 渠道选择与亲和性

- `middleware/distributor.go` 先调用 `service.GetPreferredChannelByAffinity`；命中时校验渠道状态、请求路径、基础模型和分组能力，然后使用亲和渠道。
- 未命中亲和性时才调用 `service.CacheGetRandomSatisfiedChannel`，按基础模型、分组、优先级和权重选择。
- 亲和 key 包含模型、分组和命中的 session/header/body 值；Compact 使用请求基础模型才能命中普通 Responses 已建立的亲和记录。
- 因此选定渠道未开启 Compact 透传时不得改选其他渠道，否则可能把同一会话发送到不同 sub2api 实例或账号池。

### 当前 Compact 后缀与计费阻断

- `middleware/distributor.go` 当前仅对 V1 path/body bridge 的选择模型追加 `-openai-compact`。
- `relay/helper/model_mapped.go` 当前对所有 Compact 模式把 `OriginModelName` 改为映射后模型的 `-openai-compact` 计费名。
- `controller/relay.go` 在真正调用 Relay handler 前执行模型映射和 `prepareMainRelayBilling`；未配置后缀价格时，请求会在出站前以 `model_price_error` 结束。
- 新透传分支必须在调用 `ModelMappedHelper` 前截获 Compact 请求，冻结请求基础模型作为计费模型。

### 现有稳定能力

- `dto.ChannelSettings` 已承载渠道扩展 JSON，可新增 `responses_compact_passthrough_enabled`，无需数据库迁移。
- `common.BodyStorage` 可提供原始请求体，避免 DTO 重组丢失未知字段。
- Adaptor 的 `DoRequest`、`DoResponse`、Header Setup 和 URL 规则是可复用的稳定边界。
- `BillingSession`、`ModelPriceHelper`、`PostTextConsumeQuota` 和 quota checked helper 可继续用于基础模型预扣与结算，不修改其通用语义。
- `relay/channel.CopyResponsesMetadataHeaders` 已使用安全 allowlist；客户端 Authorization/Cookie 不应透传。

## sub2api 契约事实

代码位置：`/root/project/my/sub2api`。

### V1 path

- `/v1/responses/compact`、`/responses/compact` 和 `/backend-api/codex/responses/compact` 会归一为独立 Compact 入口。
- sub2api 会按 Compact 能力筛选账号，并应用账号级 `compact_model_mapping`。
- V1 上游和下游使用 unary JSON；即使 path-based 请求 body 带 `stream:true`，sub2api 也不会把它标记为历史 SSE bridge。

### 原生 V2

- 裸 `/responses` 同时满足 `stream:true`、`compaction_trigger` 和 `remote_compaction_v2` 时保持原生 `/responses` 流式协议。
- beta feature 和原始 payload 会继续发送到真实上游，不提升为 V1。

### 历史 body bridge

- 裸 `/responses` 含 `compaction_trigger`、但不满足原生 V2 信号时，sub2api 会在内部提升为 `/responses/compact`。
- 如果客户端原请求带 `stream:true`，sub2api 会标记 client stream，并把最终 unary Compact JSON 合成为 Responses SSE。
- 因此 new-api 透传历史 body bridge 时必须保持裸 `/responses` 路径和原始 body，让 sub2api 完成提升与 SSE bridge。
- 若 new-api 预先把该请求改成 `/responses/compact`，sub2api 会按 path-based V1 处理并返回 JSON，造成下游 Codex 客户端期待 SSE、实际收到 JSON 的协议错位。

## 目标数据流

### HTTP V1

```text
客户端 /v1/responses/compact + model=M
  -> new-api 按 M 和亲和性选择渠道
  -> 检查所选渠道 compact passthrough 开关
  -> 按 M 预扣
  -> 原始 body 转发到渠道 Compact URL
  -> 原始 JSON 返回，同时旁路提取 usage
  -> 按 M 结算
```

### HTTP V2

```text
客户端 /v1/responses + remote_compaction_v2 + compaction_trigger
  -> new-api 按 M 和亲和性选择渠道
  -> 检查所选渠道开关
  -> 按 M 预扣
  -> 保持 /responses、header、body 和 SSE
  -> 旁路观测 completed usage
  -> 按 M 结算
```

### 历史 body bridge

```text
客户端 /v1/responses + compaction_trigger（非 V2）
  -> new-api 保持 /responses 原样转发
  -> sub2api 提升到 /responses/compact
  -> sub2api 合成 SSE 返回
  -> new-api 原样转发 SSE并旁路结算
```

### WebSocket V2

```text
客户端 response.create + compaction_trigger
  -> 使用基础模型和现有亲和渠道
  -> 检查渠道开关
  -> 原始 frame 不做模型映射/字段重组
  -> 按基础模型预扣并从 completed usage 结算
```

## 独立模块与薄接入建议

- 新建 HTTP 透传实现文件，负责渠道开关校验、outbound RelayInfo 视图、原始 body、路径策略、响应 usage 观测和结算。
- 新建 WebSocket Compact turn 准备文件，负责原始 frame、基础模型计费快照和开关校验。
- `middleware/distributor.go` 只移除 Compact 选择后缀这一处旧行为。
- `controller/relay.go` 只增加 Compact 透传准备/执行分派，不展开业务逻辑。
- `controller/responses_websocket.go` 只增加 Compact turn 分派，不改普通 WS 路径。
- 现有 `relay/responses_handler.go`、`relay/helper/model_mapped.go` 和 suffix 配置先保留，避免扩大与上游的冲突面；新透传分支不得调用这些旧 Compact 行为。

## 风险与验证重点

- 渠道未开启开关：应在预扣前返回不可重试配置错误，不切换亲和渠道，不自动禁用渠道，不写成上游故障。
- 历史 bridge：必须验证 sub2api 收到的仍是裸 `/responses`，客户端收到合法 SSE completed 终态。
- V1/V2 响应：旁路 usage 解析不得重组或丢弃未知字段、密文和 header。
- 计费：冻结请求基础模型；sub2api 内部映射不得改变 new-api 计费模型。
- 普通 Responses：继续走现有模型映射、Param Override、disabled fields 和计费路径。
- 真实联调：必须启动 new-api 与本地 sub2api，覆盖 V1、V2 HTTP/SSE、历史 bridge 和 WebSocket。
