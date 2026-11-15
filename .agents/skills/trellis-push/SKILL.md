---
name: trellis-push
description: "按确认的精确文件范围提交普通变更或完成已就绪的 merge commit；多仓计划可包含已展示的本地生成命令，并在普通推送后同步当前任务记录与进度。"
---

# Trellis Push

`trellis-push` 独占 Phase 3.4：精确计划、提交推送、task 进度及 untracked 清理。

## 职责与条件入口

人工确认或有效 policy_timeout claim 与 started 回执之前，禁止 `git add`、`git commit` 或 `git push`。准备后台等待不执行 Git 写入。

最终回复前按 `.trellis/scripts/confirmation.md`：已有授权续接；只看计划/停止、本机制自身开发验证或未决风险保持人工。其余必须 项目 `prepare` 完整计划及精确范围；成功后后台 `wait`，原样完整展示 `display` 并结束回合。失败报告实际原因。提醒仅 `claimed=true`、`source=policy_timeout`、`action=execute_git_plan` 且 started 成功进 Step 4，后记终态。准备不需领取，回执不是人工确认，不扩大范围。

普通模式精确计划确认一次后 commit + push；仅用户要求本地提交才 commit-only，不写任务进度。task 普通计划含独立记录提交。

- 只提交 planned exact files，保留无关 dirty/staged；不用 `git add .` / `git add -A`，不要求整体 clean。
- untracked 只接受 `stage=push`，游标不替代证据、计划、确认或 Git 检查；无任务也可普通提交。
- 不处理上线、队列、日志或物理 GC；不发起、终止或解决合并。只允许普通模式完成已就绪 merge。

| 实际场景 | 先读细则 |
| --- | --- |
| 多仓/生成依赖、已有 merge 或历史 GC ahead | [git-special-cases.md](references/git-special-cases.md)；在相关规划或副作用前读取 |
| 普通 push 且有活动 task | [task-progress.md](references/task-progress.md)；计划前读取，业务 Git 后执行 |
| taskStatus=completed 的恢复 | [completed-task-recovery.md](references/completed-task-recovery.md)；先完成只读 preflight |
| validated runner 的内部 commit-only | [auto-loop-commit.md](references/auto-loop-commit.md)；不 push、不执行 Step 5、不渲染交互模板 |

未命中场景不加载其 reference；命中却缺失细则时阻塞，不凭记忆代替。内部 commit-only 只接受 runner 的合法预授权。

## Step 0：记录完成链证据

普通 push 或用户 `commit-only` 已经构成明确 Git 意图；也接受完成链 owner 的合法交接。除 auto-loop 内部 `commit-only` 外，按当前结果与实际 diff 记录：

- Check-All：通过、有效已接受风险、未运行/失效、未处置 findings、blocked 或部分验证；保留 ID/严重度；`[上线后验证]` 作为非阻断风险保留动作、责任、预期。
- 快速直改：仅无 task/untracked、仍符合 Request Triage 且必要定向验证成功覆盖当前 diff，才记 `不适用（快速直改）` 并列真实命令、结果、范围。验证缺失/失败/失效或范围扩大不得使用；task/tracked 缺检查仍按实际状态列风险，不得伪造通过。
- Update-Spec：根据当前 `spec_update_result` 与实际 diff 标记 `no-op`、`written`、`needs-review` 或未运行/失效。

禁止从历史、摘要或 dirty 推断通过、风险接受或授权。Step 3 展示证据，不会阻止读取 Git 状态或生成提交计划。本步骤不得返回 Phase 2.2，不得加载 `trellis-check-all` 或 `trellis-update-spec`，不补跑；阶段由各 owner 推进。

无 task 时运行 `python3 ./.trellis/scripts/untracked_flow.py status --verbose`。命中须记录 work id/summary/stage 且要求 `stage=push`；miss 走无任务路径，损坏则停。游标命中不表示 Push 已计划、已确认或已执行。

## Step 1：发现仓库与任务

候选仓库：

- 含 `.trellis/` 的父仓根目录。
- `.trellis/config.yaml` 中 package 路径对应的独立 Git root。
- 用户明确指定的候选仓库。

Git root 去重；非独立仓库的 package 归父仓。

仓库展示名取匹配 package 名，否则用 Git top-level 目录名。`root`、`parent`、`main repo` 仅为输入别名，不用于展示。

task/untracked 均可选：

```bash
python3 ./.trellis/scripts/task.py current --source || true
python3 ./.trellis/scripts/task_progress.py status --json || true
```

有活动任务则计划前读 [task-progress.md](references/task-progress.md)，获取文件级状态：

```bash
git status --short --untracked-files=all -- <task-dir>
```

若 `task_progress.py status` 返回 `taskStatus=completed`，立即按需读取 `references/completed-task-recovery.md`，由该 reference 完成只读 preflight 并返回“恢复计划 / Close 待解阻 / 无需动作 / 阻断”之一。在得到结果前不得进入普通业务规划，也不得重复已经成功的业务 Git 动作。reference 缺失、不可读或证据无法闭合时失败关闭；`task.json.progress` 只作诊断，不能单独选择恢复动作。

展开 `?? <task-dir>/` 至真实文件，禁止将折叠目录当成 exact file 或 pathspec。无活动 task 时仍可提交相关代码，但不生成任务进度。untracked 命中时，结合当前请求、work summary 和实际 diff 判断业务 `planned` 文件归属，计划同时显示 work id；无法明确归属的文件只能保留或作为风险。存在活动 task 时，结合 `brief.md`、`implement.md`、当前 diff 与本轮执行范围生成一行语义进度；同时识别当前任务目录中已存在且可归属的 dirty/untracked 产物，供 Step 5 生成任务记录 exact files。不得从旧进度推断 Git 动作。

## Step 2：预检与文件归属

逐仓读取：

```bash
git status --short
git branch --show-current
git rev-parse --abbrev-ref --symbolic-full-name @{u} 2>/dev/null || true
git diff --stat
git diff --name-only
git diff --cached --stat
git diff --cached --name-only
git diff --cached --check
git ls-files -u
git rev-parse --verify MERGE_HEAD 2>/dev/null || true
git log --oneline -5
git log @{u}..HEAD --oneline 2>/dev/null || true
git rev-list --reverse @{u}..HEAD 2>/dev/null || true
```

停止条件：

- detached HEAD、分支不可读、未解决冲突、rebase、cherry-pick、revert 或其它非 merge 的未完成 Git 集成状态。
- `MERGE_HEAD` 存在时仅普通模式可继续，并且必须固定当前 `HEAD` / `MERGE_HEAD`、确认 `git ls-files -u` 为空、全部 staged paths 都属于 planned 且没有 retained staged；否则停止。用户/auto-loop `commit-only` 不适用。
- 普通推送会携带既无法归属本次任务、也未通过特殊 Git reference 的自动 GC 审计的历史 ahead commits。
- 无法确定 planned file 是否属于当前请求或活动任务。
- 内部 `commit-only` 发现 staged 区非空。

存在无法归属的历史 ahead 时，先按 [git-special-cases.md](references/git-special-cases.md) 审计候选 GC；仍无法归属则停止。普通路径不运行物理 GC。

业务文件分两组；当前任务记录按下方独立提交：

- `planned`：本轮明确归属且准备提交的 exact files。
- `retained`：当前存在、但本次明确不提交并保持原状的 dirty paths，包含计划外 untracked、unstaged、staged 文件。clean files 不进入该集合。

普通模式存在活动任务时，当前任务目录中已存在且可归属的 dirty/untracked 产物不进入业务 `planned`，也不进入 `retained`；它们与预计由 helper 更新的 `<task-dir>/task.json` 组成 Step 5 的任务记录 exact files。其他任务目录和无法归属当前任务的文件仍属于 `retained` 或风险，不得顺带提交。

多仓/生成链先读 [git-special-cases.md](references/git-special-cases.md)。

展示名为“保留未提交的变更（dirty）”，阈值由模板所有；保留 exact paths/状态，摘要不得作 pathspec。未知 ahead、分支/upstream 或归属异常列风险；retained 不默认阻塞。

计划/基线默认留在上下文；跨进程或恢复才存 gitignored runtime。不生成 `retained.md` 等展示附件，不纳入任务产物或提交；runner 沿用原契约。

允许 `retained`；记录并保留计划外 staged set。新增文件须重新计划和确认，不静默扩大范围。

## Step 3：展示最小计划

普通模式或用户 `commit-only` 在所有计划数据已经收敛、即将展示用户可见计划时，读取 `references/output-templates.md` 的“计划模板”和“共用展示规则”再渲染；已读、未变化且内容仍可用时直接复用。

模板缺失、不可读或缺章时报 `阻塞`，不得凭记忆重建、缩写或自制替代模板。

多仓只确认一次。已展示生成命令和预计 exact files 时，成功且无列表外新 dirty 沿用原确认；内容/hash/统计变化不重问。其它边界变化按 Step 4 重规划。


## Step 4：精确提交与推送

逐仓按序执行，先复核 planned files、分支、HEAD、upstream、冲突、staged、全部 dirty paths 和 retained 摘要；普通推送还须逐个重验计划中已验证 GC 的完整哈希与当前 `@{u}..HEAD`。任一关键条件变化都停止当前执行并重新规划。普通模式仅 `retained` 内容变化时可更新说明。

普通精确提交：

```bash
git add -- <exact planned files>
git commit --only -m "<confirmed message>" -- <exact planned files>
```

提交后验证：

```bash
git show --name-only --format= HEAD
git diff --cached --name-only
```

commit 只能包含 planned files，执行前的计划外 staged set 必须仍保留。

已有 merge 的提交与双父验证按 [git-special-cases.md](references/git-special-cases.md)；不得套用上方带 pathspec 的普通 commit。

普通模式继续推送当前分支：

```bash
git push origin <current-branch>
```

已有 upstream 且远端名称不是 `origin` 时，使用实际 upstream remote。无 upstream 时只能在计划中明确将当前分支设置到选定 remote；不能猜测目标分支。

`commit-only` 到本地提交成功即结束，不推送，也不写远端任务进度。

## Step 5：同步任务进度

仅普通模式且有活动 task 时执行 [task-progress.md](references/task-progress.md)；该 reference 在计划前加载，业务动作完成后写真实进度及任务记录。untracked、用户或 runner commit-only 均跳过。

## Step 6：结果

untracked 的全部已确认 Git 动作成功后，最后运行 `python3 ./.trellis/scripts/untracked_flow.py clear --reason completed --work-id <work-id>`。清理成功才报告完成链已结束；任一仓库、push 或清理失败都保留状态并报告恢复位置，禁止因部分成功伪造完成。用户 `commit-only` 的已确认动作全部成功时同样可以完成并清理。

普通模式、用户 `commit-only` 或 untracked 展示结果前，读取 `references/output-templates.md` 的“共用展示规则”、“结果模板”和“结果补充规则”；原文未变化且仍可用则复用，变化或缺失才重读。

reference 缺失、无法读取或缺少对应章节时停止并报告 `阻塞`，不得凭记忆重建、缩写或自制替代结果。auto-loop 内部 `commit-only` 不读取或渲染该交互式结果模板，只按 Step 4 的失败保留契约向调用方返回逐仓 commits、files、retained、message 和失败位置，由 `trellis-auto-loop` 完成 `record + next`。

## 禁止事项

- 扩大到计划外文件或要求清理无关工作区。
- 把普通 push 中可归属当前活动任务的规划产物列为 retained，并以“之后再整理”为由延后首次记录。
- 执行首次计划未展示的生成命令，或生成计划外文件后仍沿用旧确认。
- 用任务进度决定是否推送代码。
- 在本 skill 内发起、终止、解决冲突或改变分支合并目标；只允许完成已就绪的 merge commit。
- 自动解决 push rejection、冲突、凭证或远端保护规则问题。
- 在业务失败后伪造已完成进度，或因进度同步失败回滚业务提交。
