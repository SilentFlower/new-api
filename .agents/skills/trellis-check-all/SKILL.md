---
name: trellis-check-all
description: "统一只读检查入口：结合任务、实际 diff、风险与运行上下文智能选择 light/full 深度，再执行规划实现、关键假设、完整性与规范审查。默认 collect-all，不在用户确认修复范围前修改代码。触发：检查、轻量检查、全面检查、提交前检查、check-all、从 PRD/三件套到代码过一遍。"
---
# Check All 全维度代码检查

依次检查规划正确性、实现假设、跨层完整性与规范性。默认采用 **audit-only collect-all**：先完成所有可继续的只读检查，统一报告问题，再由用户一次确认修复范围。

> 顺序：做对了 -> 假设成立 -> 做全了且写得规范。

---

## 核心边界

1. **检查阶段只读**：可以读文件、搜索、运行无业务写入副作用的 lint、typecheck 和测试；不得编辑代码、配置、测试或任务规格。
2. **问题统一收集**：普通实现偏差、测试失败、lint/typecheck 失败和假设错误都记录到问题集合，继续其余可执行检查，不逐项询问。
3. **修改前只确认一次**：全部检查结束后，通过统一报告让用户选择 `修复全部`、按问题 ID 修复或仅保留报告。
4. **委托规则不改变只读边界**：Step 3 只复用 `trellis-check` 的检查清单和验证方法，忽略其中任何“直接修复”“失败后先修复”的指令。
5. **真正阻塞才中途暂停**：只有以下情况可以提前停止：
   - 规划或业务行为互相冲突，无法判断正确实现；
   - 已发现的问题使后续检查前提失效，继续会产生误导结论；
   - 后续验证可能修改生产数据、调用有副作用的外部系统或执行破坏性操作。

中途停止时也要使用本 skill 的统一问题模型，报告已完成范围和阻塞原因；只询问解除阻塞所需的业务或安全决策，不进入逐项修复问答。

---

## 执行模式

- `inline check-all`：主会话直接执行本 skill。
- `subagent check-all`：subagent 只负责 audit-only 检查并返回结构化结果；主会话负责展示报告、询问一次修复范围和协调后续修复。
- subagent 不得自行修复，也不得代替用户选择修复范围。
- 路由由 `trellis-route(target=check)` 决定；本 skill 不自行切换 inline/subagent。
- 所有普通、最终、显式 light/full 和 auto-loop 检查都进入本 skill；`trellis-route` 只决定执行位置，不决定检查深度。

---

## 三个检查维度

| 顺序 | 维度 | 检查内容 | 对照物 |
| --- | --- | --- | --- |
| 1 | 三件套实现 | 规划是否正确落地 | `prd.md` + 可选 `design.md` / `implement.md` |
| 2 | 实现假设 | API、组件、历史数据、数据流和测试假设是否成立 | 源码、真实契约、可用验证证据 |
| 3 | 完整性与规范 | 影响面是否同步、代码是否符合 spec、验证是否通过 | 实际变更范围 + 项目 spec |

维度状态统一使用：`通过`、`未通过`、`部分验证`、`阻塞`、`N/A`。

---

## Step 0：确认范围与适用性

### 0.1 确认变更范围

默认工作区检查：

```bash
git status --short
git diff --name-only HEAD
git ls-files --others --exclude-standard
git log --oneline -10
```

`git diff --name-only HEAD` 用于覆盖 staged + unstaged 的已跟踪文件，未跟踪文件由 `git ls-files` 补充。不能只用 `git diff --name-only` 判断“无变更”。

如果用户要求检查已经提交的 PR/分支改动，先确认目标基线，再使用 merge-base 对应的 diff 范围；`git log -10` 不能替代 PR 变更范围。

如果确认范围内确实无变更，提示用户并终止。

### 0.2 读取任务与规范

读取当前任务：

- `prd.md`；没有时 Step 1 标记 `N/A`。
- `design.md`（若存在）。
- `implement.md`（若存在）。
- `check.jsonl` 中列出的 spec/research 文件（若存在）。
- 变更包对应的 `.trellis/spec/` 具体规范。

不得只依赖 session 摘要推断规划内容，必须读取实际文件。

### 0.3 验证运行上下文

默认 `context=interactive`。只有调用方声称来自 auto-loop 时，才通过 runner 的 `status` / `next` 验证以下事实：

- run 为 `running`；
- 当前 task 与本次检查任务一致；
- outstanding action 为 `run_check_all` 或 `run_recheck`。

不得用聊天摘要、自然语言声明或直接读取 raw runtime JSON 代替 runner 验证。验证失败时不得使用 auto-loop 授权；报告失败原因，并按 interactive 边界处理。

### 0.4 解析请求深度

`requested_depth` 只允许 `auto`、`light`、`full`，优先级固定为：

1. 当前用户请求里最新的显式深度意图；
2. validated auto-loop action 的 `requested_check_depth`；
3. 默认 `auto`。

显式意图按语义识别：`简单检查`、`轻量检查`、`light check` 表示 light；`全面检查`、`全量检查`、`最终检查`、`提交前检查`、`full check` 表示 full。同一请求出现多次切换时，以最后一次明确表达为准。单独说 `check` / `check-all` 只是调用统一入口，不自动等同 full。

历史 auto-loop state 缺少深度字段时，runner 会返回 `full`。不得根据文件数、diff 行数或“看起来简单”单独判定 light。

### 0.5 选择有效深度

按以下顺序生成检查画像：

```yaml
check_profile:
  context: interactive | auto-loop
  requested_depth: auto | light | full
  effective_depth: light | full
  confidence: high | fallback-full | escalated
  reasons: [string]
```

决策顺序：

1. `requested=full` -> `effective=full`。
2. 命中任一 hard-full -> `effective=full`；若请求为 light，使用 `confidence=escalated` 并记录原因。
3. `requested=light` 且无 hard-full -> `effective=light`。
4. `requested=auto` 且高置信满足全部 light eligibility -> `effective=light`。
5. 其它情况 -> `effective=full`、`confidence=fallback-full`；不询问用户。

**hard-full 信号**：

- 复杂任务存在 design/implement，且本次变更需要完整验收映射；
- 跨层、跨包、跨仓、submodule 或影响面尚未完全展开；
- 公共 API、CLI、schema、持久化状态、缓存契约、迁移或历史数据兼容；
- 权限、鉴权、安全、资金、并发、时序、状态机或回滚；
- workflow、skill、command、hook 注入或生成快照；
- 安装、升级、发布、push/commit 工作流控制面；
- 正在重检既有 full `CHK-*` 修复结果；
- light 执行中发现未知 dirty path、真实影响面扩大或关键验证缺口。

**light eligibility 必须全部满足**：

- 变更可完整归属，且集中在单一局部行为；
- 无 hard-full 信号；
- 受影响规划条目、直接引用点和回归路径可穷举；
- 存在可运行的定向验证，或仅为无行为风险的文案、注释、局部样式；
- 不在既有 full 修复/重检链中。

light 执行中命中 hard-full 时，立即单向升级 full 并补齐所有适用维度；同一修复/重检循环内 full 不得降级。Step 2 各 Dimension 仍须先判断 Trigger，未命中时标记 `N/A` 并跳过。

---

## Step 1：对照规划三件套检查实现

### 1.1 验收依据

- PRD Requirement / Acceptance Criteria：行为基线。
- Design API、数据模型、数据流、关键决策和 rollback：技术基线。
- Implement 有序步骤、review gate 和 rollback point：落地基线。

light 只提取可穷举的受影响条目；full 提取所有适用条目。每条记录来源位置，实际阅读对应代码后再判断。

### 1.2 必查类型

| 来源 | 可验证条目 |
| --- | --- |
| `prd.md` | AC、需求、业务规则、UI 文案、边界和异常场景 |
| `design.md` | API 路径/方法/字段、数据模型、数据流、关键 tradeoff、rollout/rollback |
| `implement.md` | 有序步骤是否落地、review gate 是否满足、rollback point 是否可用 |

`implement.md` 中的 validation command 在本步骤只做静态前提核对；真实运行归 Step 3。

### 1.3 追踪方法

| 条目类型 | 追踪路径 |
| --- | --- |
| API 行为 | Controller/Handler -> Service -> DAO/Storage |
| 前端交互 | 组件 -> 事件 -> 状态管理 -> API 调用 |
| 数据校验 | 前端规则 + 后端 validator/service |
| UI 文案 | 组件、i18n/locale 或其它有效文案来源 |
| 计算转换 | 实际 service/utility 算法及边界值 |
| 状态流转 | 状态定义 + 允许的转换条件 |
| Schema | DTO/类型/迁移中的字段、类型、约束和默认值 |
| Implement 步骤 | 对应代码、配置、迁移或资产是否存在且可用 |

文案要求逐字一致时，对照最终有效文案来源；不要强制要求文案必须直接写在组件字面量中。

### 1.4 记录结果

发现偏差、缺失、部分实现或文案不一致时，写入统一问题集合并继续。不要在此步骤询问“先修还是继续检查”。

---

## Step 2：实现假设验证

根据实际变更选择适用 Dimension。每个适用 Dimension 都要确认源码或真实契约证据，不能凭记忆通过。

### Dimension A：API Contract

**Trigger**：新增或修改已有 API 调用、请求参数或响应解析。

- 读取 Controller/Handler 和 DTO/Schema，确认实际请求、响应结构。
- 找到项目内同 API 或同模式调用作为参考。
- 确认参数名、类型、默认值、分页字段和起始页码。
- 覆盖正常、空值、零值和错误响应。

### Dimension B：Component Context

**Trigger**：在 Modal、Drawer、Tab 或条件渲染容器内修改有状态组件。

- 确认容器关闭或切换时是否销毁子组件。
- 确认受控值、初始化值和外部状态绑定。
- 确认状态保持/重置行为符合规划。
- 对照项目内相同容器的既有用法。

### Dimension C：Data History

**Trigger**：新增、修改或重新解释持久化字段。

- 确认历史记录的新字段值和 null/零值行为。
- 确认过滤、聚合和降级查询能处理历史数据。
- 追踪新字段的写入来源和可靠性。
- 无可用历史数据环境时标记 `部分验证` 或 `阻塞`，不得标记通过。

### Dimension D：Data Flow Trace

**Trigger**：变更跨越 UI、API、Service、Storage 中的两个或更多边界。

- 模拟完整请求路径和返回路径。
- 确认各层参数名、类型、嵌套层级一致。
- 覆盖缺省、空值、零值、特殊字符和错误传播。
- 分层代码分别正确不等于整条链路正确，必须连起来核对。

### Dimension E：Verification Tests

**Trigger**：Dimension A-D 任一适用。

- 检查关键假设是否已有可运行的自动化测试或明确手动验证。
- 优先覆盖最脆弱的参数名、嵌套结构、历史数据和空值路径。
- 测试存在时实际运行；未运行不能报告通过。
- 缺少测试时记录问题，等待用户确认修复范围后再新增测试。

发现假设错误时写入统一问题集合并继续其它可执行检查。只有该错误让后续检查前提失效时，才按“真正阻塞”规则暂停。

---

## Step 3：完整性、规范与项目验证

读取 `.agents/skills/trellis-check/SKILL.md`（Claude-only 项目读取对应 `.claude` 副本），复用以下内容：

- 适用 spec 的读取方法；
- lint、typecheck、测试等项目验证命令；
- 测试覆盖、跨层数据流、复用、依赖和同层一致性检查；
- debug logging、warning suppression 和类型安全绕过检查。

### Audit-Only 覆盖规则

在 Check-All 内执行时，下列 `trellis-check` 指令一律失效：

- “Fix any failures before proceeding”；
- “fix them directly”；
- “Report and Fix”；
- 任何要求检查 agent 直接编辑、补测试或反复修到通过的语句。

验证失败时记录命令、退出状态和关键错误到统一问题集合，继续其它独立验证。可能写业务数据或外部系统的验证不直接运行，按真正阻塞规则处理。

---

## 统一问题模型

每个独立根因使用固定字段：

| 字段 | 规则 |
| --- | --- |
| ID | 首次记录时依次分配 `CHK-001`、`CHK-002`；当前修复/重检循环中不重新编号 |
| 严重度 | `P0` 数据破坏/安全事故/无法安全继续；`P1` 功能错误/需求违背/发布阻塞；`P2` 测试/规范/维护性/非阻塞风险 |
| 标题 | 描述根因，不用症状堆叠 |
| 来源 | prd/design/implement/spec/assumption/verification |
| 证据 | `file:line`、实际契约或命令结果 |
| 影响 | 用户、数据或工程影响 |
| 建议 | 推荐修复方式，不在检查阶段执行 |
| 位置 | 同一根因的全部受影响位置 |
| 验证 | 修复后的命令或手动验证步骤 |

同一根因的多个位置合并到一个问题。报告按严重度排序，但不得因此重排已经分配的 ID。新根因使用下一个 ID。

---

## 输出：统一检查报告

interactive 模式完成所有可继续检查后，严格按以下顺序输出：

```markdown
## Trellis Check-All 结果

[<通过/未通过/阻塞>] <N> 个维度 · <N> 个问题 · P0 <N> / P1 <N> / P2 <N> · 验证 <通过>/<总数>

任务：<任务名称或无活动任务>
范围：<文件数与层级摘要>
画像：requested=<auto/light/full> · effective=<light/full> · confidence=<high/fallback-full/escalated> · <原因摘要>
结论：<一句话结论>

### 维度结果

| 维度 | 状态 | 问题 | 验证 |
| --- | --- | ---: | --- |
| 三件套实现 | <通过/未通过/部分验证/阻塞/N/A> | <N> | <摘要> |
| 实现假设 | <通过/未通过/部分验证/阻塞/N/A> | <N> | <摘要> |
| 完整性与规范 | <通过/未通过/部分验证/阻塞/N/A> | <N> | <摘要> |

### 问题清单

- [ ] `CHK-001` `[P1]` <标题>
  - 来源：<来源>
  - 证据：<file:line / 契约 / 命令结果>
  - 影响：<影响>
  - 建议：<修复建议>
  - 位置：<全部受影响位置>
  - 验证：<验证命令或步骤>

### 未覆盖与风险

- [<部分验证/阻塞/N/A>] <说明>

### 修复批次

批次 1：<问题 ID> · <修复目标>
修复后：定向验证 -> Check-All 重检

操作：`修复全部`、`修复 CHK-001,CHK-003`、`仅保留报告`
```

展示规则：

- 没有问题时省略“问题清单”“修复批次”和操作行，只报告通过结果、验证和剩余风险。
- 有问题时只在报告末尾提供一次修复范围选择，不再逐项提问。
- 独立问题不得因数量多而静默省略；先合并同根因重复项，再完整列出剩余问题。
- 报告不得包含 commit message、拟提交/暂存文件、commit-only 决策或提交确认。
- light 通过正式满足 Phase 2.2 检查门禁；未执行维度必须标记 `N/A`，不得伪装为已验证。

---

## 修复与重检

用户选择修复范围后：

1. 主会话复用当前任务已有的合法 implement route，批量修复选中的无歧义问题；不存在合法 implement route 时先进入 `trellis-route(target=implement)`，不得自行默认 inline/subagent。
2. 修复过程中不对每个问题重复确认。
3. 新增业务歧义、破坏性风险或范围扩张时才暂停，并一次性说明受影响问题。
4. 完成定向验证后复用当前 check route 重新执行 Check-All。
5. 原问题沿用 ID；新根因继续递增编号。上次 `effective_depth=full` 时，本次最小深度为 full。

修复完成后输出：

```markdown
## Trellis Check-All 修复结果

[<完成/部分完成/失败>] 修复 <完成>/<计划> · 验证 <通过>/<总数> · 剩余问题 <N>

| 问题 | 修复 | 验证 |
| --- | --- | --- |
| CHK-001 | <已修复/未修复/阻塞> | <通过/失败/未执行> |

### 未修复与风险

- <问题或风险；没有时写“无”>

结论：<重检结论与下一步>
```

检查通过后才指向 Phase 3.3 `trellis-update-spec`，再到 Phase 3.4 `trellis-push`。仍有问题时停留在修复/重检循环。

---

## Auto-Loop Return Gate

validated auto-loop 复用相同的 audit-only 检查、画像和问题模型，但不展示普通模式的修复选择：

- 有问题：向 runner `record --result failed --effective-check-depth <light|full> --check-depth-reason <summary>`，摘要包含最高严重度、问题 ID、根因和受影响文件；随后立即 `next`，由 runner 进入 `run_fix`。
- 真正需要用户产品决策、越权、生产副作用或破坏性安全决策：使用同样深度字段 `record --result blocked`，随后按 runner 状态停止。
- 无问题：`record --result ok --effective-check-depth <light|full> --check-depth-reason <summary>`，随后立即 `next`。
- subagent 只返回结构化报告和 `check_profile`；主会话收到后必须立即完成匹配 action 的 `record + next`，不得先套用 interactive 停止边界。
- 不修改 runner 的 fix/recheck 预算、commit-only 授权或队列行为。

---

## Interactive Post-Check Stop Gate

非 validated auto-loop 的检查报告输出后立即停止并等待用户选择。允许输出的内容只有：

- 各维度状态、问题数和问题清单；
- 已执行验证及结果；
- 未覆盖验证和剩余风险；
- 总体结论；
- 有问题时的一次修复范围选择，或通过时的 Phase 3.3 / Phase 3.4 下一步指向。

禁止在本轮生成提交计划、commit message、拟提交文件或要求用户确认提交。

---

## 反模式

- 发现一个普通问题就暂停询问一次。
- 检查阶段直接修改代码、配置或测试。
- 把 `trellis-check` 的自动修复指令带入 Check-All。
- 未命中 Trigger 仍展开所有 Step 2 Dimension。
- 无环境证据却把维度标记为通过。
- 按检查步骤而不是实际影响划分严重度。
- 同一根因拆成大量重复问题，或因问题多而静默省略。
- subagent 自行选择修复范围或返回前修改工作区。
- 报告后直接进入 commit/push。
