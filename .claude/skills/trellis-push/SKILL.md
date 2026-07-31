---
name: trellis-push
description: "按确认的精确文件范围提交普通变更或完成已就绪的 merge commit；多仓计划可包含已展示的本地生成命令，并在普通推送后同步当前任务记录与进度。"
---

# Trellis Push

`trellis-push` 是 Phase 3.4 唯一的代码提交入口。它只负责生成最小计划、精确提交、普通推送，以及触发当前任务进度同步。

## 职责边界

- 普通模式默认 `commit + push`。
- 普通多仓计划可以包含本地确定性生成命令；生成后没有新增计划外文件时沿用同一次确认。
- 普通模式把当前任务产物与更新后的 `task.json` 纳入同一次确认下的独立任务记录提交。
- 用户明确要求“只提交不推送”时使用 `commit-only`。
- auto-loop 可调用内部 `commit-only`，但必须传入已经校验过的 exact files 与 commit message；本 skill 只执行该提交。
- 不发起、终止或解决分支合并；只允许普通模式完成已经开始、冲突已清零且索引完全可归属的 merge commit。
- 不处理上线核对、任务归档、会话日志或自动任务队列状态。
- 不使用 `git add .`、`git add -A`，不要求工作区整体干净，也不提交计划外文件。

## 模式

| 模式 | 确认 | Git 动作 | 进度同步 |
| --- | --- | --- | --- |
| 普通 | 展示最小计划并确认一次 | exact commit；已有 merge 就绪时完成双父提交；然后 push | 有活动任务时立即同步 |
| 用户 `commit-only` | 展示最小计划并确认一次 | exact local commit | 跳过 |
| auto-loop 内部 `commit-only` | 复用 auto-loop 预授权 | exact local commit | 跳过 |

内部 `commit-only` 不接受临时扩大文件范围、远端推送或其他附加动作。安全条件不满足时返回失败，由调用方决定后续状态。

## Step 0：记录完成链证据

除 auto-loop 内部 `commit-only` 外，普通 push 或用户 `commit-only` 已经构成明确 Git 意图。本 skill 在读取 Git 提交计划前只记录当前可用的完成链证据，不补跑、不切换阶段，也不新增确认：

- Check-All：根据当前标准报告与实际 diff 标记为 `通过`、`未运行`、`已失效`、`存在 findings`、`blocked` 或 `部分验证`。没有可验证的当前报告时使用 `未运行`，不得从历史消息、摘要或 dirty 状态猜测通过。
- Update-Spec：根据当前 `spec_update_result` 与实际 diff 标记为 `no-op`、`written`、`needs-review`、`未运行` 或 `已失效`。结果缺失或无法证明仍适用于当前 diff 时使用 `未运行` / `已失效`。

上述状态只进入 Step 3 的完成链证据与风险展示，不会阻止读取 Git 状态或生成提交计划。本步骤不得返回 Phase 2.2，不得加载 `trellis-check-all` 或 `trellis-update-spec`，也不得要求用户改写成“跳过检查后 push”。正常 workflow 的 Check-All -> Update-Spec -> Push 顺序仍由 Phase 2.2、Phase 3.3 和各自 owner 推进；`trellis-push` 不反向补做上游阶段。

auto-loop 内部 `commit-only` 已由 runner 的 `run_check_all -> run_spec_update -> commit_only` 状态机和预授权保证顺序，因此不重复记录或判断本交互证据。

## Step 1：发现仓库与任务

候选仓库包括：

- 含 `.trellis/` 的父仓根目录。
- `.trellis/config.yaml` 中 package 路径对应的独立 Git root。
- 用户明确指定的候选仓库。

同一个 Git root 只保留一次。位于父仓内部但不是独立 Git root 的 package 变更归父仓处理。

为每个候选仓库生成用户可见名称：优先使用 `.trellis/config.yaml` 中匹配的 package 名；没有配置时使用 Git top-level 目录名。`root`、`parent`、`main repo` 只允许作为输入别名，禁止直接显示在计划或结果中。

活动任务是可选上下文：

```bash
python3 ./.trellis/scripts/task.py current --source || true
python3 ./.trellis/scripts/task_progress.py status --json || true
```

存在活动任务时，必须额外获取文件级任务状态：

```bash
git status --short --untracked-files=all -- <task-dir>
```

不得把默认 `git status --short` 可能返回的 `?? <task-dir>/` 折叠目录当成 exact file、展示条目或 pathspec。无活动任务时仍可提交相关代码，但不生成任务进度。存在活动任务时，结合 `brief.md`、`implement.md`、当前 diff 与本轮执行范围生成一行语义进度；同时识别当前任务目录中已存在且可归属的 dirty/untracked 产物，供 Step 5 生成任务记录 exact files。不得从旧进度推断 Git 动作。

## Step 2：预检与文件归属

对每个候选仓库读取：

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
```

停止条件：

- detached HEAD、分支不可读、未解决冲突、rebase、cherry-pick、revert 或其它非 merge 的未完成 Git 集成状态。
- `MERGE_HEAD` 存在时仅普通模式可继续，并且必须固定当前 `HEAD` / `MERGE_HEAD`、确认 `git ls-files -u` 为空、全部 staged paths 都属于 planned 且没有 retained staged；否则停止。用户/auto-loop `commit-only` 不适用。
- 普通推送会携带无法归属本次任务的历史 ahead commits。
- 无法确定 planned file 是否属于当前请求或活动任务。
- 内部 `commit-only` 发现 staged 区非空。

业务 Git 文件分为两组；普通模式的当前任务记录 exact files 按下方独立提交规则处理：

- `planned`：本轮明确归属且准备提交的 exact files。
- `retained`：当前存在、但本次明确不提交并保持原状的 dirty paths，包含计划外 untracked、unstaged、staged 文件。clean files 不进入该集合。

普通模式存在活动任务时，当前任务目录中已存在且可归属的 dirty/untracked 产物不进入业务 `planned`，也不进入 `retained`；它们与预计由 helper 更新的 `<task-dir>/task.json` 组成 Step 5 的任务记录 exact files。其他任务目录和无法归属当前任务的文件仍属于 `retained` 或风险，不得顺带提交。

普通 `PUSH` 需要在仓库间运行本地生成命令时，首次计划同时展示命令、工作目录和后续仓预计 exact files。仅在后续仓没有 retained dirty 时使用；命令必须本地、可重复且无外部副作用。

`retained` 只是内部集合名。用户可见输出统一写“保留未提交的变更（dirty）”，并逐项标注 `[untracked]`、`[unstaged]`、`[staged]`。unknown ahead、branch/upstream 异常、归属不确定等真正需要处理的事项单独进入“风险”区；普通 retained dirty 不默认视为阻塞。

普通模式允许 `retained` 存在。执行前记录计划外 staged set，提交后确认这些 staged 文件仍保持原状。用户明确要求新增文件时，重新生成计划并确认，不能在执行中静默扩大范围。

已有 merge 会提交整个索引，因此 planned 必须覆盖全部 staged paths，`retained` 中不得存在 `[staged]`；未跟踪或未暂存 retained 仍可保留。

## Step 3：展示最小计划

确认前禁止 `git add`、`git commit` 或 `git push`。计划只展示：

```markdown
## Trellis Push 计划

[<PUSH / PUSH · MERGE / COMMIT-ONLY>] <N> 个仓库 · <N> 个 commit · <N> 个文件 · 保留未提交 <N> · 风险 <N>
[无活动任务时追加：无活动任务]
顺序：<repo-a> [-> `<local generation command>`] -> <repo-b> [-> task progress]

### 完成链证据
- Check-All：<通过 / 未运行 / 已失效 / 存在 findings / blocked / 部分验证>
- Update-Spec：<no-op / written / needs-review / 未运行 / 已失效>

### 1. <repository-name>

`<commit message>`
分支：`<branch>` -> `<upstream>`
变更：<N> 个文件 · `+<adds> -<deletes>`
[已有 merge 时：父提交：`<pre-merge-head>` + `<merge-head>`]

计划提交：
- <exact files 或分组摘要>

Push：<执行 / 跳过（commit-only）>

[生成（仅普通多仓需要时显示）：前置仓成功后，在 `<working-directory>` 运行 `<exact local command>`；预计只影响 <后续仓 exact files 或分组摘要>]

### 保留未提交的变更（dirty，仅数量大于 0 时显示）
- [untracked] <path>
- [unstaged] <path>
- [staged] <path>

### 风险（仅数量大于 0 时显示）
- <Check-All / Update-Spec 风险，或 unknown ahead / branch-upstream / attribution risk>

[任务记录（仅普通模式且存在活动任务时显示）：`chore(task): update <task-name> progress` · <N> 个文件]
[仓库：<repository-name> · 分支：`<branch>` -> `<upstream>`]
[计划提交：<当前任务 exact files 或分组摘要>]
任务进度：completed=<...> | partial=<...> | next=<...>
执行：<commit -> push -> progress commit -> progress push>

确认执行请回复 `确认`。可调整：`只提交`、`修改 message`、`展开文件`。
```

展示规则：

- 单仓 `planned` 不超过 8 个文件时完整列出。
- 超过 8 个时按目录归组，最多 12 行；用户要求展开时展示同一 exact set。
- 顶部仓库/commit/file 总数包含独立任务记录提交所在 Git root、该提交及其 exact files；任务记录文件使用相同的 8 文件展示阈值和展开规则。
- 保留未提交的变更始终逐项标注 Git 状态；真正风险在独立“风险”区逐项展示。
- 完成链证据始终显示当前状态，但不重复 Check-All 报告或 Spec review 正文；`未运行`、`已失效`、findings、blocked、部分验证或 `needs-review` 同时计入风险区。
- 无活动任务或 `commit-only` 时省略进度动作。
- 不重复展示检查结果、规范复核、归档或其他阶段的详细信息。
- 生成前无法确定的内容和增删行写“生成后计算”，不得填预测值。

普通多仓只确认一次。计划已展示生成命令和预计 exact files 时，命令成功且没有出现预计列表外的新 dirty path 就沿用原确认；内容、hash 或统计变化不重问。其它计划边界变化仍按 Step 4 重新规划。

auto-loop 内部 `commit-only` 仍生成同样的逐仓执行数据用于自检和结果记录，但不再次询问用户；它不得扩展调用方给定的 exact files/message。

## Step 4：精确提交与推送

每个仓库按计划顺序执行。执行前重新检查 planned files、当前分支、upstream、冲突状态和 ahead commits；任一关键条件变化都停止当前执行并重新规划。仅 `retained` 内容变化时保留并在结果中更新说明。

计划包含本地生成命令时，前置仓成功后按计划执行命令，再复用本节现有预检。命令成功、后续仓全部 dirty paths 都在已确认的预计 exact files 内且没有其它计划边界变化时直接继续；否则停止并重新生成计划。预计文件最终 clean 时不强行提交。

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

已有 merge：

```bash
pre_merge_head="$(git rev-parse HEAD)"
merge_head="$(git rev-parse MERGE_HEAD)"
git add -- <exact existing planned files>
git add -u -- <exact deleted planned files not already staged>
git diff --cached --check
git commit -m "<confirmed message>"
```

merge 中的 `git commit` 不能携带 pathspec。现存 planned 使用 `git add --`；已删除 planned 仅在尚未进入 cached 集合时使用 `git add -u --`。提交前确认 cached path set 与 confirmed planned files 完全相等、`git ls-files -u` 为空且 `git diff --cached --check` 通过。提交后验证：

```bash
git rev-list --parents -n 1 HEAD
git diff-tree --no-commit-id --name-only -r HEAD^1 HEAD
```

结果必须恰好有两个父提交，顺序为记录的 `pre_merge_head`、`merge_head`；first-parent 文件集合必须等于 confirmed planned files。任一验证失败都停止 push，不自动重写提交。

普通模式继续推送当前分支：

```bash
git push origin <current-branch>
```

已有 upstream 且远端名称不是 `origin` 时，使用实际 upstream remote。无 upstream 时只能在计划中明确将当前分支设置到选定 remote；不能猜测目标分支。

`commit-only` 到本地提交成功即结束，不推送，也不写远端任务进度。

多仓执行失败时停止后续未开始仓库，保留已经成功的提交/推送，不做回滚。

## Step 5：同步任务进度

仅普通模式且存在活动任务时执行。全部业务仓库成功后写完整进度；已有仓库成功而后续仓库失败时写 partial 进度，明确 completed、失败位置、next 和 notes。尚未发生成功 Git 动作就失败时，不记录虚假的 completed steps；只有父仓仍可安全提交并推送时才允许记录 failure notes。

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
- 推送不会携带无法归属的历史 ahead commits。

通过 helper 写入：

```bash
python3 ./.trellis/scripts/task_progress.py write \
  --task <task-dir> \
  --progress-json '<progress-json>' \
  --json
```

然后只提交并推送首次确认的当前任务 exact files；该集合包含 helper 更新后的 `task.json`，以及首次计划时已存在且可归属的当前任务 dirty/untracked 产物：

```bash
git add -- <current-task-exact-files>
git commit --only -m "chore(task): update <task-name> progress" -- <current-task-exact-files>
git push origin <current-branch>
```

该动作属于用户已确认的普通 push 计划，不增加第二次确认。提交后必须验证 commit 只包含首次确认的当前任务 exact files；其他任务和无关 dirty/staged 文件保持原状。如果写入、提交或推送失败，不回滚已成功的业务 Git 动作，并单独报告进度同步失败。

## Step 6：结果

结果复用计划的视觉顺序，先给总览，再逐仓报告实际 commit/push，最后报告任务进度与保留 dirty：

```markdown
## Trellis Push 结果

[完成 / 部分完成 / 失败] <N> 个仓库 · <N> 个业务 commit

### 1. <repository-name>

`<short-hash> <actual commit message>`
分支：`<branch>` -> `<upstream>`
状态：<✓ 已推送 / · 仅本地提交 / ❌ 失败>

[生成：`<exact local command>` · <✓ 已完成 / ❌ 失败 / · 未执行>]

### 任务进度

状态：<✓ 已同步 · `<progress-hash>` / · 已跳过 / ❌ 同步失败>
记录：<N> 个当前任务文件
进度：completed=<...> | partial=<...> | next=<...>
[失败时追加：原因和恢复动作]

### 保留未提交的变更（dirty，仅存在时显示）
- [untracked] <path>
- [unstaged] <path>
- [staged] <path>
```

部分完成时必须明确列出已成功仓库、失败仓库/步骤、当前分支和下一恢复动作。业务结果与 progress sync 状态不得合并成一个模糊结论。

## 禁止事项

- 扩大到计划外文件或要求清理无关工作区。
- 把普通 push 中可归属当前活动任务的规划产物列为 retained，并以“finish-work 归档时再入库”为由延后首次记录。
- 执行首次计划未展示的生成命令，或生成计划外文件后仍沿用旧确认。
- 用任务进度决定是否推送代码。
- 在本 skill 内发起、终止、解决冲突或改变分支合并目标；只允许完成已就绪的 merge commit。
- 自动解决 push rejection、冲突、凭证或远端保护规则问题。
- 在业务失败后伪造已完成进度，或因进度同步失败回滚业务提交。
