# 统计总 Token 包含 Anthropic 缓存

## Goal

将 Anthropic/Claude 请求的“总 Token”统计口径调整为包含缓存输入，避免数据看板、排行、导出等统计低估缓存命中的真实请求规模。

目标统计口径：

```
总 Token = 普通输入 Token + 缓存读取 Token + 缓存写入 Token + 输出 Token
```

扣费口径不在本任务中变更；缓存读写仍按已有缓存倍率计算。

## Confirmed Facts

- Anthropic 原生 usage 目前按 `input_tokens` 记录到 `logs.prompt_tokens`，按 `output_tokens` 记录到 `logs.completion_tokens`。
- `cache_read_input_tokens` 记录在日志 `other.cache_tokens`，`cache_creation_input_tokens` 记录在 `other.cache_creation_tokens` / `other.cache_creation_tokens_5m` / `other.cache_creation_tokens_1h`。
- 当前后端聚合统计多处使用 `prompt_tokens + completion_tokens`，因此不会包含 Anthropic 缓存读写 Token。
- 旧 UI 数据看板使用后端返回的 `token_used`；前端只是展示和绘图，不自己重新计算。
- 旧 UI 数据看板无筛选时读取 `quota_data.token_used` 缓存表；该表历史值在请求记录时已经按旧口径写入。
- 旧 UI 数据看板带 API Key / 分组筛选、单个 Token 日志查看器趋势图、导出 Sheet 1/2 均从 `logs` 聚合，历史数据可以通过调整聚合逻辑按新口径重算。
- 导出 Sheet 3 当前逐条展示输入 Token，并额外标注缓存读/写；是否把该列改为合计展示属于展示口径决策。

## Requirements

- R1：总 Token 统计必须包含 Anthropic 缓存读取 Token。
- R2：总 Token 统计必须包含 Anthropic 缓存写入 Token，包含普通 `cache_creation_tokens` 和 5m/1h 拆分值。
- R3：扣费计算、缓存读写倍率、消费日志明细中的缓存拆分信息保持不变。
- R4：后台聚合统计中的 `token_used` 统一使用新口径，覆盖数据看板、排行、旧 UI 图表、导出汇总和明细聚合。
- R5：新增逻辑必须兼容 SQLite、MySQL、PostgreSQL；不能依赖单一数据库的 JSON 函数。
- R6：旧 UI 和默认 UI 的图表展示不应各自实现不同 Token 口径，应优先由后端统一返回新口径的 `token_used`。
- R7：历史数据必须回算：`logs` 相关聚合直接按新口径实时计算；`quota_data` 历史缓存表必须提供重建/回算路径。

## Impact Notes

- 旧 UI 图表：
  - 带筛选或单 Token 日志趋势：改后会影响历史曲线，因为数据从 `logs` 聚合。
  - 数据看板无筛选总览：未来数据可随写入逻辑修正；历史 `quota_data` 是否变化取决于是否做回算/重建。
- 旧 UI 导出：
  - Sheet 1“汇总统计”和 Sheet 2“模型明细”会随后端 `logs` 聚合口径变化，历史导出会变大。
  - Sheet 3“请求日志”当前是逐条输入 + 缓存拆分展示；默认保留拆分，避免把“输入列”含义改得不清楚。
- 默认 UI：
  - Dashboard 使用 `token_used` 汇总，跟随后端新口径变化。
  - Usage Logs 列表继续显示输入/输出和缓存读写拆分，不强行把输入列改成总输入。
- 排行：
  - 模型排行读取 `quota_data.token_used`，必须跟随历史回算，否则排行历史值仍会低估缓存请求。

## Out of Scope

- 不调整实际扣费金额和倍率。
- 不改 Anthropic/OpenAI 对外响应体 usage 格式。
- 不删除缓存读写拆分展示。
- 不引入数据库专属 JSON 聚合表达式。

## Decisions

- D1：缓存写入 Token 计入总 Token。理由：`cache_creation_input_tokens` 是本次请求实际处理的输入规模，只按费用倍率不同，不应从总量统计中消失。
- D2：历史 `quota_data` 需要回算。理由：旧 UI 无筛选数据看板和默认 UI 排行读取 `quota_data`，不回算会和导出、筛选图表、单 Token 趋势不一致。
- D3：跨数据库兼容优先。历史日志中的缓存字段来自 `logs.other` JSON 字符串，统计逻辑不得依赖数据库 JSON 函数，应在 Go 侧解析。

## Acceptance Criteria

- [ ] Anthropic 原生格式的消费日志总 Token 统计包含普通输入、缓存读、缓存写和输出。
- [ ] OpenAI 格式转 Claude 的响应体 usage 行为不被破坏。
- [ ] 旧 UI 数据看板带筛选图表、单 Token 趋势图、导出 Sheet 1/2 的历史统计按新口径展示。
- [ ] 无筛选数据看板和模型排行使用回算后的 `quota_data` 新口径。
- [ ] 导出 Sheet 3 仍能看清普通输入、缓存读、缓存写、输出，不产生双算误导。
- [ ] 新增或更新测试覆盖 Anthropic 缓存读、缓存写、5m/1h 拆分缓存写。
- [ ] 通过相关 Go 测试；如改前端展示，通过旧 UI/默认 UI 相关构建或类型检查。

## Notes

- 关键代码线索：
  - `relay/channel/claude/relay-claude.go`：Anthropic usage 解析和 OpenAI usage 转换。
  - `service/text_quota.go`：消费日志写入和 `other` 缓存字段生成。
  - `model/usedata.go`：数据看板 `quota_data` 与 `logs` 聚合。
  - `model/log.go`：导出汇总、明细聚合、单 Token 趋势聚合。
  - `controller/usedata.go`：旧 UI 导出 Excel。
