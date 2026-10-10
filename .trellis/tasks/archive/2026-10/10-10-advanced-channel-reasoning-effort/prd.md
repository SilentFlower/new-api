# 高级渠道思考强度消费日志补齐

## Goal

让高级自定义渠道及非 GPT 模型的明确思考强度显示在消费日志详情中，便于直接核对实际转发参数。

## Background

- 源码基线为 `build-bak` 的 `7b042a8e69babe09d16cf65e8250f213546000f2`；生产参数治理已由根 #72 完成，日志缺口独立改造。
- `relay/channel/openai/adaptor.go:338` 附近仅在 GPT-5/o 模型分支填充 Chat 请求的 `RelayInfo.ReasoningEffort`；高级自定义的兼容路径复用此适配器，因此 DeepSeek 顶层 low 能透传但日志字段为空。
- `service/log_info_generate.go:132` 已把非空字段写入 `other.reasoning_effort`；`web/src/features/usage-logs/components/dialogs/details-dialog.tsx:1005` 已有通用展示，无须为高级渠道新建 UI。
- `relay/claude_reasoning_effort.go` 与日志规范已经规定 Anthropic 明确 effort、最终参数覆盖后取值、缺失不推断的合同。

## Requirements

1. 高级自定义渠道转发 DeepSeek、Qwen 等非 GPT 模型时，只要最终请求具有明确的字符串思考强度，消费日志记录该实际值。
2. 覆盖高级渠道支持的 OpenAI Chat、Responses、Claude 和 Gemini 请求格式，按最终格式读取对应强度字段，包含同步与流式。
3. 参数转换、模型映射、禁用字段处理和渠道参数覆盖后，再同步日志强度；覆盖、删除和跨渠道重试不得遗留旧值。
4. 请求体透传路径使用已解析的请求参数，不为日志新增完整正文复制、读取或持久化；不改变实际转发正文。
5. 复用既有日志字段与前端展示，保持 GPT、普通 DeepSeek、OpenRouter、Anthropic 等已有日志行为；没有明确强度则保持缺失，不猜测模型默认值或从思考预算推导等级。

## Constraints / Non-Goals

- 不改变模型路由、思考默认值、请求参数、计费、配额或凭据；不新增数据库列，不回填历史日志。
- 不把思考开关、reasoning Token 或数值预算伪装成 low/high；本轮不新增独立的思考开关 UI。
- 验证使用本机合成请求，不调用付费模型；提交、推送和生产发布分别按既有门禁处理，规划不包含生产写操作。

## Acceptance Criteria

- [x] 高级渠道 DeepSeek low/high、非 GPT 的明确 effort 能进入 `other.reasoning_effort`，日志详情复用原行展示。
- [x] 高级渠道各实际协议格式的明确强度正确；同步与流式一致，转换后的最终协议优先。
- [x] 覆盖后 low→high 显示 high，删除/缺失/空值/非字符串不保留旧值，跨渠道重试无串值。
- [x] 请求体透传、普通 GPT/DeepSeek/OpenRouter/Anthropic 原行为回归通过；正文与计费上下文不被日志处理修改。
- [x] 定向测试、必要回归、Go vet 和 diff 检查通过；证据严格区分本机验证与未来生产部署。

## Notes

- 用户原始要求：改造 `/root/project/new-api`，让高级渠道像其他渠道一样显示思考强度，不只支持 GPT。
- 根 #72 已按用户意见关闭；V2 故障由根 #73/#74/#75 跟踪，本任务只补日志可观测性。
- 上述开发验收基于本机 handler/元数据回归及既有持久化与详情页调用链静态核对，未代表生产消费记录或实际浏览器验收。用户在实现推送后另行授权 ACR 镜像同步与 ACK 发布，生产效果在该发布流程中核实。
