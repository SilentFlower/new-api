# 渠道设置前端 Build 薄层化设计

## 新建文件与职责

- `web/default/src/features/channels/lib/build-channel-settings.ts`
  - 承载 Default 渠道表单中 build 专属字段的 schema 片段、默认值、初始化解析、提交合并、跨字段校验和配置状态判断。
  - 覆盖 `setting` JSON 中 Responses Compact、上游模型计费、视觉辅助、WebSearch，以及 `settings` JSON 中上游模型检测字段。
- `web/default/src/features/channels/components/drawers/sections/build-channel-settings.tsx`
  - 承载 Default 渠道 drawer 的 build 专属 UI 字段。
  - 使用现有 React Hook Form 上下文，主 drawer 只保留组件挂载点。
- `web/classic/src/components/table/channels/modals/buildChannelSettings.js`
  - 承载 Classic 渠道表单中 build 专属字段的默认值、初始化解析、提交合并、提交前校验、临时字段清理和状态判断。
- `web/classic/src/components/table/channels/modals/BuildChannelSettings.jsx`
  - 承载 Classic 渠道 Modal 的 build 专属 UI 字段，保持 Semi Design 组件模式。

## 原有文件最薄接入点

- `web/default/src/features/channels/lib/channel-form.ts`
  - 只展开 build schema/default，并调用 build helper 完成解析、校验和 JSON 合并。
- `web/default/src/features/channels/components/drawers/channel-mutate-drawer.tsx`
  - 只挂载 `BuildChannelExtraSettingsFields` 和 `BuildChannelUpstreamModelDetectionSection`。
  - 保留非 build 字段、导航、权限和提交流程。
- `web/default/src/features/channels/lib/channel-form.test.ts`
  - 增加 build 字段 round-trip 回归，不改原有测试语义。
- `web/default/src/features/channels/lib/index.ts`
  - 导出新 build helper。
- `web/default/src/features/channels/components/drawers/sections/index.ts`
  - 导出新 build UI 组件。
- `web/classic/src/components/table/channels/modals/EditChannelModal.jsx`
  - 只展开 build 默认值、调用 build helper、挂载 build UI 组件。
  - 保留非 build 字段和原提交主流程。

## 冲突面与回滚

- 冲突面集中在两个主表单文件的 import、默认值展开、helper 调用和组件挂载点。
- 回滚方式：删除新增 build helper/UI 文件，撤销主表单的少量 import、展开和组件挂载。
- 上游同步后复核点：字段名、`setting`/`settings` JSON 键、WebSearch API Key 保留/清空语义、视觉辅助数值边界和 provider 选项。
