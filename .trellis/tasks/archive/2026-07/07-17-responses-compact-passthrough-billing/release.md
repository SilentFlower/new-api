# 上线操作核对

## 结论

存在上线配置与外部依赖核对事项，需要人工复核（Needs human review）。

## 已核对证据

- `task.json`
- `prd.md`
- `design.md`
- `implement.md`
- `implement.jsonl`
- `check.jsonl`
- 业务提交 `564d50d8f`
- 任务进度提交 `2f9e2e558`

## 文档漂移核对

原任务缺少 `release.md`。本文件根据任务材料、提交文件范围和当前任务进度补充；真实 new-api 到 sub2api 四协议联调尚无运行环境和安全账号，兼容性结论需要人工复核。

## SQL 变更

无。

## 配置变更

- 部署后仅对已确认支持 Responses Compact 透传的目标渠道，将渠道 JSON 设置中的 `responses_compact_passthrough_enabled` 显式设为 `true`。
- 旧渠道缺少该字段时默认关闭；未完成上游兼容性核对的渠道不得直接开启。
- 不需要配置或保留 `*-openai-compact` 虚拟模型价格作为透传前提。

## 批处理、部署脚本与数据修复

无。

## 外部系统与依赖平台

- 需要确认目标 sub2api 已支持 V1 Compact、历史 body bridge、V2 HTTP/SSE 和 Responses WebSocket 对应协议与路径。
- 本任务未修改 sub2api；当前缺少真实联调证据，需要人工确认目标环境版本和账号能力。

## 上线顺序

1. 确认或先部署兼容版本的 sub2api。
2. 部署本次 new-api 变更。
3. 对目标渠道开启 `responses_compact_passthrough_enabled`。
4. 完成四协议联调和计费、日志核对后再扩大启用范围。

## 回滚说明

- 优先关闭目标渠道的 `responses_compact_passthrough_enabled`，恢复为默认拒绝透传。
- 如需代码回滚，删除独立 Compact 透传模块并撤销原有文件中的薄接入点；无数据库回滚。

## 上线后验证

- 使用基础模型分别验证 V1、历史 body bridge、V2 HTTP/SSE 和 V2 WebSocket 能到达 sub2api。
- 验证未配置 `*-openai-compact` 模型价格时仍按基础模型完成预扣和结算。
- 验证关闭渠道开关时返回专用 503，且不换渠、不清亲和性、不自动禁用、不预扣。
- 核对请求与响应保持原始 payload，日志不包含正文、密文、完整 query 或凭证。
