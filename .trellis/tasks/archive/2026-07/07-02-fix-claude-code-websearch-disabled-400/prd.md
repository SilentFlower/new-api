# 修复 Claude Code WebSearch 未启用时官渠 400

## Goal

Claude Code 通过官方 Anthropic 渠道调用 Claude Messages WebSearch 时，如果渠道没有启用本系统的 Claude Code WebSearch 本地模拟，不应被本地配置拦截为 `status_code=400`；应让官方渠道按自身原生 WebSearch 能力正常转发。

## Background

- 用户反馈：请求失败为 `status_code=400`，错误为“渠道未启用 Claude Code WebSearch”，影响正常使用官方渠道；官方 Anthropic 渠道本身支持 WebSearch。
- 已确认代码路径：`relay/claude_handler.go:143` 对所有纯 Claude WebSearch 请求先进入 `handleClaudeWebSearchEmulation`，`relay/claude_handler.go:240` 在 `web_search.enabled=false` 时直接返回 400。
- 已确认官方渠道标识：`constant/channel.go:18` 定义 `ChannelTypeAnthropic`，`constant/channel.go:77` 默认官方 base URL 为 `https://api.anthropic.com`。
- 已有规范：`.trellis/spec/backend/relay-websearch-emulation.md` 规定本地模拟适用于渠道本身不支持 Claude `web_search` 工具的场景，并要求本地模拟短路不能污染上游请求体。

## Requirements

- R1：官方 Anthropic 渠道收到纯 Claude WebSearch 请求且渠道级 `web_search.enabled=false` 时，不能返回“渠道未启用 Claude Code WebSearch”的 400，本地逻辑必须放行到现有 Claude 转发路径。
- R2：渠道级 `web_search.enabled=true` 时，继续保持现有本地模拟行为，包括 provider 校验、短路响应和 Claude WebSearch 工具费记录。
- R3：非官方 Anthropic 渠道或不具备原生 Claude WebSearch 能力的渠道，在 `web_search.enabled=false` 时继续保持现有 400 行为，避免把不支持的工具直接转给上游。
- R4：本次修复只调整纯 Claude WebSearch 的本地模拟入口判断，不改管理端配置、provider 调用、计费规则、请求体转换和普通工具/混合工具行为。

## Acceptance Criteria

- [ ] 官方 Anthropic 渠道、纯 Claude WebSearch 请求、`web_search.enabled=false` 时，请求继续进入原有 `adaptor.ConvertClaudeRequest` / `adaptor.DoRequest` 转发链路，不调用本地模拟 provider，也不返回本地 400。
- [ ] 官方 Anthropic 渠道、纯 Claude WebSearch 请求、`web_search.enabled=true` 时，仍进入本地模拟路径。
- [ ] 非官方或不支持原生 Claude WebSearch 的渠道、纯 Claude WebSearch 请求、`web_search.enabled=false` 时，仍返回现有“渠道未启用 Claude Code WebSearch”400。
- [ ] 混合工具、多个工具、无工具或非搜索工具请求行为不变。
- [ ] 增加覆盖上述分支的后端测试，至少运行相关 Go 测试并通过。

## Notes

- 这是轻量 BUG 修复，PRD-only 足够；不新增数据库迁移或前端改动。
