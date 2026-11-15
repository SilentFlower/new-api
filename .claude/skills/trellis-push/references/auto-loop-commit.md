# Auto-Loop 内部提交

仅 validated runner 调用内部 commit-only 时读取，并与 SKILL 的普通预检和精确提交复用；不借此授权交互式 Git。

auto-loop 可调用内部 `commit-only`，复用本 skill 的仓库发现、动态多仓计划、确定性本地生成、精确提交和失败保留能力；不再次确认、不 push，也不执行 Step 5 的任务进度写入、进度 commit 或 progress push。Auto-Loop runner 在提交成功后原子写入本地 `task.json.progress`、`status=completed`、`completedAt` 与确定性 `closeout`。

内部 `commit-only` 不接受超出当前任务证据、runner owned dirty 和 protected-retained 边界的文件，不执行远端推送或其他附加动作。安全条件不满足时返回失败，由调用方决定后续状态。

auto-loop 内部 `commit-only` 已由 runner 的 `run_check_all -> run_spec_update -> commit_only` 状态机和预授权保证顺序，因此不重复记录或判断本交互证据。

auto-loop 内部 `commit-only` 也允许 retained dirty 存在，但每个生成/提交步骤前后都必须验证 retained exact paths 的内容摘要不变，并确认它们与 planned/generated paths 不冲突。内部模式仍要求 staged 区为空；计划外 dirty、retained 漂移、未知 staged、无法由当前计划或已记录提交解释的分支/HEAD 漂移，或归属歧义立即停止后续副作用。

auto-loop 内部 `commit-only` 不渲染交互式计划或结果，也不再次询问用户，因此不得为了内部执行读取 `references/output-templates.md`。它仍生成同样的逐仓执行数据用于自检、恢复和调用方结果记录，并且只能在当前任务 artifacts、runner owned dirty 和 protected-retained 边界内形成 exact files/message。

auto-loop retry/resume 时，读取调用方提供的已完成仓库提交，逐个验证 repository、commit object、message 和文件集合仍符合当前任务证据，并确认当前分支/HEAD 变化可由这些提交解释。验证通过的提交直接跳过；验证失败立即 blocked，不重复提交。确定性生成入口可以安全重跑，以当前 Git 状态重新规划后续步骤。

auto-loop 内部链失败时向调用方返回全部已完成仓库提交和失败位置。只有确定性生成未收敛或仍可安全重新规划的本地预检使用 `commit-repairable`；计划外 dirty、retained 漂移、未知 staged、无法由当前计划或已记录提交解释的分支/HEAD 漂移、归属歧义和外部副作用风险必须立即 blocked。不得 reset、rebase、revert、amend 或撤销成功提交。
