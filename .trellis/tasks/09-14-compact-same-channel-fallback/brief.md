# Brief — 同渠道 Responses Compact 额度降级兼容

## Goal

- 让 HTTP Responses Compact 在来源模型预算超限时使用同渠道目标模型完成原生远程压缩，并把所有 HTTP 降级请求的新增模型用量归属于实际目标模型。

## Scope

- 为 V1 `/v1/responses/compact`、历史 body bridge 和 V2 HTTP 接入同渠道模型降级。
- 来源与目标分别执行模型权限、分组能力、渠道状态、周期预算和 Compact 能力校验，保持一次降级、无第三跳。
- Compact 降级只替换原始请求顶层 `model`，继续绕过普通映射、参数覆盖、禁用字段和 DTO 重组。
- 保留客户端 `OriginModelName`，让路由、出站、计费、消费日志使用降级目标模型。
- 将所有现有 HTTP 额度降级请求的模型预算累计从原始模型改为目标路由模型。
- 补充 Compact 请求深拷贝以及普通/Compact 降级的完整回归测试。

## Non-Goals

- 不开放 Compact 跨渠道降级。
- 不实现 DeepSeek 等非原生 Compact 模型的本地摘要压缩。
- 不扩展 WebSocket Compact、任务接口或 Midjourney 的额度降级。
- 不重算或迁移当前周期已经写入的历史模型额度。
- 不修改数据库结构、管理端配置结构或前端展示。

## Key Decisions

- Compact 只有在目标解析为同一物理渠道时才放行；跨渠道配置继续返回来源预算 429。
- Compact 来源和目标预检都走原生透传准备，不进入普通模型转换链。
- 降级请求使用 `sjson.SetBytes` 局部替换顶层模型，其他原始字段和顺序不重组。
- `OriginModelName` 保留客户端意图，`RoutingModelName` 作为降级目标；出站、计费、日志和模型预算以目标路由模型为准。
- 模型预算使用路由模型而非渠道内部上游别名；无降级和普通模型映射的现有归属不变。
- 历史计数不回写：修复后来源模型停止新增，目标模型从新请求开始累计。

## Key Context

- `controller/channel_limit_fallback.go` 当前无条件跳过 Compact，需要增加原生预检、同渠道边界和原始 body 模型替换。
- `relay/responses_compact_passthrough.go` 当前强制使用原始模型，需要改为使用 `RoutingModel()` 且不覆盖 `OriginModelName`。
- `controller/relay_attempt.go` 的请求克隆当前未覆盖 `OpenAIResponsesCompactionRequest`，V1 目标预检可能污染正式请求。
- `service/channel_user_quota_usage.go` 当前按 `OriginModelName` 累计，正是 Astra 卡片降级后继续增长的原因。
- 现有 `ChannelLimitFallbackInfo`、`RoutingModel()`、BillingSession、Compact usage/退款和审计结构全部复用，不新增公开 API 或数据库字段。
- 变更遵循 build 分支上游友好约束，只在已有独立降级与 Compact 文件中加入局部分支。

## Risks / Deferred

- 模型预算归属变化覆盖所有现有 HTTP 降级；需要验证普通降级、无降级和模型映射三类行为。
- 目标模型预算今后会真实累计降级流量，并可能在达到目标上限时拒绝请求，这是预期保护。
- 当前 Astra `$1,519.45` 不会自动下降；部署后停止因新降级请求增长，并在周期重置后归零。
- Compact 本地摘要和 WebSocket 降级继续延期。

## Acceptance

- 三种 HTTP Compact 模式在同渠道目标可用时成功远程压缩，上游收到目标模型，除顶层 `model` 外请求语义不变。
- Compact 跨渠道配置仍返回来源预算 429，目标渠道零调用。
- 未超限 Compact 保持原模型、原请求和原响应透传。
- 目标预算、权限或 Compact 能力校验失败时，零上游、零扣费、零消费日志且不第三跳。
- Compact 降级成功只按目标模型计费一次，并记录完整降级审计。
- 普通及 Compact 降级后来源模型不再新增，目标路由模型按实际正向结算额度累计。
- 无降级、普通模型映射、渠道整体额度和个人日/周累计保持兼容。
- 相关 controller、relay、service 定向测试及全量 Go 测试通过。

## Next Step

- Check-All 已严格通过；下一步同步额度降级与 Compact 项目规格，再进入提交范围确认。
