# 上游模型计费 Build 薄层化设计

## 1. 目标结构

### 新建文件

| 文件 | 职责 |
| --- | --- |
| `relay/common/billing_model.go` | 计费模型选择、Compact 后缀处理、冻结、清理和快照读取 |
| `relay/common/billing_model_test.go` | 计费模型选择与冻结生命周期的直接契约测试 |

### 修改的原有文件

| 文件 | 只保留的接入点 |
| --- | --- |
| `relay/common/relay_info.go` | `RelayInfo` 中保留既有冻结计费模型字段，移除 build 专用方法和 `ratio_setting` import |
| `relay/responses_handler.go` | Compact 临时重新查价前后通过稳定方法读取和恢复冻结快照，不直接访问字段 |

现有 `relay/helper/price.go`、`service/quota.go`、`service/text_quota.go`、`service/task_billing.go`、日志和任务调用方继续读取 `BillingModelName()`，不增加新的判断。

## 2. 行为数据流

### 选择与冻结

渠道映射完成后，价格 helper 调用 `ResolveBillingModelName()`：开关关闭、未映射或上游模型为空时使用 `OriginModelName`；开关开启且实际映射时使用最终 `UpstreamModelName`；Compact 原始模型后缀继续附加到上游计费模型。

价格计算成功后调用 `FreezeBillingModelName()`。结算、退款、违规费、工具费、任务上下文和消费日志统一调用 `BillingModelName()`，优先读取冻结值，避免上游响应改写模型名或配置变化影响本次费用。

### 重试与临时查价

每次主渠道尝试开始时调用 `ClearBillingModelName()`，让新渠道根据自身开关和模型映射重新解析；当次价格成功后重新冻结。

Responses Compact 兼容路径临时重新查价时，先通过 `FrozenBillingModelName()` 保存原始冻结值，结束后通过 `FreezeBillingModelName()` 恢复，不能直接读写 `RelayInfo` 字段。

### 任务与历史数据

异步任务提交继续把 `BillingModelName()` 写入 `TaskBillingContext.BillingModelName`。轮询结算优先读取该字段，旧任务缺失时继续回退到 `OriginModelName`，不改变历史数据兼容。

## 3. 兼容性与安全

- 不改变 `use_upstream_model_for_billing` JSON 字段、默认值或渠道配置存储。
- 不改变价格、倍率、表达式、分组倍率、quota conversion、预扣、结算、退款、违规费和 saturation 审计。
- 不改变 `OriginModelName`、`UpstreamModelName`、日志主模型和 `billing_model_name` 的既有语义。
- 冻结字段保持既有导出名称以避免潜在 API 破坏；仓内调用方不再直接读写该字段。
- Compact 基础模型计费和 Alpha Search 工具计费继续通过现有冻结接口工作。

## 4. 回滚

- 将计费模型方法恢复到 `relay_info.go`。
- 将 Responses handler 恢复为原字段读写并删除新文件。
- 不涉及数据库、配置或价格数据回滚。

## 5. 上游同步复核点

- 上游 `RelayInfo` 字段和方法布局是否变化。
- 上游 Responses handler 是否调整 Compact 临时查价和状态恢复顺序。
- 价格 helper 是否新增冻结点或改变 `BillingModelName()` 调用契约。
- 任务上下文和消费日志是否新增计费模型来源。
