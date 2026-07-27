---
name: trellis-auto-loop
description: "启动、恢复和推进 Trellis 自动任务循环。用于用户明确要求 auto loop、自动跑任务、/goal 类似流程、一次跑多个任务、继续自动 run、查看/停止 auto-loop，或压缩恢复后需要从 .trellis/scripts/auto_loop.py 读取下一步。"
---

# Trellis Auto Loop

用 `.trellis/scripts/auto_loop.py` 驱动批量无人值守任务。runner 是状态、manifest、hash、依赖和预算的权威；本 skill 只判断语义边界并执行 runner 返回的 action。

## Run Contract

- 仅在用户明确要求 auto-loop、自动跑到底、goal-like 或继续既有 run 时使用；普通实现请求不能自动升级。
- 用户发出启动指令即授权本次 `commit-only` run。prepare 完成后不再确认 manifest，也不逐任务执行 `confirm_brief`。
- 新 run 先 prepare 全部显式任务，Open Questions 全部收敛后才进入 running。running 中不再询问 route、planning 或普通 Check-All 停止边界。
- 每个 action 完成后，必须用同名 `record --action ...` 精确回写并立即 `next`。不得根据聊天摘要手改 runtime 或跳步。
- 本地提交是自动终点。不得 push、merge、release、deploy、finish-work 或 archive；queue item 完成后 Trellis task 仍保持 `in_progress`。
- 任务顺序只决定稳定调度顺序，不隐含依赖。依赖必须通过 `--depends-on dependent=dependency` 明确传入或由 planning artifacts 明确声明。
- 任务级失败只阻塞自身及显式依赖项；独立任务继续。fix/recheck 与 planning repair 各最多 3 轮，队列结束后不自动执行第二遍恢复扫描。
- schema 1 runtime 继续按 runner 返回的旧 action 恢复，包括 outstanding `confirm_brief`；不要把旧 run 改写成 schema 2。

启动或恢复前静默清除交互式 pre-check hold；miss、task mismatch 或损坏诊断不阻断 runner：

```bash
python3 ./.trellis/scripts/pre_check_state.py clear
```

## Start

任务列表必须显式。用户只说当前任务时，先用 `task.py current --source` 解析。启动前为 implement/check 解析可复用 route；已有 session runtime 或 `.trellis/.route-prefs.tmp` 就复用，没有时按 `trellis-route` 询问并写入真实选择。临时选择才传 runner 参数，个人默认由 route helper 自己保存。

```bash
python3 ./.trellis/scripts/auto_loop.py start \
  --tasks <task> [<task> ...] \
  [--depends-on <dependent>=<dependency>] \
  --profile commit-only \
  [--check-depth auto|light|full] \
  [--route-implement inline|subagent] \
  [--route-check check-all-inline|check-all-subagent]
python3 ./.trellis/scripts/auto_loop.py next
```

默认 check depth 为 `auto`。light/full 只是 Check-All 请求深度，不是 route mode；hard-full 风险仍由 Check-All 升级。

用户一次选择全 Inline/Subagent 时可同时映射两个 route：

- Inline：`--route-implement inline --route-check check-all-inline`
- Subagent：`--route-implement subagent --route-check check-all-subagent`

runner 在 start 时检查任务存在性、task status、staged/conflict/未完成 Git 集成，并捕获主仓及已初始化子仓的 dirty baseline。全局安全错误不得通过 `--force` 绕过。

## Prepare Actions

prepare action 必须先完成并 record，runner 才会继续扫描整个队列。

| action | 主 agent 行为 | record |
| --- | --- | --- |
| `classify_dirty_baseline` | 把每个 `repository::path` 精确归到一个任务的 owned dirty，或归为 protected-retained；不猜测归属 | `record --action classify_dirty_baseline --result ok [--owned-dirty <task>=<repository>::<path>] [--protected-retained <repository>::<path>]` |
| `resolve_open_questions` | 使用 `trellis-brainstorm` 一次引导人工回答一个问题，并更新对应 planning artifacts；AI 不得代答、删除、改写或勾选 | 全部问题消失后 `record --action resolve_open_questions --result ok` |
| `review_planning_readiness` | 读取 action 绑定的 artifacts，按 Brainstorm Quality Bar 判断 `ready|repairable|blocking` | `record --action review_planning_readiness --result ok --readiness-verdict ready|repairable --summary "..."`；blocking 用 `--result blocked` |
| `run_planning_repair` | 仅按现有需求、代码、spec 和仓库证据修复 planning；不得处理 Open Questions 或高风险事项 | `record --action run_planning_repair --result ok --summary "..."` |
| `refresh_brief` | 使用 `trellis-task-brief` 刷新派生的 `brief.md`，无需再次让用户确认 | `record --action refresh_brief --result ok` |

`resolve_open_questions` 是整队列门禁：任一任务仍有 `- [ ]` 或历史裸列表时，run 保持 `awaiting_input`，不得先执行其它任务。`- [x]`、空章节或无章节不阻塞。

readiness 的 `repairable` 仅适用于不改变目标、可由仓库证据确定的问题，例如验收不可测试、design/implement 不完整或 context 未整理。达到 3 轮、需要产品选择或越过风险黑名单时返回 blocking。

## Autonomous Decisions

满足任务目标内、仅影响本地代码、可逆且可验证时，AI可自主选择推荐方案。作出选择后必须先记录，再继续修改或 record；会修改 planning/handoff 时，`--file` 必须列出全部目标 artifact：

```bash
python3 ./.trellis/scripts/auto_loop.py decide \
  --task <task> \
  --topic "<主题>" \
  --option "<候选>" [--option "<候选>" ...] \
  --choice "<选择>" \
  --summary "<依据摘要>" \
  [--evidence "<证据>" ...] \
  --risk low|medium \
  --confidence low|medium|high \
  [--requirement <id> ...] [--file <repository>::<path> ...] \
  [--verification "<验证摘要>"]
```

决策写入 runtime 摘要和任务 `decisions.jsonl`，只保存结论与证据，不保存思维链。下一次同任务 action record 会消费该决策：列明的 planning/handoff 变化生成绑定 decision ID 的 manifest revision；其它变化按 `artifact-drift` 阻塞。

以下事项不得用 `decide`，必须 blocked：

- 不可逆真实数据修改。
- 扩大权限或降低安全、隐私保护。
- 公开 API 或数据格式破坏性变更。
- 费用、生产环境或外部系统影响。
- push、merge、release、deploy、finish-work、archive。
- 明显改变任务目标或业务规则且仓库没有倾向证据。
- `Open Questions` 中人工保留的任何选择。

## Running Actions

| action | 主 agent 行为 | 成功 record |
| --- | --- | --- |
| `start_task` | 执行 action 返回的 `task.py start ...` | `record --action start_task --result ok` |
| `run_implement` | 进入 Phase 2.1，复用 manifest/当前任务 implement route | `record --action run_implement --result ok --route-mode <mode> --route-source <source>` |
| `run_check_all` | 进入 Phase 2.2，按 requested depth 执行统一 Check-All | `record --action run_check_all --result ok --route-mode <mode> --route-source <source> --effective-check-depth light|full --check-depth-reason "..."` |
| `run_fix` | 根据 `last_failure` 修复并复用 implement route | `record --action run_fix --result ok --route-mode <mode> --route-source <source>` |
| `run_recheck` | 复用 check route，且不得低于 `minimum_check_depth` | 同 `run_check_all`，action 改为 `run_recheck` |
| `run_spec_update` | 执行 `trellis-update-spec` | `no-op|written` 用 ok；`needs-review` 用 blocked + `spec-needs-review` |
| `commit_only` | 复用 `trellis-push` 内部精确本地提交能力，不 push | `record --action commit_only --result ok --commit <hash> --files <exact...> --commit-message "..."` |

失败或越权时必须回写，runner 决定重试、blocked 或继续队列：

```bash
python3 ./.trellis/scripts/auto_loop.py record \
  --action <action> --result failed|blocked \
  --failure-type <type> --summary "<摘要>" \
  [--files <repository>::<path> ...]
python3 ./.trellis/scripts/auto_loop.py next
```

Check-All 的 ok/failed/blocked 都要带实际 effective depth 与原因。validated auto-loop 不进入普通 Post-Check Stop Gate；检查完成后立即 `record + next`。

## Commit-Only

收到 `commit_only` 后：

1. 用 `status` 确认 active run、profile、outstanding action 和 task 一致。
2. 读取任务 artifacts、Git status/diff，生成 exact files、message 和归属理由。
3. staged 必须为空；不得包含冲突、未完成集成、runtime、route prefs、其它任务目录或 protected-retained。
4. 使用 `trellis-push` 内部 commit-only 提交 exact files。不得裸 `git add .`、`git add -A`、push 或按时间差猜归属。
5. 用提交 hash 和 exact files record，然后立即 next。

`decisions.jsonl` 属于当前任务文件，发生决策时应进入该任务最终精确提交。任务 `task.json.status` 不因 queue item completed 而改写。

## Resume, Retry, Stop

```bash
python3 ./.trellis/scripts/auto_loop.py resume
python3 ./.trellis/scripts/auto_loop.py next

python3 ./.trellis/scripts/auto_loop.py retry-blocked \
  [--run-id <run-id>] [--task <task>] [--all] \
  [--check-depth auto|light|full]
python3 ./.trellis/scripts/auto_loop.py next

python3 ./.trellis/scripts/auto_loop.py status [--verbose]
python3 ./.trellis/scripts/auto_loop.py stop --reason "<原因>"
```

默认使用紧凑输出；只有诊断 manifest、dirty、漂移、依赖链或决策详情时加 `--verbose`。`completed_with_blocked` 已是本次 run 的可审计终态，后续恢复由用户显式调用 `retry-blocked`。

## 禁止事项

- 不手写 `.trellis/.runtime/auto-loop/*.json`，不提交 runtime 或 `.route-prefs.tmp`。
- 不覆盖、暂存或提交 protected-retained 文件；发生路径冲突只阻塞涉及任务。
- 不用 `start --force` 代替 `retry-blocked`。
- 不把 queue item completed 解释为任务已归档。
- 不在无人值守执行中替用户回答 Open Questions。
