# 任务进度同步

仅普通 Push 且有活动任务时，在计划前读取，业务 Git 后执行；其它模式跳过。

## Step 5：同步任务进度

仅普通模式且存在活动 task 时由本 skill 执行。untracked、用户 `commit-only` 与 auto-loop 内部 `commit-only` 都跳过本 Step；Auto-Loop runner 在 action record/next 后按自身契约写入本地 `task.json.progress`，不属于这里的任务记录提交或推送。全部业务仓库成功后一次原子写入最终 progress 与完成态，再提交并推送任务记录；已有仓库成功而后续仓库失败时只写 partial 进度，明确 completed、失败位置、next 和 notes，状态保持 `in_progress`。尚未发生成功 Git 动作就失败时，不记录虚假的 completed steps；只有父仓仍可安全提交并推送时才允许记录 failure notes。

新进度固定为：

```json
{
  "updatedAt": "<ISO 8601>",
  "completedSteps": ["<已完成步骤>"],
  "partialStep": "<部分完成步骤或 null>",
  "nextStep": "<下一步>",
  "notes": "<可选说明；无说明时为空字符串>"
}
```

进度不得保存本轮模式、业务 commit hash 或提交计划。

写入前确认：

- 当前任务 exact files 与首次确认的路径集合一致，没有新增当前任务路径或无法归属的 dirty 内容。
- 父仓分支、upstream 和冲突状态安全。
- 对当前 `@{u}..HEAD` 重做 Step 2 归属检查；已验证的自动 GC 可随任务记录推送，其它无法归属的历史 ahead commits 仍停止。

全部业务 commit/push 成功时，通过 helper 用同一份最终 progress 原子写入 `progress`、`status=completed`、`completedAt` 与确定性 `closeout`：

```bash
python3 ./.trellis/scripts/task_progress.py write \
  --task <task-dir> \
  --progress-json '<progress-json>' \
  --complete \
  --json
```

helper 返回 `closeResult=closed|already-closed` 时任务立即退出所有活跃视图；返回 `closeResult=blocked` 时必须保留并展示 `closeBlockers`，但仍可提交已完成的任务记录。部分成功时调用同一 helper，但不得携带 `--complete`，并写入精确恢复位置。用户 `commit-only`、auto-loop 内部 `commit-only` 和尚未发生任何成功业务 Git 动作的失败都不得由本 skill 请求 complete；auto-loop 的本地完成态与 Close 由 Auto-Loop runner 自己写入，不经过本步骤。helper 写入失败时任务保持原状态，不得继续任务记录提交或报告完成。

helper 成功后，只提交并推送首次确认的当前任务 exact files；该集合包含完成态 `task.json`，以及首次计划时已存在且可归属的当前任务 dirty/untracked 产物：

```bash
git add -- <current-task-exact-files>
git commit --only -m "chore(task): update <task-name> progress" -- <current-task-exact-files>
git push origin <current-branch>
```

该动作属于用户已确认的普通 push 计划，不增加第二次确认。提交后必须验证 commit 只包含首次确认的当前任务 exact files，且 `task.json` 已包含同一份最终 progress、`status=completed`、`completedAt` 与同次 Close 结果；其他任务和无关 dirty/staged 文件保持原状。

失败时保留真实现场，不 reset、amend、revert 或制造 dirty 回滚：

- helper 失败：任务保持 `in_progress`，不得创建任务记录 commit。
- helper 成功但任务记录 commit 失败：保留本地 `completed` 与当前任务 exact dirty，后续按 Step 1 的任务记录 commit 恢复路径重新验证和确认；不得重复业务提交或 helper 写入。
- 任务记录 commit 成功但 push 失败：任务目录应为 clean，并保留可归属的 ahead commit；后续只重试该 commit 的 push，不重复业务提交、helper 写入或任务记录 commit。
- 任务记录 push 成功：本任务产生的当前任务目录变更必须 clean；不得再次写入完成态或 Close。

任何恢复都必须验证当前分支、upstream、HEAD、`@{u}..HEAD`、任务记录 commit message 与 exact file set，以及 `task.json` 的最终完成态。已验证自动 GC 可按 Step 2 默认许可归属；其它未知 ahead 或 dirty 不能当作可恢复任务记录。
