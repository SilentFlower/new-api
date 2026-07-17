# Brief — Claude 主链路 Build 薄层化

## Goal

- 将 Claude WebSearch 模拟、渠道 WebSearch 密钥处理和 Anthropic Reasoning Effort 同步迁入独立领域文件，使上游核心文件只保留行为不变的窄调用。

## Scope

- 新建独立 Relay 文件承载 WebSearch 模拟执行、Claude JSON/SSE 响应和成功计费。
- 新建独立 Relay 文件承载 Reasoning Effort 从透传 DTO 与最终参数覆盖 JSON 的同步。
- 新建独立 Controller 文件承载 Channel WebSearch setting 解析、回写、创建校验、更新密钥继承/清空和响应脱敏。
- 新建独立 DTO 文件承载 WebSearch 配置类型、常量、归一化和校验，并迁移对应 DTO 测试。
- 保持 `relay/claude_handler.go`、`controller/channel.go` 和 `dto/channel_settings.go` 中现有调用位置与顺序。

## Non-Goals

- 不修改 WebSearch provider 协议、前端渠道设置主表单、视觉辅助 prepared 状态或其他 Claude 行为。
- 不新增配置、数据库迁移、路由、Provider 或产品功能。
- 不修复结构迁移中发现的既有业务缺陷。

## Key Context

- `relay/websearch/` 已是独立 Provider 与 Claude 响应模块，本任务不重写它。
- WebSearch 本地短路必须继续发生在 converter、disabled fields、参数覆盖和上游 body 构造之前；未启用时必须原生透传。
- Channel API 响应脱敏必须使用副本，不能污染数据库中的真实 API Key；空 key 继续继承旧值，只有 `clear_api_key=true` 才清空。
- Reasoning Effort 普通路径读取参数覆盖后的最终 JSON，透传路径读取已解析 DTO；非 Anthropic 渠道不受影响。
- 治理前相关五包完整回归和 Relay 定向 race 已通过。

## Acceptance

- `relay/claude_handler.go` 不再定义 WebSearch 模拟和 Reasoning Effort 辅助函数。
- `controller/channel.go` 不再定义 WebSearch setting 与密钥处理实现。
- `dto/channel_settings.go` 只保留 `ChannelSettings.WebSearch` 字段，不再承载 WebSearch 类型和方法。
- WebSearch 模拟/透传、Provider 错误、工具计费、密钥脱敏/继承/清空及 effort 日志行为保持不变。
- 相关包回归、定向 race、vet、格式和差异检查通过，原上游文件冲突面明显下降。

## Next Step

- 用户确认三件套和本摘要后，运行 `task.py start` 激活该子任务，再进入 `trellis-route(implement)`。
