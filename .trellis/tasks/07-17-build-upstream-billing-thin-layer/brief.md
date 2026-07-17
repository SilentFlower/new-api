# Brief — 上游模型计费 Build 薄层化

## Goal

- 将映射后按上游模型计费的选择和冻结收敛为独立快照领域，使所有价格、结算、任务和日志路径只读取同一稳定接口。

## Scope

- 新建 `relay/common/billing_model.go` 与直接测试。
- 保留 `RelayInfo` 冻结字段的兼容名称，并从 `relay_info.go` 移出 build 专用方法。
- 将 Responses Compact 临时查价路径改为通过稳定方法保存和恢复冻结值。
- 回归普通 Relay、任务、日志、退款、表达式和 quota saturation 路径。

## Non-Goals

- 不新增计费模式、价格规则、表达式语法、配置字段或数据库字段。
- 不调整前端渠道设置。
- 不改变价格算术、预扣、结算、退款、违规费或饱和保护。

## Key Context

- `OriginModelName` 始终表示用户原始模型，`UpstreamModelName` 表示映射后的上游模型。
- `BillingModelName()` 必须优先返回价格阶段冻结值，只有未冻结时才动态解析。
- 每次渠道重试清理旧快照；当次价格成功后重新冻结。
- Compact 后缀、任务历史回退和 Alpha Search/Compact 基础模型契约必须保持不变。

## Acceptance

- 计费模型选择、冻结、清理和读取集中在独立文件并有直接测试。
- 仓内不存在冻结字段直接访问，所有消费方继续调用稳定接口。
- 映射开关、跨渠道重试、Compact 临时查价、任务和日志行为保持不变。
- 相关测试、race、vet 和差异检查通过。

## Next Step

- 激活任务后先运行治理前计费基线，再迁移方法、私有化字段、更新唯一直接调用方并完整回归。
