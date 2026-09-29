# Completed Task Recovery

本 reference 只在 `task_progress.py status` 返回 `taskStatus=completed` 时加载。它是普通任务记录发布恢复与 Close 待解阻的唯一详细语义 owner；`trellis-continue` 和 completed workflow-state 不复制本分支矩阵。

## Evidence

固定当前任务路径与 `task.json`，读取文件级任务状态、`closeout`、当前分支、upstream、`HEAD`、`@{u}..HEAD` 的提交消息与文件集合，以及 `python3 ./.trellis/scripts/auto_loop.py status --verbose`。所有恢复都必须验证 exact task、最终 progress、`status=completed`、Close 状态、分支和提交归属；progress 文本不能替代 runtime 或 Git 证据。

普通发布恢复逐个审计 ahead 提交。除本任务可归属的业务或任务记录提交外，只有按同目录 `SKILL.md` Step 2 及 `scripts/verify_gc_commit.py` 验证的纯自动 GC 提交可使用默认许可；仅凭 GC 消息或路径名不构成证据。

## Outcomes

按以下优先级只返回一个结果：

1. **Auto-loop 本地完成，无需 Push**：健康的终态或 recent auto-loop run 精确包含当前任务，记录的本地提交仍可验证，且任务 dirty 仅为 runner 在提交后写入的 `<task-dir>/task.json` progress/lifecycle bookkeeping。停止 Push；`closeout.status=closed` 时无需其它生命周期动作。
2. **任务记录 commit + push 恢复计划**：当前任务 exact files 仍 dirty，`task.json` 已包含合法最终 progress 与 `status=completed`，且文件集合可由首次确认或重新确认闭合。不得重复业务提交或 helper 写入；即使 Close 已成功，也只恢复已确认的任务记录发布。
3. **任务记录 push-only 恢复计划**：当前任务目录 clean；upstream 存在；`@{u}..HEAD` 中存在消息、exact file set 和完成态均可归属的任务记录 commit；其余 ahead 仅含已验证自动 GC 或其它可归属本任务的提交。只推送这些已存在提交，不创建新 commit。
4. **Close 待解阻**：任务记录已经同步，`closeout.status=blocked|pending`，且 blockers 证据一致。报告结构化 blockers；条件解决后只重试 `task.py close <task>`，持久化语义 blocker 由所属 owner 完成后追加 `--resolve-blocker <code>`，不重复业务 Git 动作或 progress 写入。
5. **无需动作**：任务记录已经同步且 `closeout.status=closed`。任务已退出活跃视图，物理 GC 由后续 SessionStart 在三天阈值后处理。
6. **阻断**：runtime 与 Git 矛盾、auto-loop marker 无健康完成证据、缺少普通路径 upstream、任务 dirty 超出 exact files、存在未通过 GC 审计且无法归属的 ahead，或提交消息/文件集合/分支无法闭合。报告具体证据缺口，不 push、不 Close，也不猜测完成来源。

恢复计划仍使用 `trellis-push` 的既有一次确认、执行前漂移检查和结果模板。这里只决定完成态恢复范围，不新增状态、持久化字段或自动确认。
