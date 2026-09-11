---
name: trellis-start
description: "Initializes an AI development session by reading workflow guides, developer identity, git status, active tasks, and project guidelines from .trellis/. Classifies incoming tasks and routes to brainstorm, direct edit, or task workflow. Use when beginning a new coding session, resuming work, starting a new task, or re-establishing project context."
---

# Start Session

Initialize a Trellis-managed development session. This platform has no session-start hook, so manually load the equivalent compact context by following these steps.

---

## Step 1: Current state
Identity, git status, current task, active tasks, journal location.

```bash
python3 ./.trellis/scripts/get_context.py
```

If this output includes a line beginning `Trellis update available:`, copy the full line verbatim when summarizing session context. Do not shorten operational command hints.

## Step 2: Workflow overview
Compact Phase Index, request triage rules, planning artifact contract, and the step-detail command.

```bash
python3 ./.trellis/scripts/get_context.py --mode phase
```

Full guide in `.trellis/workflow.md` (read on demand).

## Step 3: Guideline indexes
Discover packages + spec layers, then read each relevant index file.

```bash
python3 ./.trellis/scripts/get_context.py --mode packages
cat .trellis/spec/guides/index.md
cat .trellis/spec/<package>/<layer>/index.md   # for each relevant layer
```

Index files list the specific guideline docs to read when you actually start coding.

## Step 4: Decide next action
From Step 1 you know the current task and status. Check the task directory:

<!-- BEGIN skill-garden patch start-active-task-recovery-codex v0.6 -->
- **Active task exists** -> load `trellis-continue` and follow its recovery rules to decide the next step.
<!-- END skill-garden patch start-active-task-recovery-codex v0.6 -->
<!-- BEGIN skill-garden patch start-no-task-routing v0.6 -->
- **No active task** -> first run `python3 ./.trellis/scripts/untracked_flow.py status`. On `hit`, resume the reported stage through the matching `[workflow-state:untracked*]` breadcrumb; on `miss`, follow the workflow `Request Triage` contract before acting. Use `task_intent.py create` for inferred complex planning, `task.py create` for explicit task planning, and ask only for material ambiguity or an independent safety boundary.
<!-- END skill-garden patch start-no-task-routing v0.6 -->

---

## Skill routing (quick reference)

| User intent | Skill |
|---|---|
| New feature / unclear requirements | `trellis-brainstorm` |
| About to write code | `trellis-before-dev` |
| Done coding / quality check | `trellis-route(target=check)` → `trellis-check-all` |
| Stuck / fixed same bug multiple times | `trellis-break-loop` |
| Learned something worth capturing | `trellis-update-spec` |

Full rules + anti-rationalization table in `.trellis/workflow.md`.
