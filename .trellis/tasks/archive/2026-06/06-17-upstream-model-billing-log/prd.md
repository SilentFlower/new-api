# 重定向后模型用于日志与计费

## Goal

为渠道增加一个开关：当渠道配置了 `model_mapping` 且请求模型被重定向到最终上游模型时，可选择让日志主模型与计费价格按最终上游模型计算，而不是按用户请求的原始模型计算。

示例：

- 用户请求模型：`gpt-4o`
- 渠道 `model_mapping` 最终映射到：`gpt-4o-mini`
- 开关关闭：保持现状，日志主模型与计费按 `gpt-4o`
- 开关开启：日志主模型与计费按 `gpt-4o-mini`

该能力用于让渠道按真实上游模型成本定价，同时保留原始请求模型与映射链路的可追溯信息。

## Confirmed Facts

- 当前 `ModelMappedHelper` 已支持链式 `model_mapping`，最终模型写入 `RelayInfo.UpstreamModelName`，并将上游请求体模型改为最终模型。
- 当前普通文本预扣费、结算、日志主模型主要使用 `RelayInfo.OriginModelName`。
- 当前 `GenerateTextOtherInfo` 在发生模型映射时只写入 `is_model_mapped=true` 与 `upstream_model_name`，日志主模型仍为原始模型。
- 渠道普通配置存放在 `Channel.Setting` / `dto.ChannelSettings` 中，已有 `pass_through_body_enabled`、`vision_assist` 等渠道级行为开关。
- 渠道编辑存在两套前端：`web/classic` 与 `web/default`，需要同步支持，否则默认前端无法配置该开关。
- 计费表达式系统以模型名读取表达式、冻结预扣快照，并在结算与日志显示中复用该快照；本任务若改变计费模型，也必须影响表达式计费模型名。
- 异步任务路径也会先执行 `ModelMappedHelper`，但当前 `relay_task.go` 在价格计算前把 `OriginModelName` 又恢复为原始 `modelName`。

## Requirements

- 新增渠道级布尔开关，默认关闭，历史渠道与未配置渠道必须保持现有行为。
- 开关仅在“实际发生模型映射”时生效；未发生映射时，日志与计费不应变化。
- 开关开启且实际发生模型映射时，计费用模型应为最终上游模型，包括：
  - 普通价格计费 `model_price`
  - 模型倍率计费 `model_ratio` 及补全、缓存、图片、音频相关倍率
  - 分层表达式计费 `tiered_expr`
  - 异步任务类按次/按量计费 `ModelPriceHelperPerCall`
- 开关开启且实际发生模型映射时，消费日志主模型 `logs.model_name` 应显示最终上游模型。
- 日志 `other` 必须保留追溯字段：
  - `is_model_mapped=true`
  - `upstream_model_name=<最终上游模型>`
  - `origin_model_name=<用户请求原始模型>`（本任务新增）
  - 可选记录本次计费用模型，便于排查价格来源。
- 开关关闭时，不应改变现有请求、计费、日志行为。
- 两套渠道编辑 UI 都需要展示并保存该开关。
- 后端应通过结构化 `ChannelSettings` 字段读取配置，不依赖临时前端字段。
- 本次覆盖普通同步 relay、音频、实时，以及异步任务类请求，保持“渠道开关”的统一语义。

## Acceptance Criteria

- [ ] 旧渠道未设置开关时，请求 `gpt-4o` 映射到 `gpt-4o-mini` 后，计费与日志主模型仍按 `gpt-4o`。
- [ ] 渠道开启开关后，请求 `gpt-4o` 映射到 `gpt-4o-mini`，预扣费、结算与日志主模型按 `gpt-4o-mini`。
- [ ] 上述日志 `other` 同时包含原始模型与最终上游模型，便于追溯。
- [ ] 未发生模型映射时，即使开关开启，计费与日志结果与现状一致。
- [ ] 价格未配置错误应指向本次实际计费用模型。
- [ ] `web/classic` 与 `web/default` 的渠道编辑页都能读取、展示、保存该开关。
- [ ] 覆盖至少一个后端单元测试，验证开关关闭/开启两种模型映射计费模型选择。
- [ ] 异步任务类请求在开关开启且发生映射时，按最终上游模型读取价格并记录日志主模型。

## Out of Scope

- 不改变 `model_mapping` 的解析规则、链式映射规则或循环检测规则。
- 不新增全局开关；本能力为渠道级行为。
- 不回填或迁移历史日志。
- 不改变请求实际发送给上游的模型名；上游模型仍由现有 `model_mapping` 决定。

## Notes

- 该任务会触及后端计费模型选择、日志模型选择、两套前端渠道编辑表单，建议按复杂任务处理并补充 `design.md` 与 `implement.md` 后再开始实现。
