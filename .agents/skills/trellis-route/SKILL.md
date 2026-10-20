---
name: trellis-route
description: |
  Select inline/subagent execution for authorized Trellis implementation or Check-All.
  Reuse task/session choices and personal defaults; apply explicit mode instructions directly.
  Resolve and execute in the same turn. Load interaction, subagent dispatch, or error details only when needed.
  Check-All owns light/full depth. Not for research/debug agents or non-Trellis projects.
---

# Trellis 路由器：implement / check 执行模式

本 skill 由主 agent 执行，决定已获准工作的位置。模式确定后同轮实际进入对应能力；只在选择未决、用户要求暂停或执行能力不可用时停下。

**auto-loop 启动前选模例外**：用户已明确启动自动循环，且 `trellis-auto-loop` 已解析显式任务列表时，先按 [recovery-and-errors.md](references/recovery-and-errors.md) 选择模式并返回 runner。此路径允许任务仍在 planning 或尚未绑定 session，不套用下方执行门槛或同轮实施规则；普通调用和 running action 继续遵守下文。

## 进入条件与证据

- task implement 需要任务已获首次 Brief 批准且处于 `in_progress`；task check 需要 Phase 2.2。untracked 需要当前 session 的 `untracked_flow.py status` 命中对应 `stage=implement/check`。仍在 planning、等待确认或用户说“等一下”时停止，不读 runtime/prefs。
- 用 `task.py current --source` 与 `untracked_flow.py status` 确认 subject，只能命中一个；均未命中时返回 Request Triage。无 `.trellis/` 时跳过路由。
- 合法 route 决策必须能追溯到本 skill 当前明确选择、紧邻编号选择、有效个人偏好或 helper 校验的 auto-loop 授权。task 决策须匹配当前 task、target、scope 和合法 mode/source；untracked 只接受当前选择或 `read-pref`，不读写 task `route_decisions`。
- 当前上下文已有合法的当前 task + target 决策时，实现、修复、重检和最终复查直接复用，除非当前用户明确覆盖。压缩后没有此证据则调用 helper 恢复，不查 `trellis mem`，不从 compact summary、ordinary summary、SessionStart、replacement history 或历史消息推断模式。
- `<codex-mode>` / `workflow-state:*-inline` 只描述能力，不是决定或选项过滤器；历史裸数字不形成当前选择。当前自然语言须按下方明确指令流程应用并记录，不能只写“用户选过”作为证据。

## 解析模式

`target=implement` 使用 `inline/subagent`；`target=check` 使用 `check-all-inline/check-all-subagent`。light/full 是 Check-All 的 requested depth，不是 route mode。

1. **明确指令先应用**：当前请求唯一明确 target 和模式，如“这次实现用子 agent”“以后检查默认用当前会话”，直接使用，不再要求菜单复选。主会话/Inline 对应 inline，子 agent/Subagent 对应 subagent；check 使用对应 `check-all-*`。只有明确“保存/更新默认、以后默认”才持久化个人偏好，否则临时应用。假设、问题、引用或历史叙述不算执行指令。
2. **待决选择才交互**：用户只说“重选/清除默认”，或模式变更指令中的 target/mode 含糊，读取 [interaction.md](references/interaction.md)，仅询问未决选择，禁止自动代选或把 implement 选择套到 check。
3. **已有选择直接恢复**：没有覆盖意图且当前上下文没有合法 task 决策时，task 调 `resolve`；untracked 每次调用 `read-pref`。命中即执行；普通 miss 进入交互，异常按下方条件 reference 处理。

`<skill-dir>` 使用当前已安装的 skill 目录；本项目为 `.agents/skills/trellis-route`，Claude-only 项目用 `.claude/skills/trellis-route`。只执行 helper，不内嵌其解析逻辑或默认读取脚本正文：

```bash
# task：runtime -> prefs -> auto-loop，偏好/auto 命中会写回 session。
python3 <skill-dir>/scripts/route_state.py resolve --target <implement|check>
# untracked：不依赖任务或 session runtime。
python3 <skill-dir>/scripts/route_state.py read-pref --target <implement|check>
# task 明确选择；仅明确保存默认时加 --save-pref。
python3 <skill-dir>/scripts/route_state.py write --target <implement|check> --mode <mode> --source trellis-route
# untracked 明确保存默认；仅本次选择不调用写入。
python3 <skill-dir>/scripts/route_state.py write-pref --target <implement|check> --mode <mode>
```

helper 的 hit/written 输出就是本轮结构化执行证据，保留其中 mode/source/task 和本次 target/scope；正常对话不重复打印 YAML。task 临时选择写当前 session；untracked 的“仅本次”只用于当前调用，不写任何 runtime 或偏好。

个人默认仅在 `.trellis/.route-prefs.tmp`；task 决定仅在当前 session `.trellis/.runtime/sessions/`。二者 gitignored，不纳入任务属性、团队配置或提交范围。显式覆盖优先，除此以外 helper 独占 runtime → prefs → auto-loop 顺序。

## 按条件加载

| 命中条件 | 读取材料 |
| --- | --- |
| 需要模式菜单、重选、清除或编号答复 | [interaction.md](references/interaction.md) |
| 最终模式为 subagent | [subagent-dispatch.md](references/subagent-dispatch.md)，然后读取当前平台 catalog |
| helper error、损坏状态、缺少 session、需要诊断或 auto-loop prepare 只读预检 | [recovery-and-errors.md](references/recovery-and-errors.md) |

默认 inline 命中不读取上述 reference。仅当 route 已选中 subagent 时读取 catalog；inline 路径不读取 catalog，也不预先过滤 route 选项。缺少选择或能力时仍由用户决定，不静默降级、不发明替代 agent。

## 同轮执行与展示

只说明一句实际执行方式，例如“沿用默认，由当前会话实现”“恢复已有选择，使用子 agent 检查”；明确指定时说明已应用当前选择。随后主 agent 在同一回合实际调用下表能力，不以路由报告结束回合，不等用户回复“继续”。不例行展示来源路径、YAML、写入字段或工具配方；用户要求诊断时再展开。

| 模式 | 同轮动作 |
| --- | --- |
| `inline` implement | 加载 `trellis-before-dev` 和当前任务/事项上下文，实施并完成必要验证 |
| `subagent` implement | 按条件 reference 启动专用 `trellis-implement`，接收结果后返回实现 owner |
| `check-all-inline` | 执行 `trellis-check-all`，由它解析检查深度 |
| `check-all-subagent` | 按 catalog 启动专用 audit-only `trellis-check-all`；只返回 `CHK-*` / `FBK-*` / `DOC-*` 候选，不写文件 |

implement 路由只决定执行位置，不拥有实现后的停止策略。无论 inline 或 subagent，focused validation 完成后都必须返回 workflow Phase 2.1 的 completion contract，并在当前回合实际执行该 owner 解析出的 Pre-Check action：auto-loop、用户显式继续/暂缓和已有 hold 按 owner 结果处理；默认分支必须立即进入 `trellis-route(target=check)`。不得先询问是否运行 Check-All，也不得把 Check-All 作为可选下一步后结束回合。
