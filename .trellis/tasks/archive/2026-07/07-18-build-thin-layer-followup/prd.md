# Build 薄层后续治理

## Goal

把现有代码中仍然偏厚的 6 个 build 分支定制点治理为“独立实现 + 上游文件最薄接入”，降低后续同步上游时的冲突面，同时保证已有功能行为不变。

## Background

- 本批次来自现有代码审计，不只来自 Trellis 任务状态。
- 最高优先级规范：`.trellis/spec/guides/build-upstream-friendly-customization.md` 要求 build 定制逻辑独立，新文件承载完整职责，原上游文件只保留窄调用。
- 相关领域契约：
  - `.trellis/spec/backend/relay-alpha-search-compact.md`
  - `.trellis/spec/backend/relay-billing-model.md`
  - `web/default/AGENTS.md`
- 当前 6 个候选点：
  1. `service/log_info_generate.go:20`、`service/log_info_generate.go:54`、`service/log_info_generate.go:75` 的 Responses Compact 审计逻辑仍在通用日志生成文件中。
  2. `relay/helper/valid_request.go:176` 的 Alpha Search 请求校验仍在通用请求校验文件中。
  3. `middleware/distributor.go:422` 的 Responses Compact mode 检测仍嵌在 Distributor 主模型解析函数中。
  4. `relay/responses_handler.go:28`、`relay/responses_handler.go:148`、`relay/responses_handler.go:186` 的 Compact 分支、临时计费快照和请求转换仍在主 Responses helper 中。
  5. `relay/common/relay_info.go:278` 的 Responses Compact 状态方法仍在核心 RelayInfo 文件中；计费模型方法已经在 `relay/common/billing_model.go` 中。
  6. `web/classic/src/pages/LogViewer/index.jsx` 约 1301 行，`web/default/src/features/token-logs/index.tsx` 约 1057 行，公共 Token 日志入口仍承担过多 hook、组件、格式化和页面编排职责。

## Requirements

- R1：每个子任务必须把 build 专属或厚页面逻辑迁入领域明确的新文件，旧文件只保留最小调用、导入和接入分支。
- R2：不得为了 DRY 或美观重构上游主流程；不得做无关格式化、重命名、移动或样式整理。
- R3：公共函数签名、路由行为、错误码、计费模型、日志字段、前端可见文案和交互行为必须保持兼容。
- R4：每个子任务必须有独立回滚路径：删除新文件并撤销薄接入点即可回退。
- R5：实现顺序应降低相互干扰，先治理跨模块基础方法，再治理具体接入点，最后治理前端大页面。
- R6：所有涉及用户可见文案的前端变更必须保留 i18n；若新增或改动文案，必须同步 locale。

## Acceptance Criteria

- [ ] 6 个子任务都完成各自 `prd.md` / `design.md` / `implement.md` / `brief.md`。
- [ ] 6 个子任务均已通过对应验证命令，并记录无法全量执行时的原因与风险。
- [ ] `git diff --stat` 能体现：新增领域文件承载主要逻辑，原上游文件只出现少量薄接入修改。
- [ ] Responses Compact、Alpha Search、计费模型和公共 Token 日志已有功能保持正常。
- [ ] 没有修改受保护项目信息，也没有引入无关业务变化。

## Out of Scope

- 不新增新的 Compact、Alpha Search 或公共日志业务能力。
- 不重写 Distributor、Responses handler 或日志系统主架构。
- 不做全站 UI 改版，不替换前端组件库。
- 不提交或 push；提交动作必须另走 `trellis-push`。
