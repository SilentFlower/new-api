# Brief — 合并 v1.0.0-rc.24 到 build-bak

## Goal

- 将官方 `v1.0.0-rc.24` 精确合并到 `build-bak`，吸收上游新架构，同时保留全部定制能力和 `/root/project/ai-fund` 鉴权兼容性。

## Scope

- 精确合并标签提交 `5c3abffe8572aa8a49f15c3916707d2019d66af4`，合并前创建可恢复的本地备份引用，并保留真实双方 Git 历史。
- 按三方语义解决预计 96 个冲突路径，建立“双方意图、处理策略、功能契约、验证证据”冲突台账。
- 跟随 rc.24 将 Default 前端扁平化到 `web/`，正式删除 `web/classic/`，迁移 `build-bak` 的 Default 定制功能和六语言翻译。
- 接入 rc.24 的 JWT/会话控制、RelayKit、分层计费、新通道及 HTTP/2/请求体重放变化。
- 保留 Personal Access Token 回退、API Key 只读日志及旧 `New-Api-User` header 兼容，默认不修改 `ai-fund`。
- 保留消息审计、GitHub Key 泄漏扫描、通道用户并发限制、Responses Compact/WebSocket、Alpha Search、视觉辅助、Claude WebSearch 模拟、映射后模型计费、公共 API Key 日志、Excel 导出与筛选、Token 迁移、User-Agent 日志和非流式 JSON keepalive。
- 完成后端、前端、数据库兼容、Git 范围和关键功能回归，输出可追溯的合并报告。

## Non-Goals

- 不合并 `v1.0.0-rc.24` 之后 `main` 上的 19 个后续提交。
- 不借本次合并新增无关功能、重写架构或进行大范围清理。
- 未发现无法在 `new-api` 侧兼容的明确契约问题前，不修改 `/root/project/ai-fund`。
- 未经最终提交确认，不推送远端、不改写远端历史。

## Key Decisions

- 已确认正式移除 Classic，只保留 rc.24 的单前端 `web/` 结构；迁移功能契约，不迁移 Classic/Semi Design 视觉实现。
- 采用上游结构优先、`build-bak` 能力回填策略，禁止对业务冲突批量使用整侧 `ours` 或 `theirs`。
- 使用一次真实的非快进 merge，冲突解决和必要兼容修复最终形成一个 merge commit，不使用 squash 或复制文件替代合并。
- JWT/会话作为新主链路，但 Personal Access Token 和 API Key 兼容能力属于必须保留的公开契约。
- 本任务保持单任务原子执行：真实 merge 共享同一个 index 和 `MERGE_HEAD`，认证、计费、RelayKit、HTTP 与前端需联动验证，不拆成可独立提交的子任务。

## Key Context

- 规划基线：`build-bak` 为 `63c77d5ad`，rc.24 为 `5c3abffe8`，共同祖先为 `7c28993f6`；双方分别独有 250 和 77 个提交。
- 临时试合并发现 96 个未解决路径：47 UU、28 UD、18 AU、3 AA；主要集中在前端、Relay、认证、计费和 RelayKit。
- 高风险区域包括 `middleware/auth.go`、`model/user.go`、`relay/`、`relaykit/`、计费/日志 service、HTTP transport、数据库迁移以及 `web/` 目录级迁移。
- 计费必须遵循 `pkg/billingexpr/expr.md`、quota saturation 审计和三数据库兼容要求；定制能力分别受对应 `.trellis/spec/backend/` 契约约束。
- 定制逻辑优先保留在独立文件，上游核心文件只做必要薄接入，以降低后续同步冲突。

## Risks / Deferred

- 最大风险不是文本冲突数量，而是文件看似解决后认证、计费、重试或定制能力出现静默语义回退；冲突台账和逐项功能矩阵是强制验证手段。
- 前端目录重构可能造成旧路由、依赖、生成文件或翻译残留，需要确保最终只有 `web/` 参与构建。
- PAT/API Key 兼容需防止被新 JWT 会话路径短路；`ai-fund` 只读联调仅在现有凭据可用时执行，且不得输出密钥。
- 最终 merge commit 和推送延后到质量检查完成后的 Trellis 提交门禁，由用户确认精确范围。

## Acceptance

- rc.24 提交是最终 `build-bak` 的祖先，且 rc.24 之后的上游提交未被意外带入；合并前备份引用可验证。
- Git index 不存在未合并条目，代码不存在冲突标记，预计冲突路径均有处理和验证记录。
- rc.24 的前端、认证、RelayKit、计费、通道与 HTTP 核心能力落地，Classic 和旧 Default 构建入口无残留。
- 所有列明的 `build-bak` 定制能力都有新代码入口和验证证据，无静默消失。
- JWT、Personal Access Token、API Key 只读日志均可用，`ai-fund` 当前调用方式保持兼容。
- Go 格式化、静态检查、相关及全量可行测试通过；前端 i18n、typecheck、lint、格式检查和生产构建通过。
- 数据库改动兼容 SQLite、MySQL、PostgreSQL；受保护项目身份、品牌、归属和元数据没有被更改。
- 最终合并报告列明主要冲突取舍、功能保留结果、验证证据和剩余风险。

## Next Step

- 经用户确认本 Brief 后运行 `task.py start`，进入 `trellis-route(target=implement)`；首个实施动作是重新校验精确 tag 和当前 HEAD、创建备份分支，然后启动 `--no-ff --no-commit` 合并。
