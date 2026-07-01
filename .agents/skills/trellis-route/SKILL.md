---
name: trellis-route
description: |
  Route trellis-implement / trellis-check execution mode with a gitignored personal preference file.
  Implement can route inline or subagent. Check defaults to check-all inline/subagent; lightweight
  trellis-check is hidden and only available when the user explicitly requests "light check" / "轻量检查".
  Invoked from Phase 2.1 target=implement and Phase 2.2 target=check/check-all of the routing-aware workflow.
  Current-task repair/recheck loops reuse the latest valid route decision instead of prompting again.
  Compacted resumes recover same-session route choices from gitignored runtime state before prefs or prompting.
  Final re-checks return to Phase 2.2 before commit.
  Skip in non-trellis projects (no .trellis/). Not for other subagents (trellis-research / trellis-debug).
---

# Trellis 路由器：implement / check 执行模式选择

主 agent 进入 Phase 2.1 实现路由或 Phase 2.2 检查路由时调用本 skill。当前上下文或 session runtime state 内已有合法来源、target 匹配、且 task 等于当前任务路径的最近 route 决策时，后续实现、修复、重检默认复用该决策；没有合法决策时才进入本 skill 或同编号 fallback。提交前确实需要最终复查时，回到 Phase 2.2 并复用当前任务的合法 check route，除非用户明确要求重选。

个人配置只写入 `.trellis/.route-prefs.tmp`。该文件匹配 `.trellis/.gitignore` 的 `*.tmp` 规则，属于开发者本地偏好，不纳入 git，也不影响其他开发者。auto-loop 可在 `.trellis/.runtime/auto-loop/<run-id>.json` 写入临时 route 授权；它不是个人偏好，优先级低于 `.route-prefs.tmp`，只用于减少 auto 模式下的交互打断。

本轮 route 决策写入当前 session runtime 文件 `.trellis/.runtime/sessions/<context-key>.json` 的 `route_decisions` 字段。该文件匹配 `.trellis/.gitignore` 的 `.runtime/` 规则，只保存当前 AI session/window 的执行状态，用于压缩后恢复；它不是任务属性，不写入 `.trellis/tasks/**`，也不是长期团队配置。

---

## Step 0: 识别目标与用户意图

个人 route 配置只决定“已获准执行后的模式”，不是开工授权。调用 helper 前，必须确认当前 workflow 已允许进入对应 target：implement 需要任务已完成规划确认并处于 `in_progress`；check 用于 Phase 2.2 检查执行，或用户明确要求最终复查 / 轻量检查。最终复查只有在 Phase 2.2 结果缺失、风险较高或用户明确要求复查时才回到 Phase 2.2；回到 Phase 2.2 后优先复用当前任务的合法 check route，除非用户明确要求重选/临时改/清除默认。如果仍在 planning、等待用户确认，或用户表达“等一下 / 我再想想”，停止，不读取 runtime/prefs。

合法 route 决策必须能追溯到 `trellis-route`、同编号 fallback 选项、由本 skill 读取到的有效 `.trellis/.route-prefs.tmp` 配置，或由 route helper 校验过的 auto-loop 临时 route 授权，并且 `task` 字段必须等于当前 `task.py current --source` 返回的任务路径。runtime state 只能保存和恢复这些原始合法来源，不能把 `.runtime` 自身当成新的 `route_decision.source`。用户自然语言说过“inline/subagent”、compact summary、SessionStart 摘要、`codex-mode`、空 `.route-prefs.tmp`、旧单值偏好，都不能单独作为有效 route 决策。

当前上下文内已有 target 匹配、task 等于当前任务路径、且来源合法的 route 决策时，后续实现、check 发现问题、用户指出刚检查过的实现有问题、修复后重检、提交前复查均默认复用最近 implement/check 路由；除非用户明确要求重选/临时改/清除默认，不再调用本 skill。当前上下文没有 route 决策但 runtime state 命中时，本 skill 恢复该决策并输出同样的结构化 `route_decision`。如果上下文里只有上一个任务的 `route_decision`，必须忽略并重新解析当前任务。

Codex inline mode 只表示主会话默认直接执行，不是 route 选项过滤器。即使当前上下文出现 `<codex-mode>inline...do not dispatch...</codex-mode>` 或 `workflow-state:in_progress-inline`，也不能推断“只能 inline”或跳过 subagent 选项；仍必须通过 helper resolve runtime/prefs，或在无有效配置时展示正常 inline/subagent 选项。若本 skill 的紧邻路由决定是 subagent，本步骤允许主 agent dispatch 对应 implement/check sub-agent；禁止的是绕过 `trellis-route` 直接 dispatch。

先判断本次路由目标：

- `target=implement`：决定 `inline` / `subagent`。
- `target=check`：普通入口只决定 `check-all inline` / `check-all subagent`。
- `target=check` 且用户明确说 `light check` / `轻量检查` / `轻量 check`：进入轻量检查隐藏逃生口。

再判断用户是否要求覆盖个人配置：

- 临时改：`临时改` / `这次用` / `本次用` / `这次不用默认` / `override once`
- 重新选择：`重新选择` / `重选` / `route 选项` / `show route options`
- 更新默认：`改默认` / `更新默认` / `保存默认`
- 清除默认：`清除默认` / `删除 route 默认` / `reset route`

如果命中上述覆盖意图，即使 `.trellis/.route-prefs.tmp` 存在，也不能直接使用配置；必须进入 Step 2 展示对应选项。

---

## Step 0.5: 解析已有 route state

仅在没有覆盖意图、当前上下文没有 target + 当前 task 匹配的合法 `route_decision` 时调用 helper 解析已有状态。helper 的解析顺序固定为：当前 session runtime 文件里的 `route_decisions` → `.trellis/.route-prefs.tmp` → `.trellis/.runtime/auto-loop/<run-id>.json` 临时授权。命中 `.route-prefs.tmp` 或 auto-loop 临时授权时，helper 会自动把对应决策写回当前 session runtime，后续压缩恢复不需要再次读 prefs 或 auto-loop 状态。

调用随本 skill 分发的 helper；不要在对话中内嵌或改写 helper 逻辑：

```bash
python3 .agents/skills/trellis-route/scripts/route_state.py resolve --target <implement|check>
# Claude 平台若只有 .claude skill 副本，则使用：
python3 .claude/skills/trellis-route/scripts/route_state.py resolve --target <implement|check>
```

helper 只接受当前 session 或唯一 session fallback 的 `.trellis/.runtime/sessions/<context-key>.json`，并只从 `route_decisions.<target>` 恢复当前任务的决策。命中时输出 `{"status":"hit", ...}`，其中默认输出里的 `task` / `mode` / `source` 已经过 task/target/source/mode/scope 校验，可跳过 Step 2 并进入 Step 3 输出决策。`origin=route-prefs` 表示来自个人 route 配置，并且 helper 已写回 session runtime state；`origin=auto-loop` 表示来自 auto-loop 临时授权且 helper 已写回 session runtime state；`origin=runtime` 表示来自 session runtime state。

helper 默认输出为精简 JSON，只包含 route 执行必需的 `status`、`origin`、`mode`、`source`/`reason` 等字段。需要排查完整 `decision`、session 文件、context key、任务路径、个人配置路径或写回标记时，在同一命令末尾加 `--verbose`；不要为了诊断信息额外读取 runtime 文件。

输出 `status=miss`、文件缺失、JSON 损坏、任务不匹配、source/mode 不合法、prefs 缺失或 prefs 值不合法时，忽略已有状态并继续 Step 2。不要删除不匹配 runtime 文件，避免误伤其他窗口。

---

## Step 2: 展示选项并等待用户选择

优先调用 `AskUserQuestion`。选项 label 前缀编号，方便用户直接打数字快速选。

如果当前平台或模式没有 `AskUserQuestion` / `request_user_input`，不要自行选择 inline 或 subagent 继续。改用普通聊天消息原样呈现同一组编号选项，并停止等待用户回复；用户回复数字后再进入 Step 2.5 / 2.6 / 3。

### target = implement，且 resolve 未命中

- **question**: "本次 implement 走哪种模式？"
- **header**: "Impl 模式"
- **options**:
  1. label "1. 本次 Inline", description "主 agent 直接执行，只影响这一次"
  2. label "2. 本次 Subagent", description "dispatch trellis-implement，只影响这一次"
  3. label "3. 保存默认：Inline", description "本次使用 inline，并写入个人配置，后续默认不再问"
  4. label "4. 保存默认：Subagent", description "本次使用 subagent，并写入个人配置，后续默认不再问"

### target = implement，且用户要求临时改 / 重新选择

- **question**: "当前默认：implement=<当前值或无>。本次要怎么处理？"
- **header**: "Impl 覆盖"
- **options**:
  1. label "1. 仅本次 Inline", description "只覆盖这一次，不修改个人配置"
  2. label "2. 仅本次 Subagent", description "只覆盖这一次，不修改个人配置"
  3. label "3. 更新默认为 Inline", description "本次使用 inline，并写入个人配置"
  4. label "4. 更新默认为 Subagent", description "本次使用 subagent，并写入个人配置"
  5. label "5. 清除默认", description "删除 implement 默认，然后重新显示无配置选项"

### target = check，且 resolve 未命中

普通 check 路由不展示轻量 `trellis-check`。

- **question**: "本次 check 走哪种模式？"
- **header**: "Check 模式"
- **options**:
  1. label "1. 本次 Check-all inline", description "主 agent 执行全面检查，只影响这一次"
  2. label "2. 本次 Check-all subagent", description "dispatch 全面检查，只影响这一次"
  3. label "3. 保存默认：Check-all inline", description "本次使用 check-all inline，并写入个人配置"
  4. label "4. 保存默认：Check-all subagent", description "本次使用 check-all subagent，并写入个人配置"

### target = check，且用户要求临时改 / 重新选择

普通选项仍只展示 `check-all` 路径。

- **question**: "当前默认：check=<当前值或无>。本次要怎么处理？"
- **header**: "Check 覆盖"
- **options**:
  1. label "1. 仅本次 Check-all inline", description "只覆盖这一次，不修改个人配置"
  2. label "2. 仅本次 Check-all subagent", description "只覆盖这一次，不修改个人配置"
  3. label "3. 更新默认为 Check-all inline", description "本次使用 check-all inline，并写入个人配置"
  4. label "4. 更新默认为 Check-all subagent", description "本次使用 check-all subagent，并写入个人配置"
  5. label "5. 清除默认", description "删除 check 默认，然后重新显示无配置选项"

### target = check，且用户明确请求轻量检查

轻量 `trellis-check` 是隐藏逃生口，不写入个人默认。

如果用户已经明确 inline / subagent：

- `轻量检查 inline` / `light check inline` → 直接输出 `inline check`
- `轻量检查 subagent` / `light check subagent` → 直接输出 `subagent check`

如果用户只说轻量检查但未指定执行方式：

- **question**: "用户明确请求轻量检查。本次轻量 check 走哪种模式？"
- **header**: "轻量 Check"
- **options**:
  1. label "1. 轻量 Check inline", description "主 agent 执行 trellis-check，只影响这一次"
  2. label "2. 轻量 Check subagent", description "dispatch trellis-check，只影响这一次"

---

## Step 2.5: 读 subagent_skip_compile

仅 `target=implement` 且最终选择 `subagent` 时读取：

```bash
if [ -f .trellis/config.yaml ]; then
  grep -E "^\s*subagent_skip_compile:\s*true\b" .trellis/config.yaml > /dev/null && echo true || echo false
fi
```

为 `true` 时，Step 3 的 implement subagent 指令会附加“跳过编译”prompt 段。其他路径不读此配置。

---

## Step 2.6: 写入 route state / 默认配置

用户选择本次模式后，调用 helper 写入当前 session runtime。选项含“保存默认”或“更新默认”时，加 `--save-pref`，helper 会同时更新 `.trellis/.route-prefs.tmp` 并保留另一个 target 的偏好。

只影响本次：

```bash
python3 .agents/skills/trellis-route/scripts/route_state.py write --target <implement|check> --mode <mode> --source <trellis-route|numbered-fallback>
# Claude 平台若只有 .claude skill 副本，则使用：
python3 .claude/skills/trellis-route/scripts/route_state.py write --target <implement|check> --mode <mode> --source <trellis-route|numbered-fallback>
```

保存 / 更新默认：

```bash
python3 .agents/skills/trellis-route/scripts/route_state.py write --target <implement|check> --mode <mode> --source <trellis-route|numbered-fallback> --save-pref
# Claude 平台若只有 .claude skill 副本，则使用：
python3 .claude/skills/trellis-route/scripts/route_state.py write --target <implement|check> --mode <mode> --source <trellis-route|numbered-fallback> --save-pref
```

清除默认：

```bash
python3 .agents/skills/trellis-route/scripts/route_state.py clear-pref --target <implement|check>
# Claude 平台若只有 .claude skill 副本，则使用：
python3 .claude/skills/trellis-route/scripts/route_state.py clear-pref --target <implement|check>
```

helper 写入规则：保留另一个 target 的 runtime 决策和偏好；覆盖当前 target；`source` 保持原始合法来源，正常 skill 交互用 `trellis-route`，普通聊天编号 fallback 用 `numbered-fallback`，prefs 命中由 helper 写 `route-prefs`，auto-loop 临时授权命中由 helper 写 `auto-loop`；保留 session runtime 文件中的 `platform`、`current_task`、`current_run`、`current_auto_run` 等既有字段；内部 `task` 必须写当前 `task.py current --source` 的任务路径。写入失败不阻塞本次 route 输出，但要在回复中简短说明未持久化，压缩后可能再次询问。

当同一 session 从一个任务切换到另一个任务时，helper 写入当前任务任一 target 的决策前会清理 runtime 中属于其他任务的 `route_decisions`。个人默认仍保留在 `.trellis/.route-prefs.tmp`；清理只影响当前 session 的临时恢复状态，目的是避免新任务的 check 阶段误复用上一个任务的 check 路由。

---

## Step 3: 输出执行指令

本 skill 不调用 Skill / Agent 工具，而是输出指令让主 agent 在下一轮执行。

### 路由表

| 路由决定 | 主 agent 应执行 |
|---------|----------------|
| `inline implement` | `Skill({skill: "trellis-before-dev"})` 加载 spec → 读任务文档 → 主线程实施 → 跑必要验证 |
| `subagent implement` | `Agent({subagent_type: "trellis-implement"})`；若 `subagent_skip_compile=true`，dispatch prompt 附加“跳过 mvn install / npm run build / tsc 等耗时编译类检查（已由主 agent 验证或最终统一执行）” |
| `inline check-all` | `Skill({skill: "trellis-check-all"})` |
| `subagent check-all` | 优先 `Agent({subagent_type: "trellis-check-all"})`；不存在时 fallback `Agent({subagent_type: "trellis-check"})` + dispatch prompt 含 trellis-check-all 全流程要求（PRD 对照 → 5 维断言 → 跨层 → 委托 trellis-check 收尾） |
| `inline check` | 仅轻量检查隐藏逃生口；`Skill({skill: "trellis-check"})` |
| `subagent check` | 仅轻量检查隐藏逃生口；`Agent({subagent_type: "trellis-check"})` |

### 输出模板

```markdown
路由决定：<inline/subagent> <implement | check-all | check>
[来自个人 route 配置：`.trellis/.route-prefs.tmp` (<key>=<value>)。]
[来自 auto-loop 临时 route 授权：`.trellis/.runtime/auto-loop/<run-id>.json`。]
[来自 session runtime route state：`.trellis/.runtime/sessions/<context-key>.json` 的 `route_decisions`。]
[已写入 session runtime route state：`.trellis/.runtime/sessions/<context-key>.json` 的 `route_decisions`。]
[说明：用户明确请求轻量检查，使用隐藏逃生口。]

route_decision:
  target: <implement | check>
  mode: <inline | subagent | check-all-inline | check-all-subagent | check-inline | check-subagent>
  source: <trellis-route | route-prefs | auto-loop | numbered-fallback>
  scope: task
  task: <current task path>

接下来主 agent 应当：
- <路由表里对应的工具调用形式>
- [若 implement subagent 且 subagent_skip_compile=true：附加“跳过编译”prompt 段]

不要：
- <要避免的工具调用>
```

中括号内行为条件性出现：仅命中个人配置时显示配置行；仅命中 runtime state 时显示“来自”行；写入 runtime state 成功时显示“已写入”行；仅轻量 check 时显示隐藏逃生口说明；仅 implement subagent + skip_compile=true 时附加“跳过编译”段。`route_decision` 必须保留在回复中，并至少保留 target/mode/source/scope/task；需要 path/decided_at 等诊断字段时重新调用 helper 并加 `--verbose`。compact summary 若只有自然语言描述，后续 agent 仍应优先读取 runtime state，而不是把 summary 当证据。

---

## 核心原则

1. **个人配置私有**：`.trellis/.route-prefs.tmp` 是本地偏好，gitignored，不能进入提交计划。
2. **runtime state 私有**：`.trellis/.runtime/sessions/` 是 session/window 状态，gitignored，不能进入提交计划。
3. **压缩恢复少打断**：当前上下文没有 route 决策时，先读 runtime state；命中当前任务/target 的合法决策就复用，不查 `trellis mem`，也不重问。
4. **个人默认次于本轮决策**：runtime state 表示“本轮已选”，优先于 `.route-prefs.tmp`。
5. **auto 授权低于个人默认**：auto-loop 临时授权只在 runtime 和个人偏好都 miss 时生效。
6. **显式覆盖优先于一切**：用户要求临时改、重新选择或清除默认时，必须重新展示选项，不能让 runtime state 或配置优先。
7. **当前任务复用路由**：当前任务内已有合法来源的最近 implement/check 路由时，后续实现、修复、重检和复查默认沿用，不再次询问模式。
8. **check 默认全面检查**：普通 check 路由只展示 `check-all` inline/subagent，不推荐轻量 `trellis-check`。
9. **轻量 check 是隐藏逃生口**：只有用户明确请求 `light check` / `轻量检查` 时才可走轻量 `trellis-check`。
10. **决策与执行分离**：本 skill 只输出指令，下一轮由主 agent 调工具。
11. **严格执行用户选择**：路由结论一旦输出，主 agent 必须按指令执行，不可“出于谨慎”再换路径。
12. **Codex inline 不裁剪选项**：Codex inline 是默认执行模式，不是只能 inline 的强制模式；route 明确选中 subagent 时，本步骤可按 subagent 路径执行。

---

## 反模式

- 在本 skill 内部直接调用 `Agent` / `Skill` 工具。
- 用户要求临时改 / 重新选择时，仍直接使用 `.trellis/.route-prefs.tmp`。
- 把 `.trellis/.route-prefs.tmp` 加入 git 暂存或提交计划。
- 把 `.trellis/.runtime/sessions/` 加入 git 暂存或提交计划。
- runtime state 的 task/target/source/mode 不匹配时仍复用。
- 在普通 check 选项里展示 `Check inline` / `Check subagent`。
- 没有用户明确请求时，把 check 降级到轻量 `trellis-check`。
- `AskUserQuestion` / `request_user_input` 不可用时，记录为 inline 或 subagent 路径并继续。
- 没有有效 check 配置、用户选择或最近本轮 check 路由决定时，自动执行 inline check。
- 没有 `source` 合法的 `route_decision`，就把“用户说过 inline/subagent”或 compact summary 当成已路由。
- check 发现问题后，把当前任务内已有合法 route 的修复/重检当成新的 route 边界再次询问模式。
- 给 check 任何模式附加“跳过编译”指令。
- 询问后忽视用户答案默认 subagent。
- 因 `<codex-mode>` 或 `in_progress-inline` 提到 inline，就自行把无配置 route 结果改成 inline 或隐藏 subagent 选项。

---

## 边界

- **非 trellis 项目**（无 `.trellis/`）：输出“非 trellis 项目，跳过路由”，不阻断流程。
- **config.yaml 缺失或字段缺失**：视为 false，不附加跳过编译指令。
- **无法解析 session context key**：跳过 runtime state 读写，继续 `.route-prefs.tmp` 或展示选项。
- **runtime state 损坏或不匹配**：忽略该文件，继续 `.route-prefs.tmp` 或展示选项；不要删除。
- **.route-prefs.tmp 内容损坏**：忽略偏好；必要时删除该文件并重新展示选项。
- **旧单值偏好**：文件内容只有 `inline` / `subagent` 时视为无效配置，按无配置处理并重新展示选项。
