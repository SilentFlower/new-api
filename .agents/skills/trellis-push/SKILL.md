---
name: trellis-push
description: "Commit + push across configured repos with optional merge-to-target."
---
# Push — 提交并推送（可选合并到目标分支）

一键完成 commit → push → 可选 merge 到目标分支（通常是测试线）→ push 目标分支 → 切回。

支持多仓库（frontend / backend），merge 目标分支记录在 `.trellis/config.yaml` 的 `packages.<name>.merge_target` 中。

---

## 配置

目标分支存储在 `.trellis/config.yaml` 的 packages 配置中：

```yaml
packages:
  frontend:
    path: iqs-front-human
    git: true
    merge_target: test      # 首次 push 时询问后自动写入
  backend:
    path: iqs
    git: true
    merge_target: test
```

> 首次运行时如果 `merge_target` 不存在，会在 merge 步骤询问目标分支并回写到 `config.yaml`。
> 如需修改，直接编辑 `config.yaml` 或通过 `/trellis-push` 传入 `--reconfigure` 语义。

---

## 执行步骤

### Step 0: 读取配置

读取 `.trellis/config.yaml` 中的 `packages` 配置，识别所有 `git: true` 的仓库及其 `merge_target`（如果已配置）。

### Step 0.5: 读取活动任务上下文（可选）

为后续 Step 3「写入任务进度快照」准备输入。本步骤**不阻塞**主流程：任何读取失败都按"无任务上下文"继续，不要中断 push。

```bash
# 在仓库根目录（含 .trellis/ 的项目根，不是 package 子目录）
python3 ./.trellis/scripts/task.py current
```

- **无活动任务**（命令退出码非 0 或输出为空）：在内存里标记 `active_task=None`，**跳过 Step 3**，把本次 push 当作纯 chore push 处理。
- **有活动任务**：解析输出拿到 task 路径（形如 `.trellis/tasks/MM-DD-name/`），并读取下列内容供 Step 3 使用：
  - `<task_dir>/implement.md`（如有）— 步骤清单，AI 推断进度的主依据
  - `<task_dir>/task.json` 的 `last_push_snapshot` 字段（如有）— 上次推送时的进度基线
  - `<task_dir>/task.json` 的 `base_branch` 字段 — 用于 `git log <base_branch>..HEAD` 圈定本任务的 commit 范围

> 本步骤只读不写。如果读取异常（task.json 损坏、implement.md 缺失等），Step 3 退化为"完全靠用户口述进度"，不影响 push 主流程。

### Step 1: 检测变更

读取 `.trellis/config.yaml` 中的 `packages` 配置，对每个 git 仓库检测变更：

```bash
# 对每个 package
cd <package_path>
git status --short
```

列出有变更的仓库。如果所有仓库都没有变更，提示用户并终止。

如果只有部分仓库有变更，只处理有变更的仓库。

### Step 2: 逐仓库处理

对每个有变更的仓库，依次执行以下操作：

#### 2.1 展示变更摘要

```bash
cd <package_path>
git diff --stat
git diff --name-only
```

#### 2.2 暂存文件

展示要暂存的文件列表，**获得用户确认后**再暂存。

```bash
git add <具体文件列表>
```

> **[!] 禁止使用 `git add -A` 或 `git add .`**
> 必须明确列出文件，避免误提交敏感文件（.env、credentials 等）。

#### 2.3 生成 commit message 并提交

分析变更内容，生成符合项目风格的 commit message：
- 读取最近 5 条 commit 参考风格
- 类型前缀：`feat` / `fix` / `chore` / `refactor` 等
- 简短描述变更内容
- 使用中文描述

```bash
git commit -m "<type>(<scope>): <description>"
```

#### 2.4 Push 当前分支

```bash
git push origin <current_branch>
```

如果远程没有该分支，使用 `-u` 建立跟踪：

```bash
git push -u origin <current_branch>
```

#### 2.5 询问是否 Merge 到目标分支（可选）

Push 完成后，检查该 package 是否已配置 `merge_target`：

**已配置 `merge_target`**：

```markdown
<package> 已推送到 <current_branch>。是否合并到 <merge_target>？

1. 是，合并到 <merge_target>
2. 否，跳过
```

**未配置 `merge_target`（首次）**：

```markdown
<package> 已推送到 <current_branch>。是否需要合并到其他分支（如测试线）？

1. 是，请输入目标分支名
2. 否，跳过
```

如果用户输入了目标分支名，**将其回写到 `.trellis/config.yaml`** 对应 package 的 `merge_target` 字段，下次自动使用。

**如果用户选择合并**：

```bash
# 切换到目标分支并拉取最新
git checkout <target_branch>
git pull origin <target_branch>

# 合并当前开发分支
git merge <current_branch> --no-edit

# Push 目标分支
git push origin <target_branch>

# 切回开发分支
git checkout <current_branch>
```

> **[!] 如果 merge 出现冲突：**
> 1. 立即停止，展示冲突文件列表
> 2. 询问用户：手动解决 / 中止 merge
> 3. **绝对不能** `git merge --abort` 后静默跳过

**如果用户选择跳过**：直接进入下一个仓库或输出结果。

### Step 3: 写入任务进度快照（可选）

仅当 Step 0.5 识别到活动任务时执行；否则跳过本步直接进入 Step 4。

**目的**：让下次新会话进来时能感知"任务做到哪一步了"。配套机制由 `.trellis/workflow.md` 的 `[workflow-state:no_task]` 块里的 `push-progress-recovery` guard 触发（见 3.4）。

**架构说明**：`.trellis/tasks/<task>/task.json` 在**父仓**（含 `.trellis/` 的项目根目录）的 git 跟踪范围内，与子仓（frontend / backend）独立。因此本 Step 完成后必须**额外 commit + push 父仓**（见 3.3），才能让 snapshot 真正落到 remote，跨机器恢复有效，且不留脏工作区。

#### 3.1 AI 推断进度

收集以下信号：

- Step 0.5 读到的 `implement.md` 步骤清单（如缺失则只能靠 commit history 粗略推断）
- 各子仓 `git log <base_branch>..HEAD --oneline`（任务分支自分叉以来的所有 commit，含本次刚 push 的）
- 上次 `last_push_snapshot`（如有）— 作为基线，重点判断"上次之后又做了什么"

基于以上信号，AI **主动给出一个 draft**（不要让用户从零列步骤），例如：

```markdown
任务进度推断（请确认）：

- ✅ Step 1-3 已完成（frontend abc1234 / backend def5678 覆盖了对应改动）
- 🟡 Step 4 部分完成 — 看到 README.md 改了 2 处，implement.md 写要改 4 处
- ⬜ Step 5 未跑

本次 push 后停在 **Step 4（部分）**，下一步 **Step 5（校验）**。

确认（yes）/ 调整（说明具体改动）/ 跳过快照（skip）
```

#### 3.2 写入 task.json

用户确认（yes 或调整后的版本）→ 在 `<task_dir>/task.json` 写入 / 更新 `last_push_snapshot` 字段：

```json
"last_push_snapshot": {
  "snapshot_at": "<ISO 8601 时间戳>",
  "branch": "<任务分支名>",
  "pushed_commits": {
    "frontend": "abc1234",
    "backend": "def5678"
  },
  "completed_steps": ["Step 1", "Step 2", "Step 3"],
  "partial_step": "Step 4 (README 改了 2/4 处)",
  "next_step": "Step 5 (校验)",
  "notes": "<可选：用户补充说明>"
}
```

字段语义：
- `snapshot_at`：写快照的时间戳（必填）
- `branch`：任务分支名（必填，多仓不同分支时改成字典 `{"frontend": "...", "backend": "..."}`）
- `pushed_commits`：本次刚 push 的最新 commit 短 hash，按 package 名分键（必填）
- `completed_steps`：implement.md 中已完成的 step 名数组（必填）
- `partial_step` / `next_step` / `notes`：可选

写入方式：读 `task.json` → 解析 JSON → 设置 `last_push_snapshot` 字段 → 保留其它字段原样 → 写回（保持原有 indent，通常 2 空格）。**不要**覆盖整个 task.json，只更新一个字段。

> 用户回复 `skip` 时不写入 task.json，**整个 Step 3 终止**（包括跳过 3.3 父仓提交），直接进 Step 4。

#### 3.3 父仓 commit + push（同步到 remote）

写完 task.json 后，**必须**把这个改动 commit + push 到父仓 remote，否则：
- 工作区残留脏 task.json
- 跨机器 / 重新 clone 时拿不到 snapshot
- 父仓 git log 缺失任务进度的演进记录

```bash
# 切到父仓根目录（含 .trellis/ 的目录，不是 package 子目录）
cd <project_root>

# 先看父仓 status，确认变更只有 task.json
git status --short
```

**[!] 如果父仓 status 显示 task.json 之外还有其他改动**：停下来询问用户：是否一并提交 / 拆分 / 暂存？不要静默打包。

```bash
# 仅 stage 这一个文件，避免误带父仓其他改动
git add .trellis/tasks/<task_dir>/task.json

# commit（message 参考项目现有风格，如 chore(task): / [UPDATE] 等）
git commit -m "chore(task): update <task_name> push snapshot"
```

检查父仓是否配置 remote：

```bash
git remote -v | grep -E "^origin\s+"
```

- **有 remote**：`git push origin <current_branch>`（首次用 `git push -u origin <current_branch>`）
- **无 remote**：仅本地 commit，跳过 push 步骤，提示用户"父仓未配 remote，snapshot 仅本地保存"

#### 3.4 新会话恢复（被动机制，无需在本步操作）

写入并 push 后，下次新会话进来时：

1. SessionStart 检测到无 active task pointer → 输出 `<task-status>Status: NO ACTIVE TASK</task-status>`
2. UserPromptSubmit 每轮注入 `[workflow-state:no_task]` 块，含 skill-garden 的 `push-progress-recovery` guard
3. AI 看到 guard → 扫描 `.trellis/tasks/*/task.json` 找 `status=in_progress` 的任务 → 读 `last_push_snapshot` → 主动告诉用户「发现未完成任务 X，上次 push 完成到 Step Y，下一步 Z」并建议 `task.py start <task>` 恢复

### Step 4: 输出结果

```markdown
## Push 结果

| 仓库 | 分支 | 目标 | commit | 状态 |
|------|------|------|--------|------|
| frontend | v1.3 | test | abc1234 feat(...): ... | ✅ 已合并 |
| backend | v1.3 | test | def5678 fix(...): ... | ⏭️ 跳过合并 |

所有变更已推送到目标分支。
```

若 Step 3 写入了 `last_push_snapshot`，在结果末尾追加一行：

```markdown
任务进度快照已写入 `<task_dir>/task.json`：完成 Step 1-3，下一步 Step 5。
```

---

## 语义参数（通过自然语 / skill args 传入）

| 语义 | 说明 | 用户怎么说 |
|------|------|-----------|
| 默认 | 自动检测所有有变更的仓库并处理 | `/trellis-push` |
| 指定仓库 | 只处理指定仓库 | 「只 push 前端」/「push frontend」 |
| 重新配置 | 重新询问目标分支 | 「重新配置 push 目标分支」/「reconfigure push」 |
| 临时目标 | 临时指定目标分支（不修改配置） | 「push 到 hotfix 分支」 |

---

## 安全机制

1. **暂存确认** — 每个仓库暂存前展示文件列表，用户确认
2. **commit message 确认** — 展示生成的 message，用户可修改
3. **merge 冲突处理** — 冲突时暂停，不静默跳过
4. **不碰主分支** — 如果目标分支是 `master` / `main`，额外警告确认
5. **不使用 force push** — 始终使用普通 push

---

## 反模式（避免）

- ❌ `git add -A`（可能误提交敏感文件）
- ❌ merge 冲突后静默 abort（用户需要知道）
- ❌ 未经确认直接 commit（必须让用户看到 message）
- ❌ force push 到目标分支
- ❌ 在目标分支上直接开发（只 merge，不在目标分支上改代码）
