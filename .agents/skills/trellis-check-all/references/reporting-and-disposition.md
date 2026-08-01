# Reporting And Disposition

本文件定义统一问题模型、报告模板、修复循环、auto-loop 返回和 interactive 停止边界。

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

interactive 模式完成所有可继续检查和允许的 `DOC-*` 自动修复后，严格按以下顺序输出：

```markdown
## Trellis Check-All 结果

[<通过/未通过/阻塞>] <N> 个维度 · CHK <N> · 自动修复 DOC <N> · P0 <N> / P1 <N> / P2 <N> · 验证 <通过>/<总数>

工作：<任务名称 | Untracked work: work-id | 无活动工作>
范围：<文件数与层级摘要；包含自动修复产生的文档 diff>
画像：requested=<auto/light/full> · effective=<light/full> · confidence=<high/fallback-full/escalated> · <原因摘要>
结论：<一句话结论>

### 维度结果

| 维度 | 状态 | 问题 | 验证 |
| --- | --- | ---: | --- |
| 三件套实现 | <通过/未通过/部分验证/阻塞/N/A> | <N> | <摘要> |
| 实现假设 | <通过/未通过/部分验证/阻塞/N/A> | <N> | <摘要> |
| 完整性与规范 | <通过/未通过/部分验证/阻塞/N/A> | <N> | <摘要> |

### 自动修复

| 文档 | 修复 | 验证 |
| --- | --- | --- |
| DOC-001 | <文件与修复内容> | <通过/失败/未执行> |

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

### 下一步

<按下方 `Interactive Post-Check Stop Gate` 输出一个明确、可执行的主动作>
```

展示规则：

- 没有 `DOC-*` 自动修复时省略“自动修复”区。
- 没有 `CHK-*` 时省略“问题清单”“修复批次”和操作行；如果存在自动修复，仍展示自动修复和验证。
- 有 `CHK-*` 时只在报告末尾提供一次修复范围选择，不再逐项提问。
- interactive 标准报告必须以“下一步”段结束；停止等待不等于省略引导。
- 独立问题不得因数量多而静默省略；先合并同根因重复项，再完整列出剩余问题。
- 报告不得包含 commit message、拟提交/暂存文件、commit-only 决策或提交确认。
- light 通过正式满足 Phase 2.2 检查门禁；未执行维度必须标记 `N/A`，不得伪装为已验证。

---

## 修复与重检

用户选择修复范围后：

1. 主会话复用当前 task 已有的合法 implement route；untracked 则重新直接读取个人 pref。不存在合法 route 时进入 `trellis-route(target=implement)`，不得自行默认 inline/subagent。
2. 修复过程中不对每个问题重复确认。
3. 新增业务歧义、破坏性风险或范围扩张时才暂停，并一次性说明受影响问题。
4. 完成定向验证后复用当前 check route 重新执行 Check-All。
5. 原问题沿用 ID；新根因继续递增编号。上次 `effective_depth=full` 时，本次最小深度为 full。

修复完成后输出：

```markdown
## Trellis Check-All 修复结果

[<完成/部分完成/失败>] CHK 修复 <完成>/<计划> · DOC 自动修复 <N> · 验证 <通过>/<总数> · 剩余问题 <N>

| 问题 | 修复 | 验证 |
| --- | --- | --- |
| CHK-001 | <已修复/未修复/阻塞> | <通过/失败/未执行> |
| DOC-001 | <已自动修复/未修复/阻塞> | <通过/失败/未执行> |

### 未修复与风险

- <问题或风险；没有时写“无”>

结论：<重检结论>

### 下一步

<按下方 `Interactive Post-Check Stop Gate` 输出一个明确、可执行的主动作>
```

检查通过后的动作由下方 `Interactive Post-Check Stop Gate` 判断：普通交互停止等待，符合 direct Git 严格通过条件时同轮进入 Phase 3.3 `trellis-update-spec`，再到 Phase 3.4 `trellis-push`。仍有 `CHK-*` 时停留在修复/重检循环。

untracked 在最终报告前调用 `untracked_flow.py record-check`：严格通过记录 `pass`，有问题记录 `findings`，部分验证记录 `partial`，真正阻塞记录 `blocked`。普通严格通过但尚未继续时保持 `stage=check`；只有 direct Git 同轮继续或用户后续明确继续时才 `advance --stage spec`。任何报告后的新编辑先回到 `prepare-edit`，旧检查证据随 fingerprint 失效。

---

## Auto-Loop Return Gate

validated auto-loop 复用相同的画像、profile、`DOC-*` 通道和问题模型，但不展示普通模式的修复选择：

- 有 `DOC-*` 且可自动修复：主会话先应用并验证；当前任务 `implement.md` / `brief.md` 的每个实际变化都追加精确 `--doc-remediation-file`，再决定最终 `ok|failed|blocked`。
- 有剩余 `CHK-*`：向 runner `record --result failed --effective-check-depth <light|full> --check-depth-reason <summary>`，摘要包含最高严重度、问题 ID、根因、受影响文件和已自动修复的 `DOC-*`。
- 真正需要用户产品决策、越权、生产副作用或破坏性安全决策：使用同样深度字段 `record --result blocked`，随后按 runner 状态停止。
- 无剩余问题：`record --result ok --effective-check-depth <light|full> --check-depth-reason <summary>`，摘要包含自动修复数量。
- record 成功后立即 `next`；若返回 `status=retryable reason=artifact-drift`，不得 `next`，先按 runner 指令在同一 outstanding action 内自纠并重录。validated auto-loop 不渲染交互式下一步段、不提示用户回复“继续”、不等待普通修复范围选择。
- 不修改 runner 的 fix/recheck 预算、commit-only 授权或队列行为。

subagent 只返回结构化报告、`DOC-*` 候选和 `check_profile`；主会话收到后必须完成允许的 `DOC-*` 处理，再完成匹配 action 的 `record + next`。

---

## Interactive Post-Check Stop Gate

非 validated auto-loop 先输出完整标准报告，再在本 Gate 内按以下顺序分流：

1. 只从触发本轮完成链的最新用户消息识别 direct Git intent：明确请求普通 push，或用户主动 `commit-only`。不得从历史消息、任务标题、摘要、dirty 状态或 auto-loop 内部 action 推断。
2. direct Git 只有在 Check-All 整体结论通过、剩余 `CHK-*` 为 0、无阻塞、无部分验证、无待用户接受的实质剩余风险时才算严格通过。允许存在已成功验证的 `DOC-*` 自动修复；标准报告输出后，同一轮进入 Phase 3.3 `trellis-update-spec`；`no-op|written` 再由其加载 `trellis-push`，`needs-review` 停止。
3. findings、blocked、部分验证或实质剩余风险均不满足条件：输出标准报告并停止，不运行 Update-Spec，也不生成 Git 计划。原始 Git 请求不授权自动修复普通问题、忽略问题或扩大 Git 权限。
4. 没有匹配 direct Git intent 的普通 interactive 检查保持原行为：报告后立即停止并等待用户选择。

### 交互式下一步引导

所有 interactive 标准报告都必须在末尾输出 `### 下一步`，并按以下首个命中分支给出一个明确主动作：

1. 有剩余 `CHK-*`：提示用户回复 `修复全部`、精确问题 ID 或 `仅保留报告`；不得重复提出逐项确认。
2. 有 blocked、部分验证或实质剩余风险：指出解除阻塞所需的精确决策、授权或验证，以及完成后重新运行 Check-All；涉及生产、外部系统或破坏性副作用时只引导用户授权，不自行执行。
3. direct Git 严格通过：说明本轮正在进入 `trellis-update-spec`，不要求用户再次回复“继续”或确认 Git 计划。
4. 无 direct Git intent 且严格通过：提示用户回复 `继续`，下一轮进入 `trellis-update-spec`，再由 `trellis-push` 生成提交计划。

停止边界只控制是否自动推进，不能让报告在没有下一步提示的情况下结束。

允许 Check-All 标准报告输出的内容只有：

- 各维度状态、问题数和问题清单；
- `DOC-*` 自动修复内容和验证；
- 已执行验证及结果；
- 未覆盖验证和剩余风险；
- 总体结论；
- 与当前结论匹配的唯一主动作引导；有问题时是一次修复范围选择，部分验证/阻塞时是补充决策或验证，通过时是 Phase 3.3 / Phase 3.4 指向。

Check-All 不新增 direct Git 专用摘要，也不得自行生成提交计划、commit message、拟提交文件或要求用户确认提交；strict pass 后的 Git 计划仍由 Update-Spec disposition 和 `trellis-push` owner 生成。
