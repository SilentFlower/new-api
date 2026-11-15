---
name: trellis-route
description: |
  Select inline/subagent execution for authorized Trellis implementation or Check-All.
  Reuse explicit choices and personal defaults; otherwise run inline without a menu.
  Resolve and execute in the same turn. Load interaction, dispatch, and error details on demand.
  Check-All owns light/full depth. Not for research/debug agents or non-Trellis projects.
---

# Trellis 路由器：implement / check 执行模式

主 agent 解析已获准工作的执行位置，并在同轮实际执行。没有选择时使用内置 inline，不询问模式。

**auto-loop 启动前选模例外**：已明确启动自动循环且 `trellis-auto-loop` 已解析任务列表时，按 [recovery-and-errors.md](references/recovery-and-errors.md) 只读预检并返回 runner。此时不套用下方执行门槛或同轮实施规则，也不绑定任务。

## 进入条件与证据

- task implement 需要任务已获首次 Brief 批准且处于 `in_progress`；task check 需要 Phase 2.2。用 `task.py current --source` 与 `untracked_flow.py status` 确认唯一 subject；untracked 要求对应 `stage=implement/check`。仍在 planning、等待确认或明确暂停时不读 runtime/prefs；无 subject 返回 Request Triage，无 `.trellis/` 跳过。
- 当前合法 task + target 决策在实现、修复和重检中复用；丢失后由 helper 恢复，不从摘要、历史数字或 SessionStart 推断模式。`<codex-mode>` / `workflow-state:*-inline` 只描述能力。
- helper 校验 task、target、scope、mode/source。untracked 只使用当前选择或 `read-pref`，不读写 task 决策。`builtin-default` 是内置执行缺省，不是人工选择，不写 session 或个人偏好。

## 解析模式

`implement` 使用 `inline/subagent`；`check` 使用 `check-all-inline/check-all-subagent`。light/full 是 Check-All 的 requested depth，不是 route mode。

1. **明确指令先应用**：当前请求明确 target 和模式，直接使用；只有明确“保存/更新默认、以后默认”才持久化个人偏好，否则临时应用。假设、问题、引用或历史叙述不算执行指令。
2. **待决选择才交互**：主动重选、要求菜单或模式变更中的 target/mode 含糊时读 [interaction.md](references/interaction.md)，仅问未决部分。禁止自动代选或把 implement 选择套到 check；正常无设置不属于待决选择。
3. **已有选择直接恢复**：没有覆盖意图且无当前合法 task 决策时，task 调 `resolve`，untracked 调 `read-pref`。helper 按 runtime → prefs → auto-loop → 内置 inline 解析；命中即执行，miss/error 按异常 reference 处理。

`<skill-dir>` 为实际加载目录，本项目是 `.agents/skills/trellis-route`，Claude-only 项目用 `.claude/skills/trellis-route`。只执行 helper，不读取脚本正文或手写解析：

```bash
python3 <skill-dir>/scripts/route_state.py resolve --target <implement|check>
python3 <skill-dir>/scripts/route_state.py read-pref --target <implement|check>
# task 明确选择；仅明确保存默认时加 --save-pref。
python3 <skill-dir>/scripts/route_state.py write --target <implement|check> --mode <mode> --source trellis-route
# untracked 仅在明确保存默认时写入。
python3 <skill-dir>/scripts/route_state.py write-pref --target <implement|check> --mode <mode>
```

helper 的 hit/written 输出就是本轮结构化执行证据。task 临时选择写当前 session；untracked 的“仅本次”只用于当前调用，不写任何 runtime 或偏好。个人默认位于 gitignored `.trellis/.route-prefs.tmp`，task 决策位于当前 session runtime，不提交这些记录。

## 按条件加载

| 命中条件 | 读取材料 |
| --- | --- |
| 主动菜单、重选、清除或紧邻编号答复 | [interaction.md](references/interaction.md) |
| 最终模式为 subagent | [subagent-dispatch.md](references/subagent-dispatch.md)，再读当前平台 catalog |
| helper miss/error、损坏状态、无 session 或 auto-loop prepare | [recovery-and-errors.md](references/recovery-and-errors.md) |

默认 inline 命中不读取上述 reference。仅当 route 已选中 subagent 时读取 catalog；inline 路径不读取 catalog，也不预先过滤 route 选项。明确指定但能力不可用时报告缺口，不静默降级、不发明替代 agent。

## 同轮执行与展示

正常沿用已有决策、个人默认或内置缺省时静默执行，不例行展示模式、来源、路径、YAML、写入字段或固定职责说明。用户主动选择、改选或管理默认时一句话确认；异常或保存失败按异常 reference 说明影响与处理方式，用户要求诊断时再展开。随后主 agent 在同一回合实际调用对应能力，不等用户回复“继续”。

| 模式 | 同轮动作 |
| --- | --- |
| `inline` implement | `trellis-before-dev`、当前事项上下文、实现与必要验证 |
| `subagent` implement | 按 dispatch reference 启动专用 `trellis-implement`，接收后返回实现 owner |
| `check-all-inline` | 执行 `trellis-check-all`，由它决定深度 |
| `check-all-subagent` | 启动专用 audit-only `trellis-check-all`，只返回候选和证据，不写文件 |

focused validation 完成后都必须返回 workflow Phase 2.1 的 completion contract，并同轮执行该 owner 的 Pre-Check action；默认立即进入 `trellis-route(target=check)`。路由不另设停止策略，不询问是否检查，也不以路由报告结束回合。
