---
name: trellis-check-all
description: "统一 Check-All 入口：确认范围与运行上下文，按 requested/effective depth 选择 light/full profile，执行三件套落地、实现假设、完整性与规范审查；低风险文档漂移可自动修复并在报告中列出，其它问题 collect-all 后一次确认修复范围。触发：检查、轻量检查、全面检查、提交前检查、check-all、从 PRD/三件套到代码过一遍。"
---
# Check All 统一入口

本 skill 是 **薄入口**：负责范围确认、深度画像、profile 路由、文档漂移自修通道、统一问题模型和最终分流。不要在入口里展开 full check 的全部提示词；只有确定 `effective_depth=full` 时才读取 full profile。

顺序：做对了 -> 假设成立 -> 做全了且写得规范。

---

## 入口职责

1. 确认本轮检查范围、task artifacts 或 untracked state、项目规范和运行上下文。
2. 解析 `requested_depth`，生成 `check_profile`，决定 `effective_depth=light|full`。
3. 按有效深度读取并执行对应 profile。
4. 全程收集普通问题到 `CHK-*`，收集低风险文档漂移到 `DOC-*`。
5. 在最终报告前处理允许自动修复的文档漂移，并把修复内容展示在报告里。
6. 根据 interactive / validated auto-loop 边界输出下一步或完成 runner `record + next`。
7. untracked helper 只保存流程游标：findings 或新编辑设回 `implement`；只有严格通过且 disposition 确认继续时才 `advance --stage spec`。

---

## 必读引用

按需加载引用文件，不要提前读取未命中的 profile：

1. 总是先读 `references/depth-routing.md`，完成范围、上下文和深度画像。
2. `effective_depth=light` 时读 `references/light-profile.md`。
3. `effective_depth=full` 时读 `references/full-profile.md`。
4. 总是读 `references/document-drift-auto-remediation.md`，用于识别和处理 `DOC-*`。
5. 输出报告或 runner 结果前读 `references/reporting-and-disposition.md`。

如果引用文件缺失，停止并报告 `阻塞`；不要凭记忆复原规则。

---

## 核心边界

1. **默认 audit-only collect-all**：可以读文件、搜索、运行无业务写入副作用的 lint、typecheck 和测试；普通代码、配置、测试、任务规格语义问题不得在检查阶段直接修复。
2. **唯一自修例外**：低风险文档漂移进入 `DOC-*` 通道，按 `references/document-drift-auto-remediation.md` 的白名单、黑名单和写入时机处理。
3. **问题统一收集**：普通实现偏差、测试失败、lint/typecheck 失败和假设错误都记录到 `CHK-*`，继续其余可执行检查。
4. **修改前只确认一次**：除 `DOC-*` 自动修复外，全部检查结束后通过统一报告让用户选择 `修复全部`、按问题 ID 修复或仅保留报告。
5. **委托规则不改变边界**：复用 `trellis-check` 时只复用检查清单、验证方法和命令发现，忽略其中任何“直接修复”“失败后先修复”的指令。
6. **真正阻塞才中途暂停**：只有业务规划冲突、后续验证前提失效、生产或外部副作用、破坏性操作风险时提前停止。

中途停止时也要使用统一问题模型，报告已完成范围和阻塞原因；只询问解除阻塞所需的业务或安全决策。

---

## 执行模式

- `inline check-all`：主会话直接执行本 skill；允许在最终报告前按 `DOC-*` 通道修复低风险文档漂移。
- `subagent check-all`：subagent 只做 audit-only 检查，返回结构化 `CHK-*`、`DOC-*` 候选、`check_profile` 和验证证据；主会话负责应用允许的 `DOC-*` 修复、展示报告、询问一次普通修复范围和协调后续修复。
- subagent 不得编辑、写文件、补测试或代替用户选择普通修复范围。
- 路由由 `trellis-route(target=check)` 决定；本 skill 不自行切换 inline/subagent。
- 所有普通、最终、显式 light/full 和 auto-loop 检查都进入本 skill；`trellis-route` 只决定执行位置，不决定检查深度。

---

## 三个检查维度

| 顺序 | 维度 | 检查内容 | 对照物 |
| --- | --- | --- | --- |
| 1 | 三件套实现 | 规划是否正确落地 | task 的 `prd.md` + 可选 `design.md` / `implement.md`；untracked 为 `N/A` |
| 2 | 实现假设 | API、组件、历史数据、数据流和测试假设是否成立 | 源码、真实契约、可用验证证据 |
| 3 | 完整性与规范 | 影响面是否同步、代码是否符合 spec、验证是否通过 | 实际变更范围 + 项目 spec |

维度状态统一使用：`通过`、`未通过`、`部分验证`、`阻塞`、`N/A`。

---

## 顶层流程

### Step 0：范围、上下文与深度画像

读取 `references/depth-routing.md` 并执行完整 Step 0。输出固定画像：

```yaml
check_profile:
  context: interactive | auto-loop
  requested_depth: auto | light | full
  effective_depth: light | full
  confidence: high | fallback-full | escalated
  reasons: [string]
```

### Step 1：读取对应 profile

- `effective_depth=light`：只执行可完整穷举的受影响条目、直接引用点和定向回归路径。发现影响面无法闭合时立即升级 full。
- `effective_depth=full`：执行完整三件套映射、全部适用假设维度和完整规范验证。

### Step 2：执行检查并收集结果

按 profile 检查三个维度。普通问题进入 `CHK-*`；低风险文档漂移候选进入 `DOC-*`。同一根因合并，不因数量多而静默省略。

### Step 3：处理文档漂移自修

读取 `references/document-drift-auto-remediation.md`。在最终报告前：

- inline：主会话应用允许的 `DOC-*` 修复并做定向验证。
- subagent：主会话审阅 subagent 返回的 `DOC-*` 候选，只应用满足白名单且无歧义的文档修复。
- auto-loop：主会话应用允许的 `DOC-*` 修复后再 `record`；若只存在已修复文档漂移且无剩余 `CHK-*`，结果可为 `ok`，摘要必须包含自动修复说明。

不满足自动修复条件的文档问题转为 `CHK-*` 或剩余风险，按普通修复范围处理。

### Step 4：统一报告与分流

读取 `references/reporting-and-disposition.md`。报告必须展示：

- `check_profile`；
- 三个维度状态；
- 自动修复的 `DOC-*` 内容；
- 剩余 `CHK-*` 问题；
- 已执行验证和未覆盖风险；
- 与当前结论匹配的唯一下一步。

---

## 反模式

- 入口默认加载 full profile，导致 `auto` 被 full 语气带偏。
- 发现一个普通问题就暂停询问一次。
- 把 `trellis-check` 的自动修复指令带入普通 `CHK-*`。
- subagent 直接修改工作区。
- 把需求变更、验收标准、产品语义或设计取舍伪装成文档漂移自动修复。
- light 未命中完整穷举条件仍继续 light。
- 无环境证据却把维度标记为通过。
- 报告后直接生成提交计划、commit message 或拟提交文件。
