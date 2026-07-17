# 渠道设置前端 Build 薄层化

## Goal

将 Default 与 Classic 渠道主表单中的 WebSearch、视觉辅助、Responses Compact 和上游模型计费配置迁入独立组件与表单映射模块，主表单只保留挂载和结果合并。

## Requirements

- 保持所有现有字段名、Channel `setting` JSON 键、默认值、校验和未知字段保留行为不变。
- 保持 WebSearch API Key 已配置、替换、保留和清空交互不变。
- 保持视觉辅助全部数值边界、端点模式和 failure policy 不变。
- 保持 Compact 与上游模型计费开关行为不变。
- 保持六语言 i18n key 和用户可见文案不变。
- Default 使用现有 shadcn/Base UI 模式；Classic 保持 Semi Design 现有交互。

## Acceptance Criteria

- [ ] Default 主 drawer 不再直接渲染全部 build 配置字段。
- [ ] Classic `EditChannelModal.jsx` 不再直接渲染和转换全部 build 配置字段。
- [ ] 独立组件覆盖初始化、编辑、提交、未知字段保留和 API Key 状态测试。
- [ ] 两套前端字段 round-trip、错误显示、禁用状态和文案完全兼容。
- [ ] Bun 测试、类型检查、i18n 检查和生产构建通过。

## Out Of Scope

- 视觉重设计、新字段、新文案或后端配置语义修改。
