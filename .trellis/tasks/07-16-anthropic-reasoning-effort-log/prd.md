# Anthropic Reasoning Effort 日志适配

## Goal

让 Anthropic/Claude 渠道请求中明确存在的 Reasoning Effort，在现有消费日志详情中正常显示。

## Background

- 日志详情前端已经读取并展示 `other.reasoning_effort`，字段存在时不会过滤 `xhigh` 等非标准值：`web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx:997`。
- 后端只在 `relayInfo.ReasoningEffort` 非空时写入 `other.reasoning_effort`：`service/log_info_generate.go:198`。
- Claude 原生请求入口当前直接返回请求，没有从 `output_config.effort` 回填 `RelayInfo`：`relay/channel/claude/adaptor.go:28`。
- Claude 参数覆盖在请求转换和序列化之后执行；当前覆盖逻辑不会把 Anthropic `output_config.effort` 同步到日志上下文：`relay/claude_handler.go:193`、`relay/common/override.go:178`。
- 近期 Responses Compact 审计、公共日志统计和日志详情样式改动没有删除 `reasoning_effort`；该问题是 Anthropic 请求链路原有的数据采集缺口。

## Requirements

### R1. 显示 Anthropic Reasoning Effort

- Anthropic 上游请求体中明确存在 `output_config.effort` 时，将该字符串写入 `relayInfo.ReasoningEffort`。
- 复用现有日志生成逻辑，将其输出为 `other.reasoning_effort`，不修改前端展示组件。
- 参数覆盖修改了 `output_config.effort` 时，显示覆盖后的值，例如 `xhigh`。

### R2. 保持最小范围

- 只处理 Anthropic/Claude 最终请求格式中的 `output_config.effort`。
- 用户已确认该最小范围；仅有 OpenAI 原始 effort 字段但最终 Claude 请求不存在 `output_config.effort` 时，不要求显示。
- 不从 `thinking.budget_tokens` 推断 low、medium、high。
- 不为其他渠道新增 Reasoning Effort 适配。
- 不新增通用的多协议 effort 提取框架。

### R3. 安全与兼容性

- 不记录完整请求体、对话内容、凭证或其他敏感字段。
- 复用 `common.Marshal` / `common.Unmarshal` 等项目 JSON 封装，不新增业务代码中的直接 JSON 编解码调用。
- 不修改日志数据库结构，不增加迁移，不改变现有日志 API 字段结构。
- 复用现有前端 `other.reasoning_effort` 展示，不新增前端文案或国际化键。

### R4. 回归测试

- 使用 `testify/require` 和 `testify/assert` 补充确定性的表驱动测试。
- 测试覆盖 `output_config.effort` 存在、参数覆盖后改变以及字段缺失三个场景。
- 测试必须断言 `RelayInfo.ReasoningEffort` 或消费日志 `other.reasoning_effort`，不能只断言请求体。

## Acceptance Criteria

- [ ] Claude Messages 请求包含 `output_config.effort: "high"` 时，消费日志详情显示 `high`。
- [ ] 渠道参数覆盖把 Anthropic `output_config.effort` 从 `max` 改为 `xhigh` 时，消费日志详情显示最终值 `xhigh`。
- [ ] 最终 Anthropic 请求没有 `output_config.effort` 时，日志中不出现 `reasoning_effort`，且不根据 thinking budget 猜测。
- [ ] 不修改现有前端即可展示 `xhigh` 等字符串值。
- [ ] 相关定向测试、`go test` 受影响包和 `git diff --check` 通过。

## Out Of Scope

- 扩大参数覆盖审计 `po` 的默认字段白名单。
- 为 `thinking.budget_tokens` 设计新的档位反推规则。
- 统一 OpenAI Chat、OpenAI Responses、Gemini 或其他渠道的 Reasoning Effort 日志语义。
- 修改 Reasoning Effort 的计费逻辑、渠道配置界面或数据库结构。
