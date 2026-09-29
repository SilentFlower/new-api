# Local Task System

The Trellis task system is stored entirely under `.trellis/tasks/` in the user project. Each task is a directory containing requirements, context, research, state, and relationship information.

<!-- BEGIN skill-garden patch trellis-meta-managed-task-artifacts v0.6 -->
## Task Directory Structure

```text
.trellis/tasks/
├── 04-28-example-task/
│   ├── task.json
│   ├── prd.md
│   ├── design.md
│   ├── implement.md
│   ├── brief.md
│   ├── implement.jsonl
│   ├── check.jsonl
│   └── research/
└── archive/
    └── 2026-04/
```

| File | Purpose |
| --- | --- |
| `task.json` | Authoritative task metadata and lifecycle state, including optional recovery progress. |
| `prd.md` | Requirements, constraints, and acceptance criteria. Lightweight tasks may be PRD-only. |
| `design.md` | Technical design for complex tasks: boundaries, contracts, data flow, compatibility, tradeoffs. |
| `implement.md` | Execution plan for complex tasks: ordered checklist, validation commands, review gates, rollback points. |
| `brief.md` | Generated planning handoff from the latest planning artifacts. It is displayed for review before activation, but does not replace `prd.md`, `design.md`, or `implement.md`. |
| `implement.jsonl` | List of spec/research files the implement agent must read first. |
| `check.jsonl` | List of spec/research files the check agent must read first. |
| `research/` | Research artifacts. Complex findings should not live only in chat. |
<!-- END skill-garden patch trellis-meta-managed-task-artifacts v0.6 -->
## `task.json`

`task.json` records task status and metadata. Common fields:

| Field | Meaning |
| --- | --- |
| `id` / `name` / `title` | Task identity and title. |
| `status` | Status such as `planning`, `in_progress`, `review`, or `completed`. |
| `priority` | `P0`, `P1`, `P2`, `P3`. |
| `creator` / `assignee` | Creator and assignee. |
| `package` | Target package in a monorepo; may be empty. |
| `branch` / `base_branch` | Working branch and PR target branch. |
| `children` / `parent` | Parent/child task relationships. |
| `commit` / `pr_url` | Commit and PR information after completion. |
| `meta` | Extension fields. |

## Parent / Child Task Trees

Parent/child task relationships are for work structure. A parent task groups related deliverables under one source requirement set; it is not a dependency scheduler and does not replace the child task's own planning artifacts.

Use a parent task when a request has multiple independently verifiable deliverables. The parent owns:

- Source requirements and user-facing scope.
- The map of child tasks and their responsibility boundaries.
- Cross-child acceptance criteria and final integration review.

Use child tasks for deliverables that can move through planning, implementation, check, and deterministic Close independently. If one child depends on another, write that dependency in the child `prd.md` / `implement.md`; do not rely on tree position to imply ordering.

Create new children with:

```bash
python3 ./.trellis/scripts/task.py create "<child title>" --slug <child-slug> --parent <parent-dir>
```

Link or unlink existing tasks with:

```bash
python3 ./.trellis/scripts/task.py add-subtask <parent-dir> <child-dir>
python3 ./.trellis/scripts/task.py remove-subtask <parent-dir> <child-dir>
```

`children` on the parent is a historical list. Closing a child, or later moving it through physical GC, does not remove that identity, so progress such as `[2/3 done]` remains meaningful without keeping the child active.

<!-- BEGIN skill-garden patch trellis-meta-managed-task-readiness v0.6 -->
The AI should not treat phase numbers or saved progress text as task status. Planning readiness comes from the required planning artifacts plus the refreshed `brief.md` review gate; execution and recovery use authoritative task status, owner evidence, required JSONL context, and the current workflow. Saved progress is advisory recovery evidence only.
<!-- END skill-garden patch trellis-meta-managed-task-readiness v0.6 -->

<!-- BEGIN skill-garden patch trellis-meta-managed-active-task-lifecycle v0.6 -->
## Active Task And Lifecycle

The user sees a "current task," but Trellis stores the active task pointer per session.

```text
.trellis/.runtime/sessions/<context-key>.json
```

`task.py start` binds the task path to the current session. For a planning task, activation first requires `trellis-task-brief` to refresh and display `brief.md`; the task-start guard rejects a missing or stale Brief. Different AI windows can point to different tasks without overwriting each other.

If the platform or shell environment has no stable session identity, `task.py start` may be unable to persist the pointer. Read the structured result and platform context instead of falling back to a shared global pointer.

`task.json.status` and the planning artifacts are authoritative. `task.json.progress` is narrow recovery evidence owned by `task_progress.py`; it must not override status, infer a workflow phase, restore a previous push mode, or resume Git orchestration.

When no active pointer exists, `trellis-continue` may surface healthy `in_progress` or completed-but-not-closed progress candidates, together with necessary invalid-candidate or scan diagnostics. The user must explicitly choose a task before the session is rebound; the recovery flow must never bind a session automatically. A completed candidate enters the `trellis-push` recovery preflight only when task-record publication is incomplete; otherwise its Close blockers are reported directly. Rework requires an explicit `completed -> in_progress` reopen.

The normal completion boundary is:

```text
in_progress -> business push -> atomic final progress + completed + deterministic Close -> task-record commit/push
completed + closeout pending/blocked -> recover delivery or resolve blockers -> deterministic Close
closed -- SessionStart after three days --> physical GC
completed -> explicit reopen -> in_progress
```

Partial pushes, user `commit-only`, and normal helper failures remain `in_progress`. Auto-loop closes an item immediately after its verified internal commit-only chain. Closed tasks are excluded from active pointers, task queues, counts, statusline, and recovery candidates; physical GC only changes storage location and never changes semantic state.
<!-- END skill-garden patch trellis-meta-managed-active-task-lifecycle v0.6 -->
## JSONL Context

`implement.jsonl` and `check.jsonl` are context manifests for sub-agents to read first. They do not replace `implement.md`; `implement.md` is the human-readable execution plan.

Format:

```jsonl
{"file": ".trellis/spec/cli/backend/index.md", "reason": "Backend conventions"}
{"file": ".trellis/tasks/04-28-example/research/api.md", "reason": "API research"}
```

Rules:

- Include spec and research files.
- Do not include code files that are about to be modified.
- Do not treat temporary conclusions in chat as the only context.
- Seed rows have no `file` field; they only prompt the AI to fill in real entries.

<!-- BEGIN skill-garden patch trellis-meta-managed-task-common-commands v0.6 -->
## Common Commands

```bash
python3 ./.trellis/scripts/task.py create "<title>" --slug <slug>
python3 ./.trellis/scripts/task.py start <task>
python3 ./.trellis/scripts/task.py current --source
python3 ./.trellis/scripts/task.py add-context <task> implement <file> <reason>
python3 ./.trellis/scripts/task.py validate <task>
python3 ./.trellis/scripts/task.py close <task> --json
python3 ./.trellis/scripts/task.py list [--closed|--all] [--json]
python3 ./.trellis/scripts/task.py gc --closed --before 3d [--dry-run] [--json]
python3 ./.trellis/scripts/task.py restore <task> [--dry-run] [--json]
```

Close changes semantic lifecycle state immediately. Physical GC only moves already-closed task directories and is normally invoked by SessionStart; run it manually only for diagnosis or explicit maintenance. Prefer script commands to direct JSON edits.
<!-- END skill-garden patch trellis-meta-managed-task-common-commands v0.6 -->
## Local Customization Points

| Need | Edit location |
| --- | --- |
| Change the default task template | `.trellis/scripts/common/task_store.py` and task creation instructions. |
| Change status semantics | `.trellis/workflow.md`, workflow-state hook logic, and task usage conventions. |
| Add task lifecycle actions | `hooks.after_*` in `.trellis/config.yaml`. |
| Change context rules | Planning artifact guidance in `.trellis/workflow.md` and related platform agent/hook instructions. |
| Change Close or physical GC policy | `.trellis/scripts/task_lifecycle.py`, `task.py`, shared task views, and the SessionStart bridge. |

These are local files in the user project. Do not default to editing Trellis CLI source code unless the user wants to contribute upstream.
