# Brief — Build 薄层后续治理

## Goal

- 把 6 个剩余 build 定制厚点治理成独立文件承载完整逻辑、旧上游文件只保留最薄接入点。

## Scope

- 管理并串联 6 个子任务：RelayInfo 方法、Responses Compact 审计、Alpha Search 校验、Distributor Compact 检测、Responses handler Compact 分支、公共 Token 日志前端。
- 每个子任务独立实现、独立验证、独立回滚。

## Non-Goals

- 不新增业务能力，不重写上游主流程，不做无关格式化或 UI 改版，不提交或 push。

## Key Context

- 最高优先级规范：`.trellis/spec/guides/build-upstream-friendly-customization.md`。
- 相关契约：`.trellis/spec/backend/relay-alpha-search-compact.md`、`.trellis/spec/backend/relay-billing-model.md`。
- 前端约束：`web/default/AGENTS.md`，Default 使用 Bun、React 19、i18n。

## Acceptance

- 6 个子任务完成规划、实现和检查。
- 原上游文件 diff 仅保留必要薄接入。
- Compact、Alpha Search、计费模型和公共日志功能保持正常。

## Next Step

- 按顺序启动第一个子任务：`07-18-build-relay-info-methods-thin-layer`。
