# Brief — 渠道设置前端 Build 薄层化

## Goal

- 将 Default 与 Classic 渠道主表单中的 WebSearch、视觉辅助、Responses Compact 和上游模型计费配置迁入独立组件与表单映射模块，主表单只保留挂载和结果合并。

## Scope

- Default 侧新增 build 表单映射模块，承载 schema/default/初始化解析/提交合并/跨字段校验/状态判断。
- Default 侧新增 build drawer section，承载 WebSearch、视觉辅助、Responses Compact、上游模型计费和上游模型检测 UI。
- Classic 侧新增 build 表单 helper，承载默认值、初始化解析、提交合并、提交前校验、临时字段清理和状态判断。
- Classic 侧新增 build UI 组件，承载上游模型检测和 build 额外设置字段。
- Default 与 Classic 主表单仅保留默认值展开、helper 调用和组件挂载等薄接入点。
- 增加/扩展单测，覆盖 round-trip、未知字段保留、WebSearch API Key 保留/清空/替换、上游模型检测 settings 合并。

## Non-Goals

- 不做视觉重设计。
- 不新增字段。
- 不新增或修改用户可见文案。
- 不修改后端配置语义。

## Key Context

- 必须保持所有现有字段名、Channel `setting` JSON 键、默认值、校验和未知字段保留行为不变。
- WebSearch API Key 已配置、替换、保留和清空交互必须不变：新 Key 才提交 `api_key`，清空且无新 Key 才提交 `clear_api_key: true`。
- 视觉辅助全部数值边界、端点模式和 failure policy 必须不变。
- Compact 与上游模型计费开关行为必须不变。
- `settings` 内上游模型检测未知字段需保留，关闭检测时仍按旧逻辑清空 last detected models。
- 六语言 i18n key 和用户可见文案必须不变。
- Default 使用现有 shadcn/Base UI 模式；Classic 保持 Semi Design 现有交互。
- build 分支规范要求定制逻辑进入独立文件，旧上游文件只保留最薄接入点；回滚方式是删除新模块并撤销主表单少量 import、展开和挂载。

## Acceptance

- Default 主 drawer 不再直接渲染全部 build 配置字段。
- Classic `EditChannelModal.jsx` 不再直接渲染和转换全部 build 配置字段。
- 独立组件覆盖初始化、编辑、提交、未知字段保留和 API Key 状态测试。
- 两套前端字段 round-trip、错误显示、禁用状态和文案完全兼容。
- Bun 测试、类型检查、i18n 检查和生产构建通过；若 Classic 既有依赖问题阻断构建，需记录原因并以相关测试/eslint 覆盖本次变更路径。

## Next Step

- 按 `implement.md` 的六步完成 Default/Classic 新模块、主文件薄接入、测试补充和验证；实现后进入 check-all，再按 auto-loop commit-only 本地提交。
