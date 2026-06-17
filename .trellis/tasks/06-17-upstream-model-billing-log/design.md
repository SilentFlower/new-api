# 技术设计

## 架构边界

本任务新增的是“渠道级计费/日志模型选择策略”，不改变实际上游请求模型映射流程。

核心原则：

- `OriginModelName` 保持表示用户请求的原始模型。
- `UpstreamModelName` 保持表示 `model_mapping` 后的最终上游模型。
- 计费和日志主模型通过统一 helper 解析，避免各路径直接判断配置。

## 配置合同

在 `dto.ChannelSettings` 中新增布尔字段：

```go
UseUpstreamModelForBilling bool `json:"use_upstream_model_for_billing,omitempty"`
```

语义：

- 缺省或 `false`：保持现有行为，按 `OriginModelName` 计费并记录日志主模型。
- `true` 且 `IsModelMapped=true` 且 `UpstreamModelName` 非空：按 `UpstreamModelName` 计费并记录日志主模型。
- `true` 但未发生模型映射：无行为差异。

该字段存储在渠道 `setting` JSON 中，不需要数据库迁移。

## 数据流

### 渠道配置保存

`web/classic` 和 `web/default` 渠道编辑表单：

1. 从 `channel.setting` JSON 读取 `use_upstream_model_for_billing`。
2. 在“额外设置”中展示开关。
3. 保存时写回 `setting` JSON。

后端通过 `Channel.GetSetting()` 解析到 `dto.ChannelSettings`，并由 `RelayInfo.InitChannelMeta()` 写入 `RelayInfo.ChannelMeta.ChannelSetting`。

### 模型映射

`ModelMappedHelper` 继续负责：

- 解析 `model_mapping`
- 设置 `IsModelMapped`
- 设置 `UpstreamModelName`
- 改写上游请求体模型

本任务不改变链式映射、循环检测和 compact 模型后缀逻辑。

### 计费模型解析

新增统一 helper，例如：

```go
func BillingModelName(info *relaycommon.RelayInfo) string
func ShouldUseUpstreamModelForBilling(info *relaycommon.RelayInfo) bool
```

解析规则：

1. 默认返回 `info.OriginModelName`。
2. 当渠道配置开启、发生模型映射、`info.UpstreamModelName` 非空时，返回 `info.UpstreamModelName`。
3. 若 `info` 为空或模型名为空，按调用方现有容错处理。

该 helper 用于所有计费和日志主模型路径。

## 后端影响点

### 预扣费与价格读取

`relay/helper/price.go`：

- `ModelPriceHelper` 使用计费模型读取：
  - `ratio_setting.GetModelPrice`
  - `billing_setting.GetBillingMode`
  - `ratio_setting.GetModelRatio`
  - 补全、缓存、图片、音频倍率
  - `billing_setting.GetBillingExpr`
- `modelPriceHelperTiered` 的快照 `ModelName` 使用计费模型，错误提示也指向计费模型。
- `ModelPriceHelperPerCall` 使用计费模型读取按次价格、默认价格和模型倍率。

### 结算与日志

`service/text_quota.go`：

- `calculateTextQuotaSummary.ModelName` 使用计费模型。
- `logs.model_name` 随 `summary.ModelName` 显示最终上游模型。
- gizmo 特殊日志归并仍基于最终日志模型处理。

`service/quota.go`：

- 实时音频和音频结算中所有模型倍率读取和日志主模型使用计费模型。

`service/task_billing.go`：

- 异步任务消费日志主模型使用计费模型。
- `TaskPricePatches` 判断需要谨慎：如果开关开启且映射发生，应按计费模型判断是否属于特殊按次计费模型，以匹配价格来源。

`relay/relay_task.go`：

- 不能在价格计算前无条件把 `OriginModelName` 恢复为原始模型后再让 price helper 读取 `OriginModelName`。
- 推荐保持 `OriginModelName` 原义不变，由 `ModelPriceHelperPerCall` 内部通过统一 helper 选择计费模型。

### 日志追溯字段

`service/log_info_generate.go` 和任务日志 other：

- 发生模型映射时继续写 `is_model_mapped=true`。
- 继续写 `upstream_model_name`。
- 新增 `origin_model_name`，保存用户请求的原始模型。
- 新增 `billing_model_name`，保存本次实际计费用模型，方便排查价格来源。

## 前端影响点

### web/classic

`EditChannelModal.jsx`：

- 初始 state 增加 `use_upstream_model_for_billing: false`。
- 读取 `setting` JSON 时填充该字段。
- 保存 `setting` JSON 时写入该字段。
- 删除临时字段时清理该字段。
- 在额外设置区域增加开关，文案建议：
  - 标签：`重定向后按上游模型计费`
  - 说明：`开启后，model_mapping 生效时日志主模型与计费价格按最终上游模型计算`

### web/default

涉及：

- `web/default/src/features/channels/types.ts`
- `web/default/src/features/channels/lib/channel-form.ts`
- `web/default/src/features/channels/components/drawers/channel-mutate-drawer.tsx`

同样需要类型、默认值、解析、保存和 UI 开关。

## 兼容性

- 不需要数据库迁移；历史 `setting` JSON 缺少字段时默认为关闭。
- 旧日志不回填。
- 价格配置缺失行为保持：如果开关开启后最终上游模型没有价格配置，应报最终上游模型价格未配置。
- 未发生映射时即使开关开启也不改变行为。

## 风险与回滚

主要风险：

- 计费模型选择散落在多条路径中，遗漏会导致预扣、结算、日志展示不一致。
- 任务类请求当前存在恢复 `OriginModelName` 的逻辑，必须通过测试覆盖。
- 前端两套表单任一遗漏会造成配置不可见或保存丢失。

回滚方式：

- 将渠道开关关闭即可恢复现有行为。
- 代码回滚不涉及数据库结构回滚。
