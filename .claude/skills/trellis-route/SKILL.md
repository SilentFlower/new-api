---
name: trellis-route
description: |
  Route trellis-implement / trellis-check execution mode with a gitignored personal preference file.
  Implement can route inline or subagent. Check defaults to check-all inline/subagent; lightweight
  trellis-check is hidden and only available when the user explicitly requests "light check" / "轻量检查".
  Invoked from Phase 2.1 / 2.2 / 3.1 of the routing-aware workflow. Skip in non-trellis projects
  (no .trellis/). Not for other subagents (trellis-research / trellis-debug).
---

# Trellis 路由器：implement / check 执行模式选择

主 agent 进入 Phase 2.1 / 2.2 / 3.1 时调用本 skill，决定 implement / check 的执行模式。核心目标是减少重复打断：正常路由优先读取个人本地配置；用户要求临时改、重新选择或清除默认时，必须绕过配置并重新展示选项。

个人配置只写入 `.trellis/.route-prefs.tmp`。该文件匹配 `.trellis/.gitignore` 的 `*.tmp` 规则，属于开发者本地偏好，不纳入 git，也不影响其他开发者。

---

## Step 0: 识别目标与用户意图

个人 route 配置只决定“已获准执行后的模式”，不是开工授权。读取 `.trellis/.route-prefs.tmp` 前，必须确认当前 workflow 已允许进入对应 target：implement 需要任务已完成规划确认并处于 `in_progress`；check 需要已有本轮实现变更或用户明确要求检查。如果仍在 planning、等待用户确认，或用户表达“等一下 / 我再想想”，停止，不读取个人配置。

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

## Step 1: 读取个人路由配置

仅在没有覆盖意图时读取 `.trellis/.route-prefs.tmp`。

配置格式是简单 key-value 文本：

```text
implement=inline
check=check-all-inline
```

允许值：

- `implement`: `inline` / `subagent`
- `check`: `check-all-inline` / `check-all-subagent`

读取参考：

```bash
PREF_FILE=".trellis/.route-prefs.tmp"
if [ -f "$PREF_FILE" ]; then
  IMPLEMENT_PREF=$(awk -F= '$1=="implement"{print $2}' "$PREF_FILE" 2>/dev/null | tail -n 1)
  CHECK_PREF=$(awk -F= '$1=="check"{print $2}' "$PREF_FILE" 2>/dev/null | tail -n 1)
fi
```

规则：

- `target=implement` 且 `IMPLEMENT_PREF` 是 `inline` / `subagent`：跳过 Step 2，进入 Step 3。
- `target=check` 且 `CHECK_PREF` 是 `check-all-inline` / `check-all-subagent`：跳过 Step 2，进入 Step 3。
- 配置缺失、值不合法、目标 key 缺失：忽略配置，进入 Step 2。
- 文件损坏严重时可删除 `.trellis/.route-prefs.tmp`，然后进入 Step 2。

配置命中时，输出指令必须写明：

```markdown
来自个人 route 配置：`.trellis/.route-prefs.tmp` (<key>=<value>)。
如需临时改或重新显示选项，说“route 重新选择”“这次用 subagent”“清除 route 默认”等。
```

---

## Step 2: 展示选项并等待用户选择

优先调用 `AskUserQuestion`。选项 label 前缀编号，方便用户直接打数字快速选。

如果当前平台或模式没有 `AskUserQuestion` / `request_user_input`，不要自行选择 inline 或 subagent 继续。改用普通聊天消息原样呈现同一组编号选项，并停止等待用户回复；用户回复数字后再进入 Step 2.5 / 2.6 / 3。

### target = implement，且无可用配置

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

### target = check，且无可用配置

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

## Step 2.6: 写入或清除个人配置

选项含“保存默认”或“更新默认”时，更新 `.trellis/.route-prefs.tmp`。写入时保留另一个 target 的偏好；没有另一个 target 时只写当前 key。

推荐写法：

```bash
PREF_FILE=".trellis/.route-prefs.tmp"
mkdir -p .trellis
OLD_IMPLEMENT=$(awk -F= '$1=="implement"{print $2}' "$PREF_FILE" 2>/dev/null | tail -n 1)
OLD_CHECK=$(awk -F= '$1=="check"{print $2}' "$PREF_FILE" 2>/dev/null | tail -n 1)

# 按用户选择覆盖其中一个值：
# NEW_IMPLEMENT="inline" 或 "subagent" 或沿用 "$OLD_IMPLEMENT"
# NEW_CHECK="check-all-inline" 或 "check-all-subagent" 或沿用 "$OLD_CHECK"

{
  [ -n "$NEW_IMPLEMENT" ] && printf 'implement=%s\n' "$NEW_IMPLEMENT"
  [ -n "$NEW_CHECK" ] && printf 'check=%s\n' "$NEW_CHECK"
} > "$PREF_FILE"
```

清除默认时：

- 清除 implement 默认：只移除 `implement=...`，保留 `check=...`。
- 清除 check 默认：只移除 `check=...`，保留 `implement=...`。
- 如果文件最终为空，删除 `.trellis/.route-prefs.tmp`。

不要把 `.trellis/.route-prefs.tmp` 纳入任何提交计划。

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
[说明：用户明确请求轻量检查，使用隐藏逃生口。]

接下来主 agent 应当：
- <路由表里对应的工具调用形式>
- [若 implement subagent 且 subagent_skip_compile=true：附加“跳过编译”prompt 段]

不要：
- <要避免的工具调用>
```

中括号内行为条件性出现：仅命中个人配置时显示配置行；仅轻量 check 时显示隐藏逃生口说明；仅 implement subagent + skip_compile=true 时附加“跳过编译”段。

---

## 核心原则

1. **个人配置私有**：`.trellis/.route-prefs.tmp` 是本地偏好，gitignored，不能进入提交计划。
2. **正常路由少打断**：命中个人配置时直接输出路由决定，不再重复询问。
3. **显式覆盖优先于配置**：用户要求临时改、重新选择或清除默认时，必须重新展示选项，不能让配置优先。
4. **check 默认全面检查**：普通 check 路由只展示 `check-all` inline/subagent，不推荐轻量 `trellis-check`。
5. **轻量 check 是隐藏逃生口**：只有用户明确请求 `light check` / `轻量检查` 时才可走轻量 `trellis-check`。
6. **决策与执行分离**：本 skill 只输出指令，下一轮由主 agent 调工具。
7. **严格执行用户选择**：路由结论一旦输出，主 agent 必须按指令执行，不可“出于谨慎”再换路径。

---

## 反模式

- 在本 skill 内部直接调用 `Agent` / `Skill` 工具。
- 用户要求临时改 / 重新选择时，仍直接使用 `.trellis/.route-prefs.tmp`。
- 把 `.trellis/.route-prefs.tmp` 加入 git 暂存或提交计划。
- 在普通 check 选项里展示 `Check inline` / `Check subagent`。
- 没有用户明确请求时，把 check 降级到轻量 `trellis-check`。
- `AskUserQuestion` / `request_user_input` 不可用时，记录为 inline 或 subagent 路径并继续。
- 给 check 任何模式附加“跳过编译”指令。
- 询问后忽视用户答案默认 subagent。

---

## 边界

- **非 trellis 项目**（无 `.trellis/`）：输出“非 trellis 项目，跳过路由”，不阻断流程。
- **config.yaml 缺失或字段缺失**：视为 false，不附加跳过编译指令。
- **.route-prefs.tmp 内容损坏**：忽略偏好；必要时删除该文件并重新展示选项。
- **旧单值偏好**：文件内容只有 `inline` / `subagent` 时视为无效配置，按无配置处理并重新展示选项。
