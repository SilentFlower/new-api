# Brief — 修复 Claude Code WebSearch 未启用时官渠 400

## Goal

- 官方 Anthropic 渠道在未启用本系统 Claude Code WebSearch 本地模拟时，纯 Claude WebSearch 请求不再被本地 400 拦截，而是走官方原生 WebSearch 转发路径。

## Scope

- 调整纯 Claude WebSearch 的本地模拟入口判断：`web_search.enabled=false` 且渠道具备原生 Claude WebSearch 能力时放行到现有 Claude 转发链路。
- 保留 `web_search.enabled=true` 的本地模拟行为。
- 增加后端测试覆盖官方 Anthropic 透传、启用本地模拟、非原生渠道未启用仍 400。

## Non-Goals

- 不修改管理端 WebSearch 配置、脱敏、复制或保存逻辑。
- 不修改 Tavily / AnySearch provider 调用、计费规则、请求体转换和普通工具/混合工具行为。
- 不新增数据库迁移或前端改动。

## Key Context

- `relay/claude_handler.go:143` 当前对所有纯 Claude WebSearch 请求先调用 `handleClaudeWebSearchEmulation`。
- `relay/claude_handler.go:240` 当前在 `web_search.enabled=false` 时返回“渠道未启用 Claude Code WebSearch”400。
- `constant/channel.go:18` 定义官方 Anthropic 渠道类型 `ChannelTypeAnthropic`，`constant/channel.go:77` 默认 base URL 为 `https://api.anthropic.com`。
- `.trellis/spec/backend/relay-websearch-emulation.md` 说明本地模拟适用于渠道本身不支持 Claude `web_search` 工具的场景，并要求短路不污染上游请求体。

## Acceptance

- 官方 Anthropic 渠道、纯 Claude WebSearch 请求、`web_search.enabled=false` 时进入原有 `adaptor.ConvertClaudeRequest` / `adaptor.DoRequest` 链路，不调用本地模拟 provider，也不返回本地 400。
- 官方 Anthropic 渠道、纯 Claude WebSearch 请求、`web_search.enabled=true` 时仍进入本地模拟路径。
- 非官方或不支持原生 Claude WebSearch 的渠道、纯 Claude WebSearch 请求、`web_search.enabled=false` 时仍返回现有 400。
- 混合工具、多个工具、无工具或非搜索工具请求行为不变。
- 相关 Go 测试通过。

## Next Step

- 启动任务后进入 Phase 2.1，按 `trellis-route(implement)` 选择实现路径，并在编码前读取后端质量和目录规范。
