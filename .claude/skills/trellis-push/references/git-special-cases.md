# 特殊 Git 路径

仅在多仓/生成链、已有 merge 或待推送历史需要 GC 审计时读取对应节；预检、计划和执行前均核对适用条件。

### 历史自动 GC 提交

仅普通推送及其任务记录发布恢复使用这一默认许可。对 `@{u}..HEAD` 的每个完整提交哈希逐个判断：已由当前任务证据归属的提交沿用原规则；候选自动 GC 提交调用当前平台 `trellis-push` skill 目录中的只读脚本：

```bash
python3 <skill-dir>/scripts/verify_gc_commit.py --repo <repository-root> --commit <full-sha>
```

`<skill-dir>` 是当前实际加载的 `.agents/skills/trellis-push` 或 `.claude/skills/trellis-push`，不能从另一平台借用脚本。只有退出码为 0、JSON `status=verified` 且返回的完整 `commit` 与请求哈希一致，才把该提交标为“已验证自动 GC”。脚本逐项验证单父和完整固定消息、无重命名推断的 Git 树、顶层 closed 任务与 UTC 月份归档文件的同 blob/模式映射；支持同批多任务和已有相同归档的去重。它不读取或修改工作区，不凭提交消息单独放行。脚本缺失、执行失败、JSON 损坏、内容改写或任一证据不符都保持未知 ahead 并停止普通推送。

记录已验证 GC 的完整哈希及仓库，作为计划和执行前复核基线。它是用户对纯自动 GC 的默认许可，不属于业务 `planned` 或任务记录 exact files，不增加专项确认，也不自行触发 GC 或单独 push。正常业务提交仍须展示并确认一次 Push 计划；用户 `commit-only` 和 auto-loop 内部 `commit-only` 不因本规则扩大授权。

## 多仓与本地生成

普通 `PUSH` 或 auto-loop 内部 `commit-only` 需要在仓库间运行本地生成命令时，计划必须包含命令、工作目录、依赖顺序和后续仓预计 exact files。执行链可包含任意数量的仓库和生成步骤，不硬编码具体仓库、两仓三阶段或命令名称。

动态执行链按以下证据优先级生成：

1. 当前任务 `design.md` / `implement.md` 明确记录的顺序、命令和路径。
2. 项目 SOP/spec 中的 canonical、生成和分发约定。
3. 受版本控制的 `package.json`、Makefile 或仓库脚本入口，以及可确认的输入输出路径。
4. 可验证的 Git/submodule 父子关系。

上述优先级用于发现意图和执行顺序，不允许用文档覆盖当前仓库事实。命令入口、工作目录和输出路径必须由受版本控制内容验证；任务 artifacts、SOP/spec、脚本实际行为或 Git 关系互相冲突时失败关闭。

命令必须是受版本控制的稳定入口，并且本地、确定性、可重复、无外部副作用。工作目录和预期影响路径必须可审计；只有名称相似、mtime、目录邻近或惯例不足以执行。禁止任意 shell 字符串、管道、重定向、命令替换、push、release、deploy、物理 GC、凭证和生产数据操作；证据不足时失败关闭。

计划包含生成命令时，前置仓成功后执行，再重验预计 exact files、全部 dirty 与 retained 基线；超范围即停止重新规划，最终 clean 不强行提交。普通多仓失败停止后续仓库，保留已成功动作。

## 已有 merge

仅普通模式适用；固定 HEAD/MERGE_HEAD，要求冲突清零、全部 staged 属于 planned 且无 retained staged。已有 merge 会提交整个索引，禁止 commit-only；不发起、终止或解决合并。

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
