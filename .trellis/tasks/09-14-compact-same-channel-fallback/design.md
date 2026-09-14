# 技术设计

## 1. 边界与不变量

- 只扩展现有 HTTP 文本额度降级入口中的 Responses Compact，不改变 WebSocket、任务和 Midjourney 的入口矩阵。
- Compact 仅允许目标渠道 ID 与来源渠道 ID 相同；跨渠道配置继续返回来源预算错误。
- `OriginModelName` 表示客户端原始意图，`RoutingModelName` 表示额度降级目标，`UpstreamModelName` 表示当前渠道真实出站模型，三者不再互相覆盖。
- Compact 不进入普通转换链；原始请求只能修改顶层 `model`。
- 模型预算使用路由模型身份。普通渠道模型映射只是上游别名，不能把预算计数写到别名行。

## 2. 请求数据流

### 2.1 来源预算预检

`controller.prepareChannelLimitFallback` 不再无条件跳过 Compact。公共预算预检根据请求类型分支：

- 普通请求沿用 `InitChannelMeta -> ModelMappedHelper -> ModelPriceHelper -> CheckSelectedChannelPeriodLimits`。
- Compact 请求使用 `PrepareResponsesCompactPassthrough -> ModelPriceHelper -> CheckSelectedChannelPeriodLimits`，从而复用能力门禁和基础模型计费语义，同时不触发模型映射或请求转换。

`cloneRelayRequest` 增加 `OpenAIResponsesCompactionRequest` 的显式深拷贝。V1 Compact 当前会落到默认分支并复用原指针；目标探针修改模型时会污染正式请求，因此不能直接沿用该默认行为。

来源未超限时立即返回原渠道，后续正式 Compact 准备和原始透传完全沿用现有流程。

### 2.2 降级目标选择

来源预检返回周期额度 429 后，继续复用现有行级优先、策略级兜底和单跳保护。Compact 增加一个边界判断：选择结果的渠道 ID 必须等于来源渠道 ID，否则返回原始额度错误，不设置 `LimitFallback`。

同渠道目标继续经过 `ResolveChannelLimitFallbackTarget` 和 `SetupContextForSelectedChannel`，因此 Token 模型权限、分组能力、渠道状态和目标模型能力不会被绕过。

### 2.3 目标预算与能力预检

目标上下文建立后再次执行预算预检。此时 `RoutingModelName` 已是目标模型，Compact 分支会：

1. 用目标模型初始化探针的渠道元信息；
2. 校验同一渠道的 Compact 能力开关；
3. 按目标模型查价；
4. 检查目标模型相关的整体、个人和预算行限制。

任一步失败都在创建计费会话和调用上游前返回。`LimitFallback` 已标记本次请求，因此现有 Relay 循环不会继续第三跳。

### 2.4 原始请求模型替换

普通降级继续使用现有转换预检和原始 body 替换逻辑。Compact 降级跳过 `validateChannelLimitFallbackConversion`，直接把原始 `BodyStorage` 中的顶层 `model` 改为 `RoutingModelName`。

使用项目已有的 `sjson.SetBytes` 做局部替换，不把 body 反序列化为 map 后重新序列化。这样可以保留未知字段、显式零值、加密内容和原有字段顺序；模型字段变化是本需求唯一允许的正文差异。

### 2.5 正式 Compact 准备与计费

`relay.PrepareResponsesCompactPassthrough` 改为从 `RelayInfo.RoutingModel()` 获取本次基础模型：

- 未降级时回退到 `OriginModelName`，行为不变；
- 降级时使用 `RoutingModelName`；
- 不再重写 `OriginModelName`；
- `UpstreamModelName`、请求 DTO 和计费快照使用本次路由模型；
- `IsModelMapped` 保持 `false`。

正式预扣和结算仍走现有 BillingSession，Compact usage 解析、退款和原始响应透传均不改动。

## 3. 模型预算归属

`service.RecordRelayChannelUserQuotaUsage` 将模型名参数从 `relayInfo.OriginModelName` 改为 `relayInfo.RoutingModel()`：

- 无降级：`RoutingModel()` 返回原始模型，现有行为不变；
- 同渠道降级：目标模型预算增加，来源模型预算停止增加；
- 跨渠道普通降级：实际目标渠道和目标路由模型同时用于累计；
- 普通模型映射：仍按客户端/路由模型累计，不按上游别名累计。

渠道整体累计、个人日/周累计和不限定模型的预算行只依赖正向额度及实际渠道，不受模型名切换影响。

## 4. 错误与兼容矩阵

| 场景 | 预期行为 |
| --- | --- |
| Compact 来源未超限 | 原渠道、原模型、原始请求透传 |
| Compact 来源超限，同渠道目标可用 | 目标模型原生 Compact，按目标计费和累计 |
| Compact 来源超限，配置跨渠道目标 | 保持来源预算 429，零目标调用 |
| Compact 目标模型预算超限 | 返回目标预算错误，零上游和扣费 |
| Compact 渠道能力关闭 | 返回 `responses_compact_passthrough_disabled`，零上游和扣费 |
| Compact 目标上游失败 | 返回目标错误，不第三跳，现有退款语义不变 |
| 普通 HTTP 降级 | 出站、计费、转换不变；模型预算改记目标路由模型 |
| 无降级或普通模型映射 | 模型预算归属不变 |

## 5. 兼容性与数据

- 不新增数据库字段、迁移、配置键或 API 响应字段。
- 已有周期计数不回写。部署后来源模型卡片可能继续显示超限值，但不再因新降级请求增长；到期重置后自然归零。
- 目标模型计数从部署后的成功结算开始累计，不补记部署前历史流量。
- `relaykit/` 公共 API 不变，预计不修改该独立模块。

## 6. 文件范围

### 现有文件

- `controller/channel_limit_fallback.go`：Compact 预算预检、同渠道边界、目标预检和局部模型替换。
- `controller/relay_attempt.go`：为 V1 Compact 请求补充独立克隆，隔离来源和目标探针。
- `controller/relay_attempt_responses_test.go`：保护 Compact 请求副本的模型和原始字段隔离。
- `relay/responses_compact_passthrough.go`：以路由模型准备 Compact，同时保留原始模型。
- `service/channel_user_quota_usage.go`：按路由模型记录模型预算。
- `controller/channel_limit_fallback_compact_test.go`：三种 Compact 模式的降级与拒绝回归。
- `controller/channel_limit_fallback_model_test.go`：来源/目标模型预算归属回归。
- `controller/channel_limit_fallback_boundary_test.go`：跨渠道 Compact、WebSocket、任务等边界。
- `service/channel_model_usage_test.go`：降级目标模型与普通映射别名的预算归属回归。
- `.trellis/spec/backend/channel-period-budget-fallback.md`：在实现验证后同步额度降级与累计契约。
- `.trellis/spec/backend/relay-alpha-search-compact.md`：在实现验证后同步 Compact 同渠道降级契约。

### 新文件

- 不计划新增生产文件；变更保持在已有独立额度降级和 Compact 文件内。

## 7. 风险与回滚

- 风险：Compact 探针错误进入普通模型映射会破坏原始协议。通过独立预检分支及原始 body 断言保护。
- 风险：目标预算未预检会让降级流量绕过目标限制。通过目标模型预算耗尽的完整 Relay 测试保护。
- 风险：改用路由模型累计会影响所有现有 HTTP 降级。通过普通降级、无降级和模型映射三类计数断言保护。
- 回滚：生产代码可按三个独立点回退；已写入的周期计数不做反向搬移，等待周期重置或沿用现有人工调整能力。
- 上游友好性：不移动现有核心流程，不抽取大范围公共接口，只在已有降级分派和 Compact 准备点加入局部分支。
