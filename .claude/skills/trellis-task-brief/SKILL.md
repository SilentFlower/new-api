---
name: trellis-task-brief
description: "从最新 prd.md、design.md、implement.md 生成、刷新、校验并在对话中展示 Trellis 任务的 brief.md。用于 Phase 1.4 task.py start 前、planning review、用户要求任务 brief/交接摘要，或 in_progress 任务缺失/过期 brief.md 时。"
---

# Trellis 任务交接摘要

为当前任务生成或更新 `brief.md`，并把交接摘要展示在对话里。`brief.md` 是从三件套派生的交接视图，不替代 `prd.md`、`design.md`、`implement.md`。

## 核心规则

- 每次运行都重新读取最新 `prd.md`、`design.md if present`、`implement.md if present`。
- 已存在的 `brief.md` 不能作为跳过同步的理由。
- `brief.md` 必须以三件套为准覆盖旧内容；无法从三件套追溯的旧内容不能保留为事实。
- 不要在 `brief.md` 里发明三件套没有表达的新需求。缺失字段写“未明确”，并提示应补充三件套。
- 写回 `brief.md` 后，必须在当前对话中展示 brief 正文；不要只给文件路径。
- Phase 1.4 前展示完整 brief，并等待用户确认 planning artifacts 和 brief 后，才运行 `task.py start`。
- `in_progress` 阶段发现缺失 brief 时，不自动生成未经 review 的 brief；先读取三件套并建议回补。只有用户明确要求当场回补并 review 时，才继续写回 `brief.md`。
- 不要机械限制 brief 或对话展示长度；信息完整优先，不能截掉会影响实现判断的范围、约束、风险或验收条件。

## 执行步骤

1. 确定任务目录：
   - 用户给了任务路径时使用该路径。
   - 否则运行 `python3 ./.trellis/scripts/task.py current --source`，读取 `Current task:`。
2. 读取任务状态：
   - 读取 `<task>/task.json` 的 `status`。
   - 如果 `status` 是 `in_progress` 且 `<task>/brief.md` 不存在，并且用户没有明确要求“回补 / backfill / 重新生成并 review brief”，则只读取三件套、说明 brief 缺失、建议回补，不写 `brief.md`。
3. 读取任务文件：
   - 必读：`prd.md`。
   - 存在则读：`design.md`、`implement.md`。
4. 从最新三件套提取：
   - `Goal`：任务目标一句话。
   - `Scope`：本轮实现范围。
   - `Non-Goals`：明确不做的范围。
   - `Key Context`：关键文件、模块、入口、约束或风险。
   - `Acceptance`：主要验收标准。
   - `Next Step`：进入实现后的下一步。
5. 写回 `<task>/brief.md`。如果文件已存在，仍用最新三件套派生内容覆盖旧正文。
6. 在对话中展示 brief 正文，并说明来源文件。

## 模板

```markdown
# Brief — <任务标题>

## Goal

- <一句话说明任务目标>

## Scope

- <本轮要做的事情>

## Non-Goals

- <本轮明确不做的事情>

## Key Context

- <关键文件、模块、入口、约束或风险>

## Acceptance

- <主要验收标准>

## Next Step

- <进入实现后的下一步>
```

## 展示格式

Phase 1.4 review 前：

```markdown
任务交接摘要已更新：<task>/brief.md

<brief.md 正文>

请确认 planning artifacts 和上述 brief；确认后才运行 `task.py start <task>`。
```

任务已经是 `in_progress` 时，如果 brief 存在，进入 implement route 前重述：

```markdown
当前任务 brief：<目标一句话>
范围/约束：<不失真的压缩要点>
验收：<不失真的压缩要点>
完整摘要：<task>/brief.md

下一步：进入 `trellis-route(implement)`。
```

压缩重述不能丢掉会影响实现判断的范围、约束、风险和验收条件。

## 不要做

- 不要执行 `task.py start`；这仍由主 workflow 在用户确认后执行。
- 不要修改三件套来迎合 brief。
- 不要把 `brief.md` 当作第四件套扩写。
- 不要只因为 `brief.md` 已存在就跳过更新。
- 不要只输出“已写入 brief.md”而不展示正文。
