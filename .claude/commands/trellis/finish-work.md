# Finish Work

### HIGHEST PRIORITY: skill-garden finish-work bookkeeping override

<!-- BEGIN skill-garden skill override trellis-finish-work v0.6 -->

> 来源：github.com/SilentFlower/skill-garden。本注入块覆盖原生 finish-work Step 2-4 中的 release 核对、archive/journal 提交和可选 bookkeeping push 规则。

#### Finish-work Exact Bookkeeping

业务代码必须已经在 Phase 3.4 通过 `trellis-push` 处理。finish-work 只处理当前任务的 release audit、archive 和本次 journal，不重新提交业务代码，也不根据任务进度决定 Git 动作。

开始任何移动前记录：

- 当前活动任务的 source path、task name 和 `task.json.children`。
- 当前 branch、upstream、`HEAD`、upstream HEAD，以及 `@{u}..HEAD`。
- `baseline_synced=true` 仅当 upstream 存在且开始时 `HEAD == upstream HEAD`。
- 当前计划外 staged paths，供提交后复核。

无关规划任务、旧 archive、其他窗口留下的 untracked/unstaged/staged 文件全部保留，不要求用户分类，不阻塞 finish-work，也不成为跳过本地 bookkeeping commit 的理由。只有当前任务仍有未提交业务文件时才停止并返回 Phase 3.4。detached HEAD、冲突、rebase 或其他无法安全提交的 Git 状态仍应停止并报告。

按下面顺序执行。

##### 1. 当前任务 release audit

自动调用 `trellis-release audit-current`。该模式负责读取当前任务 artifacts 和 Git 证据，并返回 `no-op`、`written` 或 `needs-review` 的结构化结果。

- 不增加确认问题。
- 不在 finish-work 内重复 SQL、配置、批处理、外部系统或文档漂移判断。
- audit 写入的 `<task>/release.md` 由后续 archive 路径自然纳入当前任务归档。
- audit 不确定或标记 `Needs human review` 时继续归档，并在最终结果中保留该风险。

##### 2. 归档落盘

始终使用：

```bash
python3 ./.trellis/scripts/task.py archive <task-name> --no-commit
```

从命令结果与 scoped diff 确定：原 source path、实际 archive destination，以及归档父任务时实际变化的 child `task.json`。不得把 `.trellis/tasks` 或 `.trellis/tasks/archive` 根目录作为暂存范围。

##### 3. Journal 落盘

始终使用：

```bash
python3 ./.trellis/scripts/add_session.py --no-commit \
  --title "<session title>" \
  --commit "<business commit hashes>" \
  --summary "<brief summary>"
```

从命令结果与 scoped diff 确定本次实际变化的 journal/index exact paths。

##### 4. 精确提交

如果 `session_auto_commit: false`，保留上述磁盘写入，不执行任何 commit 或 push；报告 release、archive 和 journal 的 exact dirty paths。

如果 `session_auto_commit: true`，生成两个独立提交：

```bash
git add -- <actual archive destination> <changed child task.json files>
git rm -r --cached --ignore-unmatch -- <original task source>
git commit --only -m "chore(task): archive <task-name>" -- \
  <original task source> <actual archive destination> <changed child task.json files>

git add -- <exact journal/index paths>
git commit --only -m "<configured session commit message>" -- <exact journal/index paths>
```

没有实际 journal 变化时跳过第二个提交。每个提交后用 `git show --name-only --format=` 复核只包含允许路径，并确认开始时的计划外 staged paths 仍保留在 index。无关 dirty/staged 文件不要求工作区 clean。

##### 5. 自动 push

只使用 finish-work 开始时记录的 Git 基线：

- `baseline_synced=true`：确认当前分支/upstream 未变化，且开始后新增的 ahead commits 只包含本轮 archive/journal bookkeeping commits；然后执行 `git push <upstream-remote> <current-branch>`。
- 开始时已有 ahead commits、分支落后/分叉，或没有 upstream：保留本轮本地 commits，不自动 push。
- 执行期间出现并发 commit、分支/upstream 变化或 push rejection：停止自动 push，保留本地结果并报告。

禁止 force push。是否自动 push 与 `progress`、legacy task 字段、工作区是否整体 clean 无关。

##### 6. 最终结果

分别报告：

- release audit 状态与 `release.md` 路径（如有）。
- archive destination 和 archive commit（如有）。
- journal paths 和 journal commit（如有）。
- push 状态：`pushed`、`local-only`、`skipped` 或 `failed`，以及基线原因。
- 保留未动的无关 dirty/staged paths。

<!-- END skill-garden skill override trellis-finish-work v0.6 -->

Wrap up the current session: archive the active task (and any other completed-but-unarchived tasks the user wants to clean up) and record the session journal. Code commits are NOT done here — those happen in workflow Phase 3.4 before you invoke this command.

## Step 1: Survey current state

```bash
python3 ./.trellis/scripts/get_context.py --mode record
```

This prints:

- **My active tasks** — review whether any besides the current one are actually done (code merged, AC met) and should be archived this round.
- **Git status** — quick visual on what's dirty.
- **Recent commits** — you'll need their hashes in Step 4 for `--commit`.

If `--mode record` surfaces other completed tasks not tied to the current session, surface them to the user with a one-shot confirmation: "These N tasks look done — archive them too in this round? [y/N]". Default is no; the current active task is always archived in Step 3 regardless.

## Step 2: Sanity check — classify dirty paths

Run:

```bash
git status --porcelain
```

Filter out paths under `.trellis/workspace/` and `.trellis/tasks/` — those are managed by `add_session.py` and `task.py archive` auto-commits and will appear dirty as part of this skill's own work.

For each remaining dirty path, decide whether it belongs to **the current task** or to **other parallel work** (e.g., another terminal window editing the same repo). Heuristics:

- Paths referenced in the current task's `prd.md` / `implement.jsonl` / `check.jsonl` → current task
- Paths in code areas matching the task's stated scope, or that you remember editing this session → current task
- Paths in unrelated areas you have no recollection of touching this session → other parallel work

Then route:

- **Any remaining path looks like current-task work** — bail out with:
  > "Working tree has uncommitted code changes from this task: `<list>`. Return to workflow Phase 3.4 to commit them before running `/trellis:finish-work`."

  Do NOT run `git commit` here. Do NOT prompt the user to commit. The user goes back to Phase 3.4 and the AI drives the batched commit there.
- **All remaining paths look unrelated** (other parallel-window work) — report them once and continue to Step 3:
  > "FYI, dirty files outside this task's scope — leaving them for the other window: `<list>`."
- **Genuinely unsure** — ask the user once: "Are `<list>` this task's work I forgot to commit, or another window's? (commit / ignore)" — then route per their answer.

## Step 3: Archive task(s)

```bash
python3 ./.trellis/scripts/task.py archive <task-name>
```

At minimum: the current active task (if any). Plus any extra tasks the user confirmed in Step 1. Each archive produces a `chore(task): archive ...` commit via the script's auto-commit.

If there is no active task and the user did not confirm any cleanup archives, skip this step.

## Step 4: Record session journal

```bash
python3 ./.trellis/scripts/add_session.py \
  --title "Session Title" \
  --commit "hash1,hash2" \
  --summary "Brief summary"
```

Use the work-commit hashes produced in Phase 3.4 (visible in Step 1's `Recent commits` list, or via `git log --oneline`) for `--commit`. Do not include the archive commit hashes from Step 3. This produces a `chore: record journal` commit.

Final git log order: `<work commits from 3.4>` → `chore(task): archive ...` (one or more) → `chore: record journal`.
