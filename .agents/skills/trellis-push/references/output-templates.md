# Trellis Push 输出模板

本 reference 只定义用户可见的计划、结果和展示规则。何时读取、是否确认、能否执行以及失败恢复均由同目录 `SKILL.md` 所有。

## 计划模板

```markdown
## Trellis Push 计划

[<PUSH / PUSH · MERGE / COMMIT-ONLY>] <N> 个仓库 · <N> 个 commit · <N> 个文件 · 保留未提交 <N> · 风险 <N>

- **工作**：<任务名 | `Untracked work: <work-id>` | 无活动任务>
- **顺序**：<repo-a> [-> `<local generation command>`] -> <repo-b> [-> task progress]

### 完成链证据
- **Check-All**：<通过 / 通过（已接受风险：CHK-001,FBK-002） / 未运行 / 已失效 / 存在未处置 findings / blocked / 部分验证>
- **Update-Spec**：<no-op / written / needs-review / 未运行 / 已失效>

### 1. <repository-name>

- **Message**：`<commit message>`
- **分支**：`<branch>` -> `<upstream>`
- **变更**：<N> 个文件 · `+<adds> -<deletes>`
- **父提交**：`<pre-merge-head>` + `<merge-head>`（仅已有 merge 时显示）
- **Push**：<执行 / 跳过（commit-only）>

计划提交：
- <exact files 或分组摘要>

[生成（多仓需要时显示）：前置仓成功后，在 `<working-directory>` 运行 `<exact local command>`；预计只影响 <后续仓 exact files 或分组摘要>]

### 保留未提交的变更（dirty，仅数量大于 0 时显示）
- <按共用规则逐项展示或按仓库、目录、Git 状态汇总数量>
- [staged] <repository/path>（仅存在时逐项显示，兼有未暂存修改时同时标注 [unstaged]）

### 风险（仅数量大于 0 时显示）
- <Check-All / Update-Spec 风险，或 unknown ahead / branch-upstream / attribution risk>

### 任务记录（仅普通模式且存在活动任务时显示）

- **Message**：`chore(task): update <task-name> progress` · <N> 个文件
- **仓库**：<repository-name> · 分支：`<branch>` -> `<upstream>`
- **计划提交**：<当前任务 exact files 或分组摘要>
- **进度**：completed=<...> | partial=<...> | next=<...>
- **执行**：<business commit/push -> `task_progress.py write --complete` -> task-record commit -> task-record push>

确认执行请回复 `确认`。可调整：`只提交`、`修改 message`、`展开文件`、`展开保留变更`。
```

## 共用展示规则

- 计划与结果模板中的字段行必须使用 `- **字段**：值` 列表项。这些行在 Markdown 段落内会被折叠成一段，不得改回裸段落行，也不得依赖行尾空格换行。
- 计划中的「任务记录」是与各仓库区平级的独立 `###` 小节，仅普通模式且存在活动任务时整节展示；不再用方括号条件行代替小节标题。
- 计划中单仓 `planned` 不超过 8 个文件时完整列出。
- 超过 8 个时按目录归组，最多 12 行；用户要求展开时展示同一 exact set。
- 顶部仓库/commit/file 总数包含独立任务记录提交所在 Git root、该提交及其 exact files；任务记录文件使用相同的 8 文件展示阈值和展开规则。
- 计划中的保留变更按仓库计数：不超过 8 项时逐项标注 `[untracked]`、`[unstaged]`、`[staged]`；超过 8 项时将非 staged 项按目录与 Git 状态汇总数量，每仓最多 12 行，必要时合并到上级目录。同一路径计数一次，兼有 staged/unstaged 时同时标注。
- 计划中的计划外 staged 项始终逐项单列，不计入分组摘要；计划和结果中的真正风险均在独立“风险”区逐项展示，不受行数限制。分组、展开均只改变展示，不改变 exact set 或确认范围。
- 用户要求“展开保留变更”时在对话中列出同一 exact set 与 Git 状态，不生成清单附件；“展开文件”仍指 planned files。
- 计划的完成链证据始终显示当前状态，但不重复 Check-All 报告或 Spec review 正文；`未运行`、`已失效`、任一未处置 `CHK-*` / `FBK-*`、blocked、部分验证或 `needs-review` 同时计入风险区。已接受风险的问题也必须按 ID、严重度和影响进入风险区，但不得改标为阻断 finding。`[上线后验证]` 作为非阻断风险逐项保留动作、环境/责任边界和预期结果，不改变 Check-All 状态，并注明由既有 `trellis-release` / `release.md` 流程承接。
- 无活动 task、untracked 或 `commit-only` 时省略进度动作。
- 不重复展示检查结果、规范复核、归档或其他阶段的详细信息。
- 生成前无法确定的内容和增删行写“生成后计算”，不得填预测值。

## 结果模板

结果先给结论，每仓一行报告实际提交和推送状态，再报告任务记录与保留变更。全部成功时不重复文件清单、commit message、生成命令、文件统计或完整进度字段：

```markdown
## Trellis Push 结果

[推送成功 / 仅本地提交成功 / 部分完成 / 失败]

- **<repository-name>**：`<branch> → <upstream>` · `<short-hash[, short-hash...]>` · <已推送 / 仅本地 / 失败 / 未执行>
- **任务记录**：`<task-record-hash>` · <completed 已同步 / partial 已同步，仍为 in_progress / commit 待恢复 / push 待恢复 / 同步失败>
- **保留未提交的变更（dirty）**：<每仓数量与实际核对结论>

### 失败与恢复（仅部分完成或失败时显示）

- **失败位置**：<仓库/步骤、失败原因；生成失败时包含命令>
- **已保留**：<成功提交/推送及未完成现场；未执行的后续步骤>
- **下一步**：<精确恢复动作>

### 风险（仅存在时显示）

- <仍适用的风险、异常或未核验项；按共用规则保留必要细节>
```

## 结果补充规则

- 仅普通模式且存在活动任务时显示“任务记录”行；没有实际 commit hash 时省略 hash，不填占位值。用户 `commit-only` 的仓库行只写当前分支与本地提交，不显示推送箭头或暗示已同步远端。
- untracked 结果用“无任务状态”行替代“任务记录”，展示 work id 与 `<已清理/保留待恢复>`；不生成或暗示 task progress commit。没有保留变更、失败或风险时省略对应行或章节。
- 部分完成时必须明确列出已成功仓库、失败仓库/步骤、当前分支和下一恢复动作。业务结果与 progress sync 状态不得合并成一个模糊结论。
- 普通成功结果必须确认本任务产生的当前任务目录变更 clean。其它 retained dirty（含计划外 staged）仍逐项核验，已核对保持原状时每仓只报告数量与结论，不重复清单。异常或未核验项列出路径、实际状态和处理情况，不得笼统声称全部保持原状；用户要求详情时再展示实际文件、message、命令或进度，展开文件仍沿用共用规则。
- Git 成功不消除现有风险；结果的“风险”区保留仍适用的完成链风险、已接受问题与 `[上线后验证]`，不得因成功摘要而省略。
- helper 成功但任务记录 commit 失败时，结果写“任务记录 commit 待恢复”，说明本地 `completed` 与 exact task dirty 已保留；任务记录 commit 成功但 push 失败时写“任务记录 push 待恢复”，说明 clean ahead commit 已保留。两种情况都不得暗示需要重复业务提交或 helper 写入。
- validated auto-loop local completion 不渲染本模板，也不得被普通结果文案描述为任务记录 push 待恢复。
