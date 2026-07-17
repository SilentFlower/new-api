# Claude 主链路 Build 薄层化

## Goal

将 Claude WebSearch 模拟、渠道 WebSearch 密钥处理和 Anthropic Reasoning Effort 同步迁入独立领域文件，使 `relay/claude_handler.go` 与 `controller/channel.go` 只保留行为不变的窄调用。

## Requirements

- 保持纯 WebSearch 请求识别、官渠原生透传、本地 provider 调用、工具计费和错误行为不变。
- 保持 Channel `setting.web_search` 的 API Key 保留、清空、脱敏、校验和旧记录兼容行为不变。
- 保持 Anthropic `output_config.effort` 在透传、参数覆盖和多次重试下的日志同步行为不变。
- WebSearch provider 继续使用现有 `relay/websearch/`，不得重写 provider 协议。
- 新增同 package 领域文件承载完整实现；原文件只保留检测、调用和标准结果返回。
- 不修改渠道设置前端主表单结构；该部分由独立前端子任务处理。

## Acceptance Criteria

- [ ] `relay/claude_handler.go` 不再定义 WebSearch 模拟和 Reasoning Effort 辅助函数。
- [ ] `controller/channel.go` 不再定义 WebSearch setting 解析、密钥合并和响应脱敏实现。
- [ ] Claude 普通请求、WebSearch 原生透传/模拟、参数覆盖、透传 body 和日志行为保持不变。
- [ ] Channel 创建、更新、查询和密钥清空/保留契约保持不变。
- [ ] 相关 Relay、Controller、计费、日志和安全测试通过。

## Out Of Scope

- 视觉辅助 prepared 状态接入。
- WebSearch 前端组件拆分。
- 新 provider、新配置项或既有 WebSearch 缺陷修复。
