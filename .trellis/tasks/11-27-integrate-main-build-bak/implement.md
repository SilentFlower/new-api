# 集成实施顺序

1. 核对所在分支、两个固定提交与现存未提交文件的内容摘要。在当前工作目录执行 `git merge --no-ff --no-commit 81c1fd20efade39a5d5692c8018dfef4cfb30e1d`，用 `git ls-files -u` 对照 research 冲突清单。若 Git 报告覆盖某一路径，先单独保护该路径再继续。
2. 恢复 Go 1.26.10 与 Bun 1.4.4 的构建依赖、DTO、路由及模块边界。处理已复现的 Excel 库缺失、视觉辅助类型缺失、PING 双重声明及 Claude/Gemini 转换回归。仅暂存已核对的合并路径。
3. 按渠道选择、预算预检、预扣、上游调用、结算或退款的顺序合并。逐轮核对 WebSocket、Compact、搜索工具、识图缓存和异步任务，在相同测试输入下对照余额及租约。
4. 合并数据库、账号与管理端口，覆盖历史身份、工作流令牌、受限 PAT、审计和告警。合并看板、Excel/CSV、七语言、表单和路由。将 48 个定制侧同路径独有测试名逐项对照。
5. 按 `research/upstream-upgrade-notices.md` 检查每一项的适用性。在测试副本演练四个用户额度列、价格重配、旧 PAT 到期与新作用域、敏感词规则迁入和回退、Redis 签名及两节点缓存一致性。记录需要部署方提供的价格和启用项。
6. 在候选代码上用 `bun install --frozen-lockfile`、`bun run i18n:sync`、`bun run typecheck`、`bun run test`、`bun run build` 验证 web 并准备嵌入产物。根目录执行 `go test ./...`、`go build ./...`。分别在 relaykit 与 tokenkit 目录执行 `GOWORK=off go build ./...` 和 `GOWORK=off go test ./...`，另做并发路径定向 race 验证、多库及 Redis 演练、跨层重放。需要测试数据库时优先准备隔离实例。
7. 核对 `git ls-files -u`、`git diff --cached --check`、MERGE_HEAD、原有工作区摘要，以及所有阶段的实测结果。将实测与待验证项目分别记录，进入质量检查和后续提交评审。

仅在决定撤销待提交合并时使用 `git merge --abort` 并复核工作区。数据恢复演练按测试副本升级前的余额和策略记账。
