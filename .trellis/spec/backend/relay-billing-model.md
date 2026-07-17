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

### 5. Good/Base/Bad Cases

- Good: 用户请求 `gpt-4o`，渠道映射到 `gpt-4o-mini` 且开启开关；价格、预扣、结算、任务和日志统一使用冻结的 `gpt-4o-mini`。
- Good: 第一次渠道映射到模型 A 后失败；第二次渠道开始时清空快照并映射到模型 B，最终所有费用和日志使用 B。
- Good: 上游响应把模型名改成资源路径；结算继续读取预扣阶段冻结模型，不重新查价。
- Base: 历史渠道缺少开关字段；零值为 false，继续按原始模型计费。
- Base: 历史任务没有 `billing_model_name`；轮询结算按保存的原始模型兼容执行。
- Bad: 在 `service/quota.go`、任务或日志层重复判断 `UseUpstreamModelForBilling`，导致不同路径语义漂移。
- Bad: 结算阶段直接读取当前 `UpstreamModelName` 或重新调用价格配置，绕过预扣阶段冻结。
- Bad: Responses handler 直接保存和恢复 `ResolvedBillingModelName` 字段，使快照语义泄漏到领域外。

### 6. Tests Required

- `relay/common/billing_model_test.go` 直接覆盖：
  - 开关关闭、未映射、已映射、上游模型为空和 Compact 后缀。
  - 冻结值 trim、上游模型变化不污染冻结值、清理后重新动态解析。
- 价格测试覆盖普通倍率、固定价格和 tiered expression 按映射后的计费模型读取并冻结。
- 文本结算测试覆盖上游模型变化后仍使用冻结值。
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
- 价格阶段解析使用 `ResolveBillingModelName()`，成功后调用 `FreezeBillingModelName()`。
- 重试开始调用 `ClearBillingModelName()`；临时保存只读 `FrozenBillingModelName()`。
