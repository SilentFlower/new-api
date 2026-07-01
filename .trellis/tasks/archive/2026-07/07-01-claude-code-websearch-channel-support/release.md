# Release Operations

## Conclusion
Release operations exist.

## Evidence Checked
- `task.json`
- `prd.md`
- `design.md`
- `implement.md`
- `implement.jsonl`
- `check.jsonl`
- `git show --name-only e280416c 17ba4cc3`
- `git log --oneline --name-only -6`
- `git status --porcelain`

## Drift Check
Missing release.md; 本文件按当前任务文件与已推送提交证据补充。

## SQL Changes
None. 本任务复用 `channels.setting` JSON 字段，不需要数据库迁移。

## Configuration Changes
- 上线后需要管理员在渠道配置中按渠道开启 `setting.web_search.enabled`，并选择 `tavily` 或 `anysearch`。
- 使用 Tavily 时必须配置 WebSearch API Key；使用 AnySearch 时 API Key 可选。
- 编辑已配置密钥的渠道时，空输入表示保留旧密钥；显式清空需要 `clear_api_key=true`。

## Batch / Deployment Scripts / Data Repair
None.

## External Systems / Dependent Platforms
- Tavily provider 调用 `https://api.tavily.com/search`，需要确认部署环境可以访问该端点。
- AnySearch provider 调用 `https://api.anysearch.com/mcp`，需要确认部署环境可以访问该端点。
- 不需要在第三方控制台执行批处理；只需要按所选供应商准备可用凭据。

## Release Order
1. 部署后端和前端变更。
2. 在目标渠道开启 Claude Code WebSearch 并选择供应商。
3. 如选择 Tavily，填写供应商 API Key；如选择 AnySearch，可按需填写 API Key。
4. 使用纯 Claude Code `web_search` 请求验证本地短路响应。

## Rollback Notes
- 可先在渠道配置中关闭 `setting.web_search.enabled`，停止本地 WebSearch 模拟。
- 如需完全回滚，回滚本任务相关代码提交。

## Post-release Verification
- 验证 default UI 和 classic UI 都能显示并保存 WebSearch 配置。
- 验证 Tavily 未配置 key 时创建或更新会返回明确错误。
- 验证 AnySearch 未配置 key 时仍允许保存并执行搜索。
- 验证纯 Claude Code WebSearch 请求不会生成上游请求体，混合工具请求保持原转发路径。
