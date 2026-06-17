---
name: trellis-push
description: "提交并推送配置仓库，可选合并到目标分支，并同步任务进度快照。"
---
# Push — 提交并推送（可选合并到目标分支）

一键完成 commit → push → 可选 merge 到目标分支 → 写入任务进度快照 → 输出结果。

核心原则：**先计划、一次确认、后执行**。在任何 `git add`、`git commit`、`git push`、`git merge` 之前，必须先展示完整执行计划，并获得用户确认。

支持多仓库（frontend / backend 等），merge 目标分支记录在 `.trellis/config.yaml` 的 `packages.<name>.merge_target` 中。

---

## 配置

目标分支存储在 `.trellis/config.yaml` 的 packages 配置中：

```yaml
packages:
  frontend:
    path: iqs-front-human
    git: true
    merge_target: test      # 首次指定后可回写
  backend:
    path: iqs
    git: true
    merge_target: test
```

> 首次运行时如果 `merge_target` 不存在，默认跳过 merge；如果用户在统一执行计划中明确指定目标分支，可将其回写到 `config.yaml`。如需修改，直接编辑 `config.yaml` 或通过 `/trellis-push` 传入 `--reconfigure` 语义。

---

## 总流程

```text
Step 0  读取配置、模式与活动任务上下文
Step 1  预检并收集所有候选仓库状态
Step 2  生成统一执行计划（业务提交 + push/merge + snapshot + 父仓 bookkeeping）
Step 3  用户确认后执行业务仓库 commit / push
Step 4  按已确认计划执行可选 merge
Step 5  写入已确认的任务进度快照，并补齐运行后字段
Step 6  输出结果
```

除非出现“计划变化、执行失败重试、用户调整 snapshot、父仓 staged/冲突/task 文件预脏等 bookkeeping 安全条件不满足、merge 冲突”等情况，否则不要在执行中途追加新的确认问题。

---

## Step 0: 读取配置、模式与活动任务上下文

### 0.1 解析语义模式

支持自然语言或 skill args 传入：

| 语义 | 说明 | 用户怎么说 |
|------|------|-----------|
| 默认 | 自动检测所有有变更的仓库并处理 | `/trellis-push` |
| commit-only | 只 commit 不 push，跳过 push / merge | `只提交不推` / `commit-only` |
| 指定仓库 | 只处理指定仓库 | `只 push 前端` / `push frontend` |
| 重新配置 | 本次允许重新指定 merge 目标，并回写 `merge_target` | `重新配置 push 目标分支` / `reconfigure push` |
| 临时目标 | 本次临时指定 merge 目标，不修改配置 | `push 到 hotfix 分支` |
| snapshot-only | 不提交业务代码，只写入 / 同步任务进度快照 | `只更新 snapshot` / `snapshot-only` |

`snapshot-only` 只在用户明确要求时使用。它仍必须展示统一执行计划；`pushed_commits` 取计划中确认的现有 commit，或按用户说明记录。

### 0.2 发现候选 Git 仓库

读取 `.trellis/config.yaml` 中的 `packages` 配置，但不要把 `git: true` 当作唯一仓库发现规则。`git: true` 主要用于 Trellis session context 展示独立包仓库状态；`trellis-push` 必须以实际 Git root 为准发现可提交仓库：

- **父仓根目录（含 `.trellis/` 的项目根）始终作为一个候选仓库**，即使它没有配置 `git: true`。
- packages 中配置的路径如果自身是独立 Git root（存在 `.git` 目录或 submodule 的 `.git` 文件），也作为候选仓库。
- 如果 package 路径位于父仓内部但不是独立 Git root，不要把它当作单独仓库；它的变更归父仓候选项处理。
- 如果用户指定了仓库，只保留匹配的候选项；指定父仓可用 `root`、`parent`、`main repo` 或默认 package 名。
- 同一个 Git root 只保留一次，避免 `path: .` 与父仓重复。

merge 目标仍从对应 package 的 `merge_target` 读取；父仓没有 package 配置时，默认不设置 merge 目标。

### 0.3 读取活动任务上下文（可选）

为生成 snapshot 草案准备输入。本步骤**不阻塞**主流程：任何读取失败都按“无任务上下文”继续，不要中断 push。

```bash
# 在仓库根目录（含 .trellis/ 的项目根，不是 package 子目录）
python3 ./.trellis/scripts/task.py current
```

- **无活动任务**（命令退出码非 0 或输出为空）：在计划中标记 `snapshot: 跳过（无活动任务）`。
- **有活动任务**：解析输出拿到 task 路径（形如 `.trellis/tasks/MM-DD-name/`），并读取下列内容：
  - `<task_dir>/implement.md`（如有）— 步骤清单，AI 推断进度的主依据
  - `<task_dir>/task.json` 的 `last_push_snapshot` 字段（如有）— 上次推送时的进度基线
  - `<task_dir>/task.json` 的 `base_branch` 字段 — 用于 `git log <base_branch>..HEAD` 圈定本任务 commit 范围

---

## Step 1: 预检并收集仓库状态

对每个候选 Git 仓库收集：

```bash
cd <package_path>
git status --short
git branch --show-current
git rev-parse --abbrev-ref --symbolic-full-name @{u} 2>/dev/null || true
git diff --stat
git diff --name-only
git log --oneline -5
```

如果有活动任务和 `base_branch`，再收集：

```bash
git log <base_branch>..HEAD --oneline
```

预检规则：

- 所有候选仓库都无变更，且不是 `snapshot-only`：提示用户并终止。
- 有未合并状态、rebase 状态或 merge 冲突残留：立即停止，展示状态；不要继续生成执行计划。
- 当前分支为空、detached HEAD、或无法确认分支：停止并说明原因。
- dirty 文件必须按来源分组：**AI 本轮编辑** 与 **未识别 dirty 文件**。未识别 dirty 文件默认不纳入提交计划。
- 如果父仓（含 `.trellis/` 的仓库）后续要写 snapshot，先记录父仓当前 `git status --porcelain`，供 Step 5 判断：
  - 是否存在未合并 / 冲突状态；
  - 是否存在与本次 bookkeeping 无关的 staged 文件；
  - `<task_dir>/task.json` 是否在写入前已经 dirty；
  - reconfigure 场景下 `.trellis/config.yaml` 是否在写入前已经 dirty 且未在统一计划中确认。
  父仓存在无关、未暂存 dirty 文件本身不阻塞 snapshot；这些文件只需要在计划或结果中提示会保留未提交。

---

## Step 2: 生成统一执行计划

这是唯一的常规确认点。**确认前禁止执行任何 `git add` / commit / push / merge / task.json 写入。**

### 2.1 生成业务提交计划

对每个有变更的仓库生成：

- 仓库名、路径、当前分支、上游分支
- AI 本轮编辑且拟纳入提交的具体文件列表
- 未识别 dirty 文件列表（默认不纳入提交）
- 用户明确要求纳入的未识别文件列表（如有）
- 草拟 commit message
- push 计划（默认 / commit-only / 已有 upstream / 需要 `-u`）
- merge 计划（跳过 / 合并到配置目标 / 合并到临时目标 / 重新配置后回写）

commit message 生成规则：

- 读取最近 5 条 commit 参考风格。
- 类型前缀使用 `feat` / `fix` / `docs` / `chore` / `refactor` 等。
- 简短描述变更内容。
- 默认使用中文描述，除非项目历史明显使用英文。

### 2.2 生成 snapshot 草案

仅当有活动任务，或用户明确要求 `snapshot-only` 时生成。snapshot 的语义内容并入统一确认，不再默认二次询问。

收集信号：

- `implement.md` 步骤清单（如缺失，则说明推断依据不足）
- 各仓库 `git log <base_branch>..HEAD --oneline`
- 上次 `last_push_snapshot`（如有）
- 本次业务提交计划中的变更内容

计划中展示：

```markdown
任务进度快照计划：
- task: <task_dir>
- completed_steps: ["Step 1", "Step 2"]
- partial_step: "Step 3（如有）"
- next_step: "Step 4（如有）"
- notes: "<可选说明>"
- branch: 执行成功后按实际分支补齐
- pushed_commits: 执行成功后按实际 commit hash 补齐
- snapshot_at: 执行成功后写入当前 ISO 8601 时间
- bookkeeping: 只提交 `<task_dir>/task.json`
```

字段语义保持兼容：

```json
"last_push_snapshot": {
  "snapshot_at": "<ISO 8601 时间戳>",
  "branch": "<任务分支名，或多仓字典>",
  "pushed_commits": {
    "frontend": "abc1234",
    "backend": "def5678"
  },
  "completed_steps": ["Step 1", "Step 2"],
  "partial_step": "Step 3（可选）",
  "next_step": "Step 4（可选）",
  "notes": "可选说明"
}
```

> `pushed_commits`、`snapshot_at`、实际 branch / commit hash 是运行后字段。用户确认的是 snapshot 的语义内容和写入动作；执行成功后由 AI 按实际结果补齐。

commit-only 模式下，字段名仍保持 `pushed_commits` 以兼容恢复逻辑；值记录本次生成的本地 commit hash，并在 `notes` 中注明“commit-only：本地已提交，未推送”。

### 2.3 展示确认模板

使用以下格式向用户展示完整计划：

```markdown
## trellis-push 执行计划

模式：<默认 / commit-only / snapshot-only / 指定仓库 / reconfigure / 临时目标>

### 业务仓库

1. <package> (`<path>`)
   - 分支：<current_branch> -> <upstream 或 origin/current_branch>
   - AI 本轮编辑，拟提交：
     - <file-a>
     - <file-b>
   - 未识别 dirty，默认不提交：
     - <unknown-file 或 无>
   - 用户要求纳入的未识别文件：
     - <file 或 无>
   - commit message：`<type>(<scope>): <description>`
   - push：<执行 / 跳过（commit-only）>
   - merge：<跳过 / 合并到 target / 临时合并到 target / 重新配置为 target>

### 任务快照

- <跳过原因，或 snapshot 草案>

### 父仓 bookkeeping

- <跳过原因，或仅提交 `<task_dir>/task.json`>
- 无关未暂存 dirty：<list 或 无>（保留未提交，不阻塞 snapshot commit）
- 无关 staged / 冲突 / 目标文件预脏：<list 或 无>（有则停止，需处理后重新计划）

确认后将按上述计划执行。回复 `ok` / `行` / `确认` 执行；回复 `skip snapshot` 跳过快照；回复修改意见则先更新计划；回复 `manual` / `我自己来` 则停止。
```

确认规则：

- 用户确认前不得暂存。
- 用户修改 commit message、merge 目标或 snapshot 草案时，更新计划并重新展示确认。
- 用户要求纳入未识别 dirty 文件时，必须明确列出这些文件后重新确认。
- 用户拒绝文件范围或选择手动处理时，立即停止，不执行任何 git 写操作。

---

## Step 3: 按计划执行业务 commit / push

确认后，对每个业务仓库按计划执行。

### 3.1 执行前复核

执行前重新运行：

```bash
git status --short
git diff --name-only
```

如果文件状态与确认计划不一致，停止并说明“计划已变化，需要重新确认”。不要临时扩展提交范围。

### 3.2 暂存与提交

只暂存计划中明确列出的文件：

```bash
git add <具体文件列表>
git commit -m "<type>(<scope>): <description>"
```

禁止：

- `git add -A`
- `git add .`
- 暂存确认计划之外的文件
- 未经确认修改 commit message

### 3.3 Push 当前分支

commit-only 模式跳过本节。

```bash
git push origin <current_branch>
```

如果远程没有该分支，且计划已说明需要建立 upstream：

```bash
git push -u origin <current_branch>
```

push 失败时立即停止，展示失败原因和已完成仓库状态。不要自动 force push。

---

## Step 4: 按计划执行可选 merge

只有在统一执行计划中明确写了 merge 目标时才执行。不要在 push 后临时追问是否 merge。

执行前再次确认当前工作区干净：

```bash
git status --short
```

如果不干净，停止并说明原因。

按计划执行：

```bash
git checkout <target_branch>
git pull origin <target_branch>
git merge <current_branch> --no-edit
git push origin <target_branch>
git checkout <current_branch>
```

如果 merge 目标是 `main` / `master`，计划中必须已经包含额外警告和用户确认。

如果 merge 出现冲突：

1. 立即停止，展示冲突文件列表。
2. 询问用户：手动解决 / 中止 merge。
3. 绝对不能 `git merge --abort` 后静默跳过。

如果用户选择 `reconfigure` 并确认回写目标分支，将 `.trellis/config.yaml` 对应 package 的 `merge_target` 更新为确认的目标；该配置变更属于父仓 dirty 文件，必须在 Step 5 的父仓检查里显式处理。

---

## Step 5: 写入任务进度快照与父仓 bookkeeping

仅当统一执行计划中确认了 snapshot 时执行；否则跳过。

### 5.1 补齐运行后字段

业务 commit / push 完成后，按实际结果补齐：

- `snapshot_at`：当前 ISO 8601 时间戳
- `branch`：实际分支；多仓不同分支时使用字典
- `pushed_commits`：各 package 的短 hash
- `notes`：保留已确认 notes；commit-only 模式追加“本地已提交，未推送”

写入前先复核目标文件：

```bash
git status --porcelain -- <task_json_path> [".trellis/config.yaml"]
```

- `<task_json_path>` 如果在本次写入前已经 dirty，且该 dirty 未在统一计划中确认：立即停止并说明原因，避免覆盖或混入别人对同一任务文件的修改。
- reconfigure 场景下，`.trellis/config.yaml` 如果在本次回写前已经 dirty，且未在统一计划中确认：立即停止并说明原因。

写入方式：

1. 读 `<task_dir>/task.json`
2. 解析 JSON
3. 只设置 / 更新 `last_push_snapshot` 字段
4. 保留其它字段原样
5. 写回并保持原有缩进（通常 2 空格）

不要覆盖整个 `task.json`。

### 5.2 父仓 status 检查

写完 snapshot 后，在父仓根目录执行：

```bash
git status --porcelain
git diff --name-only --cached
```

bookkeeping 的自动处理范围只允许统一计划中确认过的文件：

- `<task_dir>/task.json`
- 如果 Step 4 选择了 reconfigure：`.trellis/config.yaml`

检查规则：

- 如果存在未合并路径、rebase 状态或 merge 冲突残留：立即停止，展示状态。
- 如果 `git diff --name-only --cached` 显示除上述允许文件之外的 staged 文件：立即停止，说明这些 staged 文件会被普通 commit 混入，必须先处理或重新确认。
- 如果存在除上述允许文件之外的**未暂存** dirty 文件：不要阻塞；保留它们未暂存，并在结果中提示“未纳入 snapshot commit”。
- 如果允许文件之外的 dirty 同时被 staged 和 unstaged 修改，按 staged 风险处理：立即停止。

如果父仓也是本次业务仓库，snapshot / config bookkeeping 仍然使用单独 commit，不和业务 commit 混合。

### 5.3 提交并推送父仓 bookkeeping

```bash
git add <task_json_path> [".trellis/config.yaml"]
git commit --only -m "chore(task): update <task_name> push snapshot" -- <task_json_path> [".trellis/config.yaml"]
```

`git commit --only ... -- <paths>` 的目的是把 bookkeeping commit 限定到当前任务的 `task.json`（以及已确认的 `config.yaml`），即使父仓还有无关未暂存 dirty 文件，也不会把它们带入本次提交。执行前仍必须确认 staged 区没有无关文件；如果本地 Git 对 `--only` 参数不兼容，只有在 staged 区确认仅包含上述允许文件时，才可退回普通 `git commit -m ...`。

检查父仓是否配置 remote：

```bash
git remote -v | grep -E "^origin\s+"
```

- 有 remote：`git push origin <current_branch>`；首次使用 `git push -u origin <current_branch>`。
- 无 remote：仅本地 commit，提示用户“父仓未配 remote，snapshot 仅本地保存”。

---

## Step 6: 输出结果

使用结果表汇总：

```markdown
## Push 结果

| 仓库 | 分支 | 目标 | commit | 状态 |
|------|------|------|--------|------|
| frontend | v1.3 | test | abc1234 feat(...): ... | 已合并 |
| backend | v1.3 | - | def5678 fix(...): ... | 已推送，跳过合并 |

任务进度快照：已写入 `<task_dir>/task.json`，完成 Step 1-3，下一步 Step 5。
父仓同步：已提交并推送 `chore(task): update <task_name> push snapshot`。
父仓存在未暂存 dirty 时补充：这些文件已保留未暂存，未纳入 snapshot commit：<list>
```

如果部分仓库已成功、后续失败，必须明确列出：

- 已完成的 commit / push / merge
- 失败位置
- 当前所在分支
- 用户下一步需要处理什么

---

## 安全机制

1. **统一执行计划确认** — 文件列表、commit message、push/merge 意图、snapshot 草案一起确认。
2. **未识别 dirty 文件隔离** — 默认不纳入提交，除非用户明确确认。
3. **执行前复核** — git 状态与计划不一致时停止，不临时扩展范围。
4. **snapshot 合并确认** — 语义字段执行前确认，运行后字段执行后补齐。
5. **父仓 bookkeeping 隔离** — 只提交已确认的 `task.json` / `config.yaml`；无关未暂存 dirty 文件保留未提交并提示，不阻塞；无关 staged 文件、冲突状态、目标文件预脏必须停止。
6. **merge 冲突处理** — 冲突时暂停，不静默跳过。
7. **主分支保护** — 目标分支是 `master` / `main` 时必须在计划中额外警告确认。
8. **不使用 force push** — 始终使用普通 push。

---

## 反模式（避免）

- ❌ 在展示统一执行计划前执行 `git add` / commit / push / merge
- ❌ `git add -A` 或 `git add .`
- ❌ 把未识别 dirty 文件静默纳入提交
- ❌ commit message 未展示给用户就提交
- ❌ push 后临时追问 merge，而不是在计划中提前确认
- ❌ snapshot 语义内容未确认就写入 task.json
- ❌ 父仓 snapshot commit 混入业务代码提交
- ❌ 父仓存在无关未暂存 dirty 就跳过 snapshot，尽管可以只提交目标 `task.json`
- ❌ 父仓已有无关 staged 文件时仍执行 snapshot commit
- ❌ merge 冲突后静默 abort 或跳过
- ❌ force push 到当前分支或目标分支
- ❌ 在目标分支上直接开发（只 merge，不在目标分支上改代码）
