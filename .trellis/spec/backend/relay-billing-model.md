# Relay 计费模型快照契约

> 记录渠道模型映射后的计费模型选择、价格阶段冻结、跨重试清理、结算/任务/日志读取和历史数据兼容契约。

## 场景：映射后按上游模型计费

### 1. Scope / Trigger

- Trigger: 修改 `use_upstream_model_for_billing`、模型映射后的价格查询、`RelayInfo` 计费模型字段或方法、预扣/结算/退款、任务计费上下文、消费日志模型名，或 Compact 临时查价。
- 适用范围: 普通文本、音频/实时、图片、异步任务、Alpha Search 和 Responses Compact 等读取 `RelayInfo` 计费模型的路径。
- 风险背景: `OriginModelName`、`UpstreamModelName` 和实际计费模型承担不同语义。价格阶段未冻结或调用方重新推导模型，会让预扣、结算、退款、任务和日志使用不同价格来源；跨渠道重试复用旧快照还会把前一渠道映射带入下一渠道。

### 2. Signatures

- 渠道配置与 Relay 状态：

```go
type ChannelSettings struct {
	UseUpstreamModelForBilling bool `json:"use_upstream_model_for_billing,omitempty"`
}

type RelayInfo struct {
	OriginModelName         string
	ResolvedBillingModelName string
	*ChannelMeta
}
```

- 计费模型快照接口：

```go
func (info *RelayInfo) ShouldUseUpstreamModelForBilling() bool
func (info *RelayInfo) ResolveBillingModelName() string
func (info *RelayInfo) FreezeBillingModelName(modelName string)
func (info *RelayInfo) ClearBillingModelName()
func (info *RelayInfo) FrozenBillingModelName() string
func (info *RelayInfo) BillingModelName() string
func ConsumeLogModelName(relayInfo *relaycommon.RelayInfo) string
```

- 异步任务冻结字段：

```go
type TaskBillingContext struct {
	OriginModelName  string `json:"origin_model_name,omitempty"`
	BillingModelName string `json:"billing_model_name,omitempty"`
}
```

### 3. Contracts

- 文件所有权：
  - `relay/common/billing_model.go` 独占计费模型选择、Compact 后缀处理、冻结、清理和快照读取方法。
  - `relay/common/relay_info.go` 只保留 `RelayInfo.ResolvedBillingModelName` 字段，不承载 build 专用选择逻辑或 `ratio_setting` 依赖。
  - 仓内调用方不得直接读写 `ResolvedBillingModelName`；需要保存当前冻结值时调用 `FrozenBillingModelName()`，写入或恢复时调用 `FreezeBillingModelName()`。
- 模型语义：
  - `OriginModelName` 始终表示用户请求的原始模型，不能被覆盖成计费模型。
  - `UpstreamModelName` 表示模型映射后的最终上游模型。
  - `ResolveBillingModelName()` 只解析当前渠道尝试：开关开启、`IsModelMapped=true` 且上游模型非空时使用上游模型，否则使用原始模型。
  - 原始模型带 `-openai-compact` 后缀且选择上游模型计费时，计费模型必须通过 `ratio_setting.WithCompactModelSuffix` 保留后缀。
- 冻结与读取：
  - `relay/helper/price.go` 必须先用 `ResolveBillingModelName()` 读取价格、倍率或表达式；价格计算成功后调用 `FreezeBillingModelName()`。
  - `BillingModelName()` 优先返回非空冻结值；尚未冻结时才动态解析，供价格前兼容调用。
  - 上游响应返回的实际模型名、运行时配置变化或后续字段改写不得改变已冻结值。
- 重试与临时查价：
  - 每次主渠道尝试开始必须调用 `ClearBillingModelName()`，让新渠道按自身映射和开关重新解析；成功查价后重新冻结。
  - Responses Compact 兼容路径临时重新查价时，必须用 `FrozenBillingModelName()` 保存原值，并在成功和错误返回前通过 `FreezeBillingModelName()` 恢复。
  - Compact 原始透传使用基础模型冻结计费，`use_upstream_model_for_billing` 不得改变其基础模型契约。
- 消费方：
  - 预扣、文本/音频/实时结算、工具费、违规费、BillingSession、渠道测试和日志统一调用 `BillingModelName()`，不得重新判断渠道开关或映射状态。
  - 消费日志主模型字段和消息审计 finalize 必须共用 `ConsumeLogModelName()`；该函数以 `BillingModelName()` 为输入，仅保留既有 `gpt-4-gizmo*`、`gpt-4o-gizmo*` 通配展示兼容。计费查价仍直接使用 `BillingModelName()`，不得使用展示归一化值。
  - 异步任务提交把 `BillingModelName()` 写入 `TaskBillingContext.BillingModelName`；轮询结算和日志优先读取该字段。
  - 历史任务缺少 `BillingModelName` 时，继续回退 `TaskBillingContext.OriginModelName`，再回退任务属性中的原始模型。
- 计费安全：
  - 本契约只决定价格配置键，不改变表达式、倍率、分组倍率、quota conversion、Checked saturation、预扣、结算、退款或违规费算术。
  - 冻结模型与冻结价格/表达式快照必须指向同一模型，禁止结算阶段重新按可变配置查价。

### 4. Validation & Error Matrix

| 条件 | 计费模型行为 |
| --- | --- |
| 开关关闭，模型发生映射 | 使用 `OriginModelName` |
| 开关开启，但未发生映射 | 使用 `OriginModelName` |
| 开关开启且发生映射 | 使用最终 `UpstreamModelName` |
| 上游模型为空 | 回退 `OriginModelName` |
| 原始模型带 Compact 后缀且映射 | 上游模型附加同一 Compact 后缀 |
| 价格阶段冻结后上游模型字段变化 | `BillingModelName()` 仍返回冻结值 |
| 新渠道重试开始 | 清空旧冻结值，按新渠道重新解析并冻结 |
| Compact 临时查价成功或失败 | 恢复进入临时查价前的冻结值 |
| 历史任务缺少 `billing_model_name` | 回退历史 `origin_model_name`，不得为空或改用当前配置 |
| 实际计费模型缺少价格或表达式 | 错误指向实际计费模型，不静默回退原始模型价格 |
| `gpt-4-gizmo*` 或 `gpt-4o-gizmo*` 写消费日志/消息审计 | 两处分别归一为 `gpt-4-gizmo-*` 或 `gpt-4o-gizmo-*` |
| 消息审计 finalize 未获得模型名 | 不以空值覆盖 capture 阶段的原始模型名 |

### 5. Good/Base/Bad Cases

- Good: 用户请求 `gpt-4o`，渠道映射到 `gpt-4o-mini` 且开启开关；价格、预扣、结算、任务和日志统一使用冻结的 `gpt-4o-mini`。
- Good: 第一次渠道映射到模型 A 后失败；第二次渠道开始时清空快照并映射到模型 B，最终所有费用和日志使用 B。
- Good: 上游响应把模型名改成资源路径；结算继续读取预扣阶段冻结模型，不重新查价。
- Good: 模型映射后消费日志和消息审计都调用 `ConsumeLogModelName()`，管理员按请求 ID 对照时模型名一致。
- Base: 历史渠道缺少开关字段；零值为 false，继续按原始模型计费。
- Base: 历史任务没有 `billing_model_name`；轮询结算按保存的原始模型兼容执行。
- Bad: 在 `service/quota.go`、任务或日志层重复判断 `UseUpstreamModelForBilling`，导致不同路径语义漂移。
- Bad: 结算阶段直接读取当前 `UpstreamModelName` 或重新调用价格配置，绕过预扣阶段冻结。
- Bad: Responses handler 直接保存和恢复 `ResolvedBillingModelName` 字段，使快照语义泄漏到领域外。
- Bad: 消息审计重新实现模型映射或 gizmo 判断，导致与消费日志展示漂移。

### 6. Tests Required

- `relay/common/billing_model_test.go` 直接覆盖：
  - 开关关闭、未映射、已映射、上游模型为空和 Compact 后缀。
  - 冻结值 trim、上游模型变化不污染冻结值、清理后重新动态解析。
- 价格测试覆盖普通倍率、固定价格和 tiered expression 按映射后的计费模型读取并冻结。
- 文本结算测试覆盖上游模型变化后仍使用冻结值。
- service 测试覆盖普通冻结模型和两类 gizmo 归一化，断言消费日志与消息审计使用相同结果。
- 任务测试覆盖提交时保存 `BillingModelName`、消费日志追溯字段和历史任务回退。
- 重试测试覆盖每次渠道尝试清理旧快照、成功渠道重新冻结。
- 回归命令：
  - `go test ./relay/common ./relay/helper ./relay ./service ./controller -count=1`
  - `go test -race ./relay/common ./relay/helper ./service -run 'BillingModel|MappedUpstreamModel|TaskBilling|TextQuotaSummary' -count=1`
  - `go vet ./relay/common ./relay/helper ./relay ./service ./controller`
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong

```go
originBillingModelName := info.ResolvedBillingModelName
_, err := helper.ModelPriceHelper(c, info, tokens, meta)
info.ResolvedBillingModelName = originBillingModelName
```

问题：调用方直接依赖快照字段，无法区分动态解析值与冻结值，后续改变冻结规则时容易漏改。

#### Correct

```go
originBillingModelName := info.FrozenBillingModelName()
_, err := helper.ModelPriceHelper(c, info, tokens, meta)
info.FreezeBillingModelName(originBillingModelName)
```

要求：
- 价格、结算、任务和日志读取 `BillingModelName()`。
- 消费日志展示字段与消息审计 finalize 读取 `ConsumeLogModelName()`，不得复制 gizmo 分支。
- 价格阶段解析使用 `ResolveBillingModelName()`，成功后调用 `FreezeBillingModelName()`。
- 重试开始调用 `ClearBillingModelName()`；临时保存只读 `FrozenBillingModelName()`。

## 场景：流式 client_gone 本地估算 usage 零额结算

### 1. Scope / Trigger

- Trigger: 修改文本流式 usage 生成、`PostTextConsumeQuota`、本地 token 估算、`StreamStatus`、预扣/结算、用户/渠道/token 统计、消费日志或 `quota_data` 写入。
- 风险背景: 客户端断开后，部分渠道没有可信上游 usage，会通过 `ResponseText2Usage` 等本地估算路径填充 usage。该 usage 只适合审计排查，不能作为正常消费依据，否则可能把异常中断请求记为 `type=2` 消费并更新统计。
- 适用范围: 文本类流式 relay 的统一计费收口；不得把同一判断复制到 Claude、Gemini、OpenAI 兼容等渠道 adapter。

### 2. Signatures

```go
func PostTextConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent []string)

func shouldSkipClientGoneLocalUsageBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage) bool

const ContextKeyLocalCountTokens ContextKey = "local_count_tokens"
const StreamEndReasonClientGone StreamEndReason = "client_gone"
```

审计字段：

```json
{
  "stream_status": {"status": "error", "end_reason": "client_gone"},
  "admin_info": {
    "local_count_tokens": true,
    "usage_billing_path": "local",
    "billing_skipped_reason": "client_gone_local_usage"
  }
}
```

### 3. Contracts

- 命中零额结算必须同时满足：
  - `relayInfo != nil`
  - `relayInfo.IsStream == true`
  - `relayInfo.StreamStatus != nil`
  - `relayInfo.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone`
  - `usage != nil`
  - `common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens) == true`
- 命中后必须调用 `SettleBilling(ctx, relayInfo, 0)`，让 BillingSession 通过既有生命周期完成退款或零额结算。
- 若零额结算返回错误，仅当 `relayInfo.Billing != nil && relayInfo.Billing.NeedsRefund()` 时调用 `relayInfo.Billing.Refund(ctx)` 兜底，释放仍处于预扣状态的资金。
- 若零额结算返回错误但 `NeedsRefund()` 为 false，不得调用退款兜底；此时资金阶段可能已经提交，重复退款会造成余额或额度多退。
- 命中后不得调用 `UpdateUserUsedQuotaAndRequestCount`、`UpdateChannelUsedQuota` 或任何 token 消费方向统计更新。
- 命中后不得调用 `RecordConsumeLog`，因为 `RecordConsumeLog` 会间接写 `quota_data` 消费统计。
- 若 `constant.ErrorLogEnabled` 为 true，应写一条 `type=5`、`quota=0` 的错误审计日志；若为 false，尊重现有策略，不强制写数据库错误日志。
- 错误审计日志必须把 `billing_skipped_reason=client_gone_local_usage` 放在 `other.admin_info` 下；普通用户日志视图会剥离 `admin_info` 和 `stream_status`。
- 零额结算失败时，错误审计还必须写入 `admin_info.billing_settlement_failed=true` 和 `admin_info.billing_refund_fallback_triggered=true|false`，区分退款兜底是否执行。
- 不得把所有 `client_gone` 一概免费。未设置 `ContextKeyLocalCountTokens` 的可信上游 usage 继续按现有逻辑结算。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 流式 `client_gone` 且 `local_count_tokens=true` 且 `usage != nil` | `SettleBilling(..., 0)`；不写消费日志；不更新 used quota；不写 `quota_data` |
| 上述条件且 `ErrorLogEnabled=true` | 额外写 `type=5`、`quota=0` 错误审计，`admin_info.billing_skipped_reason=client_gone_local_usage` |
| 上述条件但 `ErrorLogEnabled=false` | 不写数据库错误日志，仍零额结算且不更新统计 |
| 零额结算失败且 `Billing.NeedsRefund()=true` | 调用 `Billing.Refund(ctx)` 一次；错误审计标记结算失败和已触发退款兜底 |
| 零额结算失败且 `Billing.NeedsRefund()=false` | 不调用 `Billing.Refund(ctx)`；错误审计标记结算失败但未触发退款兜底 |
| `client_gone` 但没有本地估算标记 | 视为可信上游 usage，继续现有消费结算 |
| 本地估算 usage 但流正常结束、EOF 或 handler stop | 继续现有消费结算 |
| `usage == nil` | 走既有“上游无计费信息”路径，不由本场景改写语义 |

### 5. Good/Base/Bad Cases

- Good: Claude 流式客户端断开，未收到终态 usage，本地估算填充 usage 并设置 `local_count_tokens=true`；文本计费收口只结算 0，错误日志保留估算 token 到 `admin_info.estimated_*` 供管理员排查。
- Good: Gemini 或 OpenAI 兼容未来出现相同本地估算断开路径，不需要改 adapter，仍由 `PostTextConsumeQuota` 附近统一防护。
- Good: 零额结算在资金仍处于预扣状态时失败，`NeedsRefund()` 返回 true，统一收口调用一次 `Refund(ctx)` 并记录退款兜底审计。
- Base: 客户端断开前上游已经返回可信 usage，未设置 `local_count_tokens`；平台仍按真实 usage 结算。
- Base: 零额结算在资金已提交、后续 token 调整失败时返回错误，`NeedsRefund()` 返回 false；仅记录失败，不重复退款。
- Bad: 在 `relay/channel/claude` 里特殊判断 `client_gone` 并返回 nil usage，导致其他渠道漏防护。
- Bad: 命中零额场景后继续调用 `RecordConsumeLog` 但写 `quota=0`；这仍会污染消费日志和 `quota_data` 的请求统计口径。
- Bad: 只检查 `StreamEndReasonClientGone`，把可信上游 usage 也免费。
- Bad: 零额结算返回任意错误就无条件调用 `Refund(ctx)`，导致资金已提交后发生二次退款。

### 6. Tests Required

- 文本计费 service 测试覆盖 `client_gone + local_count_tokens`：
  - `SettleBilling` 收到 `0`。
  - `users.used_quota`、`users.request_count`、`channels.used_quota`、`tokens.used_quota/remain_quota` 不发生消费方向变化。
  - 不存在 `type=2` 消费日志，`model.CacheQuotaData` 不新增消费统计。
  - `ErrorLogEnabled=true` 时写 `type=5`、`quota=0`，且 `other.admin_info.billing_skipped_reason=client_gone_local_usage`。
- 回归测试覆盖 `client_gone + 非 local usage` 仍产生消费日志和 used quota。
- 回归测试覆盖 `local_count_tokens + 非 client_gone` 仍产生消费日志和 used quota。
- 回归测试覆盖 `ErrorLogEnabled=false` 时不强制写错误日志。
- 回归测试覆盖零额结算失败且 `NeedsRefund()=true` 时只调用一次退款，并写入 `billing_settlement_failed=true`、`billing_refund_fallback_triggered=true`。
- 回归测试覆盖零额结算失败且 `NeedsRefund()=false` 时不调用退款，并写入 `billing_settlement_failed=true`、`billing_refund_fallback_triggered=false`。
- 验证命令：
  - `go test ./service ./model -run 'ClientGone|TextQuota|ConsumeLog|QuotaData' -count=1`
  - `go test -race ./service -run 'ClientGone|TextQuotaGuard' -count=1`
  - `git diff --check`

### 7. Wrong vs Correct

#### Wrong

```go
if relayInfo.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone {
	return
}
```

问题：只按断开原因跳过会把可信上游 usage 也免费，并且没有通过 BillingSession 完成预扣退款。

#### Correct

```go
if shouldSkipClientGoneLocalUsageBilling(ctx, relayInfo, originUsage) {
	if err := SettleBilling(ctx, relayInfo, 0); err != nil &&
		relayInfo.Billing != nil && relayInfo.Billing.NeedsRefund() {
		relayInfo.Billing.Refund(ctx)
	}
	return
}
```

要求：
- 跳过条件必须同时检查流式、`client_gone`、`usage != nil` 和 `ContextKeyLocalCountTokens`。
- 零额结算必须发生在用户/渠道统计、`RecordConsumeLog` 和 tiered billing 消费结算之前。
- 零额结算错误不能被静默忽略；退款兜底必须以 `NeedsRefund()` 为资金状态门禁。
- 审计字段放入 `admin_info`，不要暴露给普通用户日志视图。
