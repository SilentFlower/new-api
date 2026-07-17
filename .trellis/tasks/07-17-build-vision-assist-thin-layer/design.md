# 视觉辅助 Build 薄层化设计

## 1. 目标结构

### 新建文件

| 文件 | 职责 |
| --- | --- |
| `controller/relay_error_log.go` | Relay 错误日志门禁、字段组装、上下文扩展合并和数据库写入 |

### 修改的原有文件

| 文件 | 只保留的接入点 |
| --- | --- |
| `controller/relay.go` | 每次渠道重试开始时调用准备状态重置；预处理失败和渠道失败继续调用 `recordRelayErrorLog` |
| `relay/vision_assist.go` | 独占渠道请求准备状态的标记、读取和重置，并继续承载视觉辅助完整 Relay 实现 |
| `relay/compatible_handler.go` | 通过稳定 helper 判断是否已完成渠道元信息和模型映射 |
| `relay/claude_handler.go` | 通过稳定 helper 判断是否已完成渠道元信息和模型映射 |
| `relay/vision_assist_test.go` | 增加准备状态生命周期回归测试 |

## 2. 行为数据流

### 主请求重试

`controller.Relay` 在每次尝试开始时重置准备状态，选择渠道后调用 `relay.PrepareRequestForSelectedChannel`。该函数初始化渠道元信息、执行模型映射、标记准备完成，再按既有门禁执行视觉辅助。

Claude 与 OpenAI Chat handler 只查询准备状态：已准备时跳过重复的 `InitChannelMeta` 和 `ModelMappedHelper`，未准备时保持原有独立 handler 行为。

### 视觉辅助嵌套请求

切换到辅助渠道时继续快照主请求上下文，并把嵌套请求准备状态重置为未完成；辅助 handler 完成后恢复主请求上下文。准备状态 helper 只封装现有 context key，不改变快照字段或恢复顺序。

### 错误日志

视觉辅助预处理失败仍在主渠道错误处理前直接调用 `recordRelayErrorLog`，因此只落错误日志而不触发主渠道自动封禁。普通渠道错误仍由 `processChannelError` 调用同一入口。

日志门禁继续遵循：可记录错误且全局错误日志开启，或上下文包含非空 `vision_assist_failure_reason`。日志字段、脱敏、请求路径、渠道信息、亲和性 admin 信息和耗时计算全部保持不变。

## 3. 兼容性

- 不改变图片抽取、缓存、并发、重试、端点模式、失败策略或请求改写。
- 不改变渠道选择、模型映射、计费预扣/结算、重试和自动封禁顺序。
- 不改变 `ContextKeyVisionAssistPrepared` 的值语义，只收敛其读写位置。
- 不改变错误日志的写入条件、字段、脱敏或数据库接口。
- 不修改 DTO、数据库、配置或前端。

## 4. 回滚

- 将错误日志函数体恢复到 `controller/relay.go` 并删除独立文件。
- 将准备状态 helper 的调用恢复为原 context key 读写。
- 不涉及数据、配置或迁移回滚。

## 5. 上游同步复核点

- 上游 `controller.Relay` 是否调整渠道重试、预处理或错误日志调用顺序。
- 上游 Claude/OpenAI handler 是否调整 `InitChannelMeta` 或 `ModelMappedHelper` 顺序。
- `ContextKeyVisionAssistPrepared` 是否被重命名或改变语义。
- Relay 错误日志是否新增通用字段或 admin 信息。
