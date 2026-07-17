# 视觉辅助 Build 薄层化

## Goal

收敛视觉辅助在 Relay 生命周期、专用错误日志和各请求 handler 中的接入，使既有 `relay/vision_assist.go` 与 `service/vision_assist.go` 成为完整领域实现。

## Requirements

- 保持图片抽取、缓存、并发、重试、端点模式、失败策略和请求改写行为不变。
- 保持 OpenAI、Claude、Responses、Gemini/Vertex 的现有转换边界。
- 保持主渠道模型映射、计费、重试、auto-ban 和错误日志语义不变。
- 将视觉专用准备状态和错误日志判断迁入独立领域文件，主 Relay 只留窄生命周期调用。
- 不修改渠道设置前端主表单。

## Acceptance Criteria

- [ ] `controller/relay.go` 不再承载视觉辅助专用错误日志实现。
- [ ] 各 handler 只通过稳定入口读取视觉准备结果，不包含视觉业务逻辑。
- [ ] Chat、Claude、Responses 普通图片和工具输出图片行为保持不变。
- [ ] 辅助失败的记录、脱敏、主渠道封禁隔离和 failure policy 保持不变。
- [ ] 视觉辅助跨层测试、相关 Relay 回归和定向 race 通过。

## Out Of Scope

- 新视觉能力、识别提示词调整或 Provider 修复。
- 前端视觉配置组件拆分。
