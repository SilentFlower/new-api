# Development Workflow

---

## Core Principles

1. **Plan before code** — figure out what to do before you start
2. **Specs injected, not remembered** — guidelines are injected via hook/skill, not recalled from memory
3. **Persist everything** — research, decisions, and lessons all go to files; conversations get compacted, files don't
4. **Incremental development** — one task at a time
5. **Capture learnings** — after each task, review and write new knowledge back to spec

---

## Trellis System

### Developer Identity

On first use, initialize your identity:

```bash
python3 ./.trellis/scripts/init_developer.py <your-name>
```

Creates `.trellis/.developer` (gitignored) + `.trellis/workspace/<your-name>/`.

### Spec System

`.trellis/spec/` holds coding guidelines organized by package and layer.

- `.trellis/spec/<package>/<layer>/index.md` — entry point with **Pre-Development Checklist** + **Quality Check**. Actual guidelines live in the `.md` files it points to.
- `.trellis/spec/guides/index.md` — cross-package thinking guides.

```bash
python3 ./.trellis/scripts/get_context.py --mode packages   # list packages / layers
```

**When to update spec**: new pattern/convention found · bug-fix prevention to codify · new technical decision.

### Task System

Every task has its own directory under `.trellis/tasks/{MM-DD-name}/` holding `task.json`, `prd.md`, optional `design.md`, optional `implement.md`, optional `research/`, and context manifests (`implement.jsonl`, `check.jsonl`) for sub-agent-capable platforms.

```bash
# Task lifecycle
python3 ./.trellis/scripts/task.py create "<title>" [--slug <name>] [--parent <dir>]
python3 ./.trellis/scripts/task.py start <name>          # set active task (session-scoped when available)
python3 ./.trellis/scripts/task.py current --source      # show active task and source
python3 ./.trellis/scripts/task.py finish                # clear active task (triggers after_finish hooks)
python3 ./.trellis/scripts/task.py archive <name>        # move to archive/{year-month}/
python3 ./.trellis/scripts/task.py list [--mine] [--status <s>]
python3 ./.trellis/scripts/task.py list-archive

# Code-spec context (injected into implement/check agents via JSONL).
# `implement.jsonl` / `check.jsonl` are seeded on `task create` for sub-agent-capable
# platforms; the AI curates real spec + research entries during planning when needed.
python3 ./.trellis/scripts/task.py add-context <name> <action> <file> <reason>
python3 ./.trellis/scripts/task.py list-context <name> [action]
python3 ./.trellis/scripts/task.py validate <name>

# Task metadata
python3 ./.trellis/scripts/task.py set-branch <name> <branch>
python3 ./.trellis/scripts/task.py set-base-branch <name> <branch>    # PR target
python3 ./.trellis/scripts/task.py set-scope <name> <scope>

# Hierarchy (parent/child)
python3 ./.trellis/scripts/task.py add-subtask <parent> <child>
python3 ./.trellis/scripts/task.py remove-subtask <parent> <child>

# PR creation
python3 ./.trellis/scripts/task.py create-pr [name] [--dry-run]
```

> Run `python3 ./.trellis/scripts/task.py --help` to see the authoritative, up-to-date list.

**Current-task mechanism**: `task.py create` creates the task directory and (when session identity is available) auto-sets the per-session active-task pointer so the planning breadcrumb fires immediately. `task.py start` writes the same pointer (idempotent if already set) and flips `task.json.status` from `planning` to `in_progress`. State is stored under `.trellis/.runtime/sessions/`. If no context key is available from hook input, `TRELLIS_CONTEXT_ID`, or a platform-native session environment variable, there is no active task and `task.py start` fails with a session identity hint. `task.py finish` deletes the current session file (status unchanged). `task.py archive <task>` writes `status=completed`, moves the directory to `archive/`, and deletes any runtime session files that still point at the archived task.

### Workspace System

Records every AI session for cross-session tracking under `.trellis/workspace/<developer>/`.

- `journal-N.md` — session log. **Max 2000 lines per file**; a new `journal-(N+1).md` is auto-created when exceeded.
- `index.md` — personal index (total sessions, last active).

```bash
python3 ./.trellis/scripts/add_session.py --title "Title" --commit "hash" --summary "Summary"
```

### Context Script

```bash
python3 ./.trellis/scripts/get_context.py                            # full session runtime
python3 ./.trellis/scripts/get_context.py --mode packages            # available packages + spec layers
python3 ./.trellis/scripts/get_context.py --mode phase --step <X.Y>  # detailed guide for a workflow step
```

---

<!--
  WORKFLOW-STATE BREADCRUMB CONTRACT (read this before editing the tag blocks below)

  The [workflow-state:STATUS] blocks embedded in the ## Phase Index section
  below are the SINGLE source of truth for the per-turn `<workflow-state>`
  breadcrumb that every supported AI platform's UserPromptSubmit hook
  reads. inject-workflow-state.py (Python platforms) and
  inject-workflow-state.js (OpenCode plugin) only parse them — there is no
  fallback dict baked into the scripts after v0.5.0-rc.0.

  STATUS charset: [A-Za-z0-9_-]+. When the hook can't find a tag, it
  degrades to a generic "Refer to workflow.md for current step." line —
  intentionally visible so users notice and fix a broken workflow.md.

  INVARIANT (test/regression.test.ts):
    Every workflow-walkthrough step marked `[required · once]` must have a
    matching enforcement line in its phase's [workflow-state:*] block. The
    breadcrumb is the only per-turn channel; if a mandatory step isn't
    mentioned there, the AI silently skips it (Phase 1 planning gate
    skip and Phase 3.4 commit skip both manifested via this gap).

  TAG ↔ PHASE scoping:
    [workflow-state:no_task]      → no active task; before Phase 1
    [workflow-state:planning]     → all of Phase 1 (status='planning')
    [workflow-state:planning-inline] → Codex inline variant of Phase 1
    [workflow-state:in_progress]  → Phase 2 + Phase 3.2-3.4
                                    (status stays 'in_progress' from
                                    task.py start until task.py archive)
    [workflow-state:in_progress-inline] → Codex inline variant of Phase 2/3
    [workflow-state:completed]    → currently DEAD: cmd_archive flips
                                    status and moves the dir in the same
                                    call, so the resolver loses the
                                    pointer (block kept for a future
                                    explicit in_progress→completed
                                    transition)

  Editing checklist:
    - When you change a [workflow-state:STATUS] block, also check the
      matching phase's `[required · once]` walkthrough steps for sync
    - Run `trellis update` after editing to push the new bodies to
      downstream user projects (block-level managed replacement)
    - Full runtime contract:
      .trellis/spec/cli/backend/workflow-state-contract.md
-->

## Phase Index

<!-- BEGIN skill-garden patch workflow-hub v0.6 -->
### Skill-Garden Workflow Control Hub

> Central control hub for Skill-Garden workflow behavior. Source: github.com/SilentFlower/skill-garden.

**Scope**: the cross-stage behaviors covered by the sections below. Phase walkthroughs point to the owning Skill; workflow-state bodies keep only current-state actions and short guards.

**Mechanical rule**: use this hub as the source of truth. Do not duplicate full hub or skill rules inside `workflow-state:*` bodies.

#### Request Intent Routing

For a new request, infer `discuss`, `inspect`, `direct_edit`, `task_plan`, or
`workflow_action` from the whole current message, scope, risk, side effects,
active-task state, and the latest explicit switch. High-confidence reversible
steps proceed without a mechanical task-creation question.

Clear complex implementation intent authorizes creating a planning task and
entering `trellis-brainstorm`; it never authorizes `task.py start` or
implementation. Ask one focused question only when ambiguity changes material
side effects or an independent safety boundary requires confirmation.

The latest explicit switch wins for the current request. `discuss` / `inspect`
route silently; entering untracked `direct_edit`, creating/resuming a task, or
switching intent gets one non-blocking status line. New unrelated requests
return to automatic inference instead of inheriting a session-wide mode.

If the latest switch leaves an auto-created planning task for untracked work,
run `task_intent.py discard --task <current-task>` before changing route. Proceed
only when it returns `status=discarded`; otherwise retain the task and report its
structured blocker. Never force-delete or silently leave a dormant planning task.

#### Brainstorm Gate

`trellis-brainstorm` is the required Phase 1.1 gate for new, complex, or unclear work.

`task.py create` only creates the planning workspace. A default `prd.md` does not mean requirements are ready.

Before `task.py start`, unclear scope, unresolved decisions, or non-testable acceptance criteria must return to `trellis-brainstorm`.

#### Project Knowledge Discovery

Before choosing an approach for non-trivial project work, run project knowledge
discovery when project-local SOPs, package conventions, workflow rules (CI, hooks),
config/state contracts, release/publish/deploy/rollback steps, git history actions,
data changes or fixes (including migrations), cross-layer design, generated artifacts,
install/sync pipelines, or destructive operations may affect the correct approach:

```bash
python3 ./.trellis/scripts/spec_router.py "<short query describing the intended action>"
```

Build the query from the current user request plus relevant immediate context:
the intended action, commands about to run, files or systems involved, package/layer,
and domain words matching the trigger list above.

Read high-confidence matches before acting; read medium-confidence matches only when
the path, heading, index description, or reason clearly fits the intended change.
If nothing matches, continue normally. Skip pure Q&A, simple read-only inspection,
opening local tools, or trivial edits unless project conventions or local SOPs may
change the approach.

#### Task Brief Handoff

Before Phase 1.4 `task.py start`, use `trellis-task-brief` to refresh `<task>/brief.md` from latest task artifacts, display it in chat, and wait for user confirmation.

`brief.md` is derived; `prd.md` / `design.md` / `implement.md` remain authoritative.

Before the first implement route, restate existing `<task>/brief.md` in chat. If missing, read task artifacts and suggest backfilling brief; do not invent one from memory.

#### Flower Update Confirmation

If `<flower-update>` has `priority: blocking_confirmation_required`, handle it first: briefly show `release_notes` when present, show `recommended_command`, and ask before running it.

If `<flower-update-result>` requests `run_trellis_push_confirmation`, enter `trellis-push` planning with update changes as default candidates; still require file-list and commit-message confirmation.

#### Active Task Scope Guard

When a session already has an active task, do not treat unrelated new implementation
requests as permission to implement under that task. If the work is not plainly
covered by the active task title/brief, route it as a new request and stop before
`trellis-route` or file edits. If the user explicitly declines task tracking,
state once that task/progress will not be recorded and do not use active-task
artifacts or progress for that work. If the user says it belongs to the active task,
update that task's artifacts before implementation.

#### Routing Gate

Phase 2.1 implement and Phase 2.2 check/check-all require route evidence before execution.

At each route boundary:

1. Reuse only an explicit, target-matched `route_decision` already present in the current context.
2. Otherwise you MUST invoke `trellis-route(target=implement|check)` before deciding; if the platform cannot invoke skills directly, read the local `trellis-route/SKILL.md` copy and follow its numbered fallback choices in normal chat and wait. `trellis-route` owns session runtime recovery, `.route-prefs.tmp`, fallback choices, runtime-state writes, and dispatch mapping.
3. If the route helper cannot ask through `AskUserQuestion` / `request_user_input`, ask the same numbered choices from `trellis-route` in normal chat and wait.

Plain preferences, summaries (including compact/SessionStart), replacement history, historical bare numeric replies, `codex-mode`, empty/stale prefs, and raw runtime files are not route evidence unless validated by `trellis-route`; numbered fallback validity is governed by `trellis-route`.

User reselect/override/use-X-this-time/clear-default wins over remembered route evidence, runtime state, and personal prefs.

Phase 2.1/2.2 and Active Task Routing enter this gate through `trellis-route`; dispatch subagents only when the resolved route selected subagent.

#### Auto-Loop Return Gate

When `trellis-auto-loop` is validated through the runner and the outstanding action is `run_check_all` or `run_recheck`, Check-All completion must immediately return the effective depth/result through the matching `record`, then call `next`. This gate applies before the interactive stop gate for both inline checks and subagent results; only a runner mismatch or a genuine product, authority, production-side-effect, or destructive-safety blocker may stop the loop.

#### Interactive Post-Check Stop Gate

Outside validated auto-loop, Check-All must report and stop. A pass permits only Phase 3.3 `trellis-update-spec` then Phase 3.4 `trellis-push`; it does not authorize archive or finish-work.

The report may contain only check results, validations, residual risks, conclusion, and the next-step pointer. It must not perform Phase 3.3/3.4, draft commit files/messages, choose commit-only, or ask for confirmation. Stop and wait for the user to continue.

After a passed stop, later `next` / `continue` or direct push must load `trellis-update-spec` in the same turn without asking whether. `no-op` / `written` then loads `trellis-push` in the same turn; `needs-review` stops for one focused question. Missing current results run spec first; still-current results are not rerun. This does not weaken the stop or final confirmation.

#### Code Commit Confirmation Gate

Code commit/push belongs only to Phase 3.4 and must go through `trellis-push`; the main session must not run bare `git commit` / `git push` for code.

Entering Phase 3.4 means loading and following `trellis-push`; drafting a commit message or file plan outside that skill is not an equivalent substitute. Ordinary mode defaults to exact commit + push. Commit-only is allowed only when the user explicitly requests a local commit, or when auto-loop invokes the internal executor after its own preauthorization check.

`trellis-push` owns all detailed plan/result presentation; this hub must not duplicate its templates, field order, repository labels, retained-dirty wording, or display thresholds. The hub only requires one ordinary confirmation over the exact file set and commit message. Before approval, do not `git add`, commit, or push; after approval, use exact paths with `git commit --only`, never `git add -A` / `git add .`. Unrelated dirty/staged paths stay retained and do not block the exact commit, while unknown ahead commits remain a branch-level push risk.

For "commit now, push later", use explicit `trellis-push` commit-only mode. `session_auto_commit` never authorizes code commits; it only affects finish-work bookkeeping below.

#### Auto-loop Commit-only Preauthorization

When the user explicitly starts `trellis-auto-loop` with `profile=commit-only`, that start preauthorizes only task-related local commits inside that run. `trellis-auto-loop` owns all status/profile/action/task validation, staged-area safety checks, semantic file attribution, and runner `record` calls.

After validation, auto-loop passes exact files and commit message to `trellis-push` internal commit-only. That internal path only performs the exact local commit: it does not read auto-loop runtime, call `status`/`record`, push, or write remote task progress. On success auto-loop records the commit hash/files/message and immediately asks the runner for `next`.

This exception does not apply to ordinary `trellis-push`. If the plan contains non-empty staged state, conflicts, unattributable files, remote push, release/archive intent, external systems, credentials, or production data effects, auto-loop must write the matching failed/blocked result and leave the files untouched.

#### Bookkeeping Auto-commit Scope

`session_auto_commit` only governs exact finish-work bookkeeping commits produced after `task.py archive --no-commit` and `add_session.py --no-commit`. When `false`, release/archive/journal writes stay disk-only. When `true`, finish-work commits only actual task source/destination/changed child task files and actual journal/index files; unrelated dirty/staged paths are retained.

Finish-work auto-push uses only its start baseline: push bookkeeping when an upstream exists and start `HEAD` exactly matched upstream; if the branch was already ahead, behind/diverged, or had no upstream, keep the new bookkeeping commits local. Task progress and working-tree cleanliness do not decide this behavior.

#### Task Progress Recovery

Use `python3 ./.trellis/scripts/task_progress.py status --json` for recovery reads and `write --task ... --progress-json ...` for progress writes; do not hand-scan or hand-edit task progress.

The helper only touches `task.json.progress`, whose fields are `updatedAt`, `completedSteps`, `partialStep`, `nextStep`, and `notes`. It may read legacy `last_push_snapshot` as a compatibility source; the next successful write creates `progress` and removes the legacy field.

Ordinary `trellis-push` owns the semantic progress summary and the separate exact current-task record/progress commit/push after business Git actions. That exact commit includes attributable dirty/untracked artifacts in the current task directory plus the updated `task.json`; those paths are not retained dirty or deferred to finish-work. Other task directories remain untouched. Commit-only paths do not create remote progress commits.

On recovery, relay the helper's `summary` / `candidates` once and suggest rebinding if there is no active task. Show only partial step, next step, and notes when useful. Never auto-rebind, infer a workflow phase, or restore old commit/push orchestration.
<!-- END skill-garden patch workflow-hub v0.6 -->

```
<!-- BEGIN skill-garden patch workflow-phase-summary v0.6 -->
Phase 1: Plan    → infer intent, take the authorized reversible next step, and write planning artifacts when needed
<!-- END skill-garden patch workflow-phase-summary v0.6 -->
Phase 2: Execute → implement only after task status is in_progress
Phase 3: Finish  → verify, update spec, commit, and wrap up
```

### Request Triage

<!-- BEGIN skill-garden patch workflow-request-triage v0.6 -->
- Infer `discuss`, `inspect`, `direct_edit`, `task_plan`, or `workflow_action` from the current request, its scope, risk, side effects, active-task state, and the latest explicit switch. Do not classify from one keyword alone.
- High-confidence reversible steps proceed without a mechanical task-creation question. Explicit complex implementation intent authorizes creating a planning task and entering `trellis-brainstorm`; it does not authorize `task.py start` or implementation.
- Ask one question only when ambiguity changes material side effects, or when destructive, production, database, credential, external-system, or permission boundaries require confirmation.
- The latest explicit `走任务` / `不要任务` / `先讨论` / `直接做` / `先别做` style instruction wins for the current request. New unrelated requests return to automatic inference.
<!-- END skill-garden patch workflow-request-triage v0.6 -->

### Planning Artifacts

- `prd.md` — requirements, constraints, and acceptance criteria. Do not put technical design or execution checklists here.
- `design.md` — technical design for complex tasks: boundaries, contracts, data flow, tradeoffs, compatibility, rollout / rollback shape.
- `implement.md` — execution plan for complex tasks: ordered checklist, validation commands, review gates, and rollback points.
- `implement.jsonl` / `check.jsonl` — spec and research manifests for sub-agent context. They do not replace `implement.md`.
- Lightweight tasks may be PRD-only. Complex tasks must have `prd.md`, `design.md`, and `implement.md` before `task.py start`.

### Parent / Child Task Trees

Use a parent task when one user request contains several independently verifiable deliverables. The parent task owns the source requirement set, the task map, cross-child acceptance criteria, and final integration review; it normally should not be the implementation target unless it also has direct work.

Use child tasks for deliverables that can be planned, implemented, checked, and archived independently. Parent/child structure is not a dependency system: if one child must wait for another, write that ordering in the child `prd.md` / `implement.md` and keep each child's acceptance criteria testable.

Create new children with `task.py create "<title>" --slug <name> --parent <parent-dir>`. Link existing tasks with `task.py add-subtask <parent> <child>`, and unlink mistakes with `task.py remove-subtask <parent> <child>`.

<!-- Per-turn breadcrumb: shown when there is no active task (before Phase 1) -->

[workflow-state:no_task]
<!-- BEGIN skill-garden patch workflow-state-no-task v0.6 -->
No active task. Infer the current request intent before acting.
Handle `discuss` and `inspect` silently. For `workflow_action`, load the named Trellis capability directly. For non-destructive `direct_edit`, state once that task/progress will not be recorded and proceed.
For high-confidence complex implementation, create an auto-routed planning task through `task_intent.py create`, show one non-blocking switch hint, and enter `trellis-brainstorm`. Ask only for material ambiguity or independent safety gates.
<!-- END skill-garden patch workflow-state-no-task v0.6 -->
[/workflow-state:no_task]

### Phase 1: Plan
<!-- BEGIN skill-garden patch workflow-phase-index-create-task v0.6 -->
- 1.0 Create task `[required · once]` (when `task_plan` is explicit or inferred from clear complex implementation intent)
<!-- END skill-garden patch workflow-phase-index-create-task v0.6 -->
- 1.1 Requirement exploration `[required · repeatable]` (`prd.md`; complex tasks also need `design.md` + `implement.md`)
- 1.2 Research `[optional · repeatable]`
- 1.3 Configure context `[required · once]` — Claude Code, Cursor, OpenCode, Codex, Kiro, Gemini, Qoder, CodeBuddy, Copilot, Droid, Pi, ZCode, Reasonix (sub-agent-dispatch platforms only; inline platforms skip)
- 1.4 Activate task `[required · once]` (review gate, then `task.py start`; status → in_progress)
- 1.5 Completion criteria

<!-- Per-turn breadcrumb: shown throughout Phase 1 (status='planning') -->

[workflow-state:planning]
<!-- BEGIN skill-garden patch workflow-state-planning v0.6 -->
Planning is not implementation permission. Load `trellis-brainstorm` and stay in planning while requirements remain unclear.
A created task or default `prd.md` is not enough to start implementation. Lightweight tasks may remain PRD-only; complex tasks require `prd.md`, `design.md`, and `implement.md`.
If the latest current-request switch says no task/direct edit, call `task_intent.py discard --task <current-task>` only for a task auto-created by intent routing. Continue only on `status=discarded`; keep manual or historical tasks unchanged.
Before `task.py start`, refresh and display `brief.md` through `trellis-task-brief`, then wait for planning review confirmation.
After status becomes `in_progress`, enter Phase 2 through `trellis-route(target=implement)` instead of editing or dispatching directly.
Sub-agent mode requires at least one real curated entry in both `implement.jsonl` and `check.jsonl`; the seed `_example` row alone is not ready.
<!-- END skill-garden patch workflow-state-planning v0.6 -->
[/workflow-state:planning]

<!-- Per-turn breadcrumb: shown throughout Phase 1 when codex.dispatch_mode=inline.
     Codex-only opt-in alternate to [workflow-state:planning]. The main agent
     edits code directly in Phase 2, so jsonl curation is skipped —
     the inline workflow loads `trellis-before-dev` instead of injecting JSONL
     into a sub-agent. -->

[workflow-state:planning-inline]
<!-- BEGIN skill-garden patch workflow-state-planning-inline v0.6 -->
Planning is not implementation permission. Load `trellis-brainstorm` and stay in planning while requirements remain unclear.
A created task or default `prd.md` is not enough to start implementation. Lightweight tasks may remain PRD-only; complex tasks require `prd.md`, `design.md`, and `implement.md`.
If the latest current-request switch says no task/direct edit, call `task_intent.py discard --task <current-task>` only for a task auto-created by intent routing. Continue only on `status=discarded`; keep manual or historical tasks unchanged.
Before `task.py start`, refresh and display `brief.md` through `trellis-task-brief`, then wait for planning review confirmation.
After status becomes `in_progress`, enter Phase 2 through `trellis-route(target=implement)` instead of editing or dispatching directly.
Inline mode skips JSONL curation and loads task artifacts plus relevant specs through `trellis-before-dev` before editing.
<!-- END skill-garden patch workflow-state-planning-inline v0.6 -->
[/workflow-state:planning-inline]

### Phase 2: Execute
- 2.1 Implement `[required · repeatable]`
- 2.2 Quality check `[required · repeatable]`
- 2.3 Rollback `[on demand]`

<!-- Per-turn breadcrumb: shown while status='in_progress'.
     Scope: all of Phase 2 + Phase 3.2-3.4 (status stays 'in_progress' from
     task.py start until task.py archive; only archive flips it). The body
     therefore must cover every required step from implementation through
     commit, including Phase 3.3 spec update and Phase 3.4 commit. -->

Sub-agent dispatch protocol applies to all platforms and all sub-agents, including class-2 Codex/Gemini/Qoder/Copilot/ZCode/Reasonix/Trae and `trellis-research`: every dispatch prompt starts with `Active task: <task path from task.py current>` before role-specific instructions.

[workflow-state:in_progress]
<!-- BEGIN skill-garden patch workflow-state-in-progress v0.6 -->
Before the first implement route, restate `<task>/brief.md`; if it is missing, read the task artifacts and suggest backfilling it instead of relying on memory.
New work outside the active task title/brief must stop before routing or edits. Recommend a new task; if the work belongs here, update artifacts first; if the user declines tracking, confirm untracked execution.
Enter Phase 2.1/2.2 through the target-matched `trellis-route`; a user route override wins over remembered evidence.
After Check-All, a validated auto-loop immediately records and advances; otherwise report and stop. A later interactive next/continue runs `trellis-update-spec`, then `trellis-push` for `no-op`/`written`, or stops on `needs-review`.
Run `/trellis:finish-work` only when explicitly requested after Phase 3.4 completes.
Dispatch `trellis-implement` or audit-only Check-All sub-agents only when the matching route selected subagent mode. Every dispatch prompt starts with `Active task: <task path from task.py current>` and loads JSONL entries before task artifacts.
<!-- END skill-garden patch workflow-state-in-progress v0.6 -->
[/workflow-state:in_progress]

<!-- Per-turn breadcrumb: shown while status='in_progress' when
     codex.dispatch_mode=inline. Codex-only opt-in alternate to
     [workflow-state:in_progress]. The main session edits code directly
     instead of dispatching sub-agents. -->

[workflow-state:in_progress-inline]
<!-- BEGIN skill-garden patch workflow-state-in-progress-inline v0.6 -->
Before the first implement route, restate `<task>/brief.md`; if it is missing, read the task artifacts and suggest backfilling it instead of relying on memory.
New work outside the active task title/brief must stop before routing or edits. Recommend a new task; if the work belongs here, update artifacts first; if the user declines tracking, confirm untracked execution.
Enter Phase 2.1/2.2 through the target-matched `trellis-route`; a user route override wins over remembered evidence.
After Check-All, a validated auto-loop immediately records and advances; otherwise report and stop. A later interactive next/continue runs `trellis-update-spec`, then `trellis-push` for `no-op`/`written`, or stops on `needs-review`.
Run `/trellis:finish-work` only when explicitly requested after Phase 3.4 completes.
Inline workflow-state is not an inline route decision. Do not default inline because the state or helper is inline; follow the resolved route, and use `trellis-before-dev` before main-session edits.
<!-- END skill-garden patch workflow-state-in-progress-inline v0.6 -->
[/workflow-state:in_progress-inline]

### Phase 3: Finish
- 3.2 Debug retrospective `[on demand]`
- 3.3 Spec update `[required · once]`
- 3.4 Commit changes `[required · once]`
- 3.5 Wrap-up reminder

> Note: step 3.1 was folded into 2.2 (last-iteration full-scope check) and 3.4 (commit preamble). Numbering kept stable to avoid breaking external references.

<!-- Per-turn breadcrumb: shown while status='completed'.
     Currently DEAD in normal flow: cmd_archive writes status='completed' in
     the same call that moves the task dir to archive/, so the active-task
     resolver loses the pointer and the hook never fires on archived tasks.
     Block preserved for a future status-transition redesign (e.g. an
     explicit in_progress→completed command). Edit through the same spec
     channel as the live blocks. -->

[workflow-state:completed]
Code committed. Run `/trellis:finish-work`; if dirty, return to Phase 3.4 first.
[/workflow-state:completed]

### Rules

1. Identify which Phase you're in, then continue from the next step there
2. Run steps in order inside each Phase; `[required]` steps can't be skipped
3. Phases can roll back (e.g., Execute reveals a prd defect → return to Plan to fix, then re-enter Execute)
4. Steps tagged `[once]` are skipped if the output already exists; don't re-run
5. Artifact presence informs the next step; missing `design.md` / `implement.md` is valid for lightweight tasks and incomplete planning for complex tasks.

<!-- BEGIN skill-garden patch workflow-active-task-routing v0.6 -->
### Active Task Routing

When a user request matches one of these intents inside an active task, enter the owning workflow capability before loading step detail:

- Planning or unclear requirements -> `trellis-brainstorm`.
- `in_progress` implementation -> `trellis-route(target=implement)`.
- `in_progress` check/check-all -> `trellis-route(target=check)`.
- Repeated debugging -> `trellis-break-loop`; spec updates -> `trellis-update-spec`.

The route result owns the inline/subagent choice. Do not infer execution mode from platform name or dispatch directly from this table.
<!-- END skill-garden patch workflow-active-task-routing v0.6 -->
### Guardrails

- Task creation approval is not implementation approval; implementation waits for `task.py start` after artifact review.
- PRD-only is valid for lightweight tasks; complex tasks need `design.md` + `implement.md`.
- Planning must be persisted to task artifacts; checks must run before reporting completion.

### Loading Step Detail

At each step, run this to fetch detailed guidance:

```bash
python3 ./.trellis/scripts/get_context.py --mode phase --step <step>
# e.g. python3 ./.trellis/scripts/get_context.py --mode phase --step 1.1
```

---

## Phase 1: Plan

<!-- BEGIN skill-garden patch workflow-phase-one-goal v0.6 -->
Goal: infer the request intent, enter planning automatically when authorized by explicit or high-confidence complex implementation intent, and produce the artifacts required before implementation.
<!-- END skill-garden patch workflow-phase-one-goal v0.6 -->

#### 1.0 Create task `[required · once]`

<!-- BEGIN skill-garden patch workflow-create-task-rule v0.6 -->
Create the task directory after explicit task intent or high-confidence complex implementation intent authorizes planning. Auto-routed creation should use `task_intent.py create` so request scope and the pre-planning dirty baseline are recorded. The task remains `planning`, writes `task.json`, creates a default `prd.md`, and targets the current session when identity is available:
<!-- END skill-garden patch workflow-create-task-rule v0.6 -->

<!-- BEGIN skill-garden patch workflow-create-task-command v0.6 -->
For inferred high-confidence complex implementation intent, preserve request scope and the dirty baseline:

```bash
python3 ./.trellis/scripts/task_intent.py create --title "<task title>" --slug <name>
```

For explicit user-requested task planning or a manually maintained task, use the ordinary creator:

```bash
python3 ./.trellis/scripts/task.py create "<task title>" --slug <name>
```
<!-- END skill-garden patch workflow-create-task-command v0.6 -->

`--slug` is the human-readable name only. Do **not** include the `MM-DD-` date prefix; `task.py create` adds that prefix automatically.

For task trees, create the parent task first and then create each child with `--parent <parent-dir>`. Do not start the parent just because children exist; start the child that owns the next independently verifiable deliverable.

After this command succeeds, the per-turn breadcrumb auto-switches to `[workflow-state:planning]`, telling the AI to stay in planning.

Run only `create` here — do not also run `start`. `start` flips status to `in_progress`, which switches the breadcrumb to the implementation phase before planning artifacts are reviewed. Save `start` for step 1.4.

Skip when `python3 ./.trellis/scripts/task.py current --source` already points to a task.

#### 1.1 Requirement exploration `[required · repeatable]`

Load the `trellis-brainstorm` skill and explore requirements interactively with the user per the skill's guidance.

The brainstorm skill will guide you to:
- Ask one question at a time
- Prefer researching over asking the user
- Prefer offering options over open-ended questions
- Update `prd.md` immediately after each user answer
- Split large scopes into a parent task plus child tasks when the deliverables can be verified independently
- Keep `prd.md` focused on requirements and acceptance criteria
- For complex tasks, produce `design.md` and `implement.md` before implementation starts

When considering a parent/child split:
- Use a parent task when one request contains several independently verifiable deliverables.
- Parent tasks own source requirements, child-task mapping, cross-child acceptance criteria, and final integration review.
- Child tasks own actual deliverables that can be planned, implemented, checked, and archived independently.
- Parent/child structure is not a dependency system. If child B depends on child A, write that ordering in child B's `prd.md` / `implement.md`.
- Start the child task that owns the next deliverable. Do not start the parent unless the parent itself has direct implementation work.

Return to this step whenever requirements change and revise the relevant artifact.

#### 1.2 Research `[optional · repeatable]`

Research can happen at any time during requirement exploration. It isn't limited to local code — you can use any available tool (MCP servers, skills, web search, etc.) to look up external information, including third-party library docs, industry practices, API references, etc.

[Claude Code, Cursor, OpenCode, codex-sub-agent, Kiro, Gemini, Qoder, CodeBuddy, Copilot, Droid, Pi, ZCode, Reasonix, Trae]

Spawn the research sub-agent:

- **Agent type**: `trellis-research`
- **Task description**: Research <specific question>
- **Key requirement**: Research output MUST be persisted to `{TASK_DIR}/research/`

[/Claude Code, Cursor, OpenCode, codex-sub-agent, Kiro, Gemini, Qoder, CodeBuddy, Copilot, Droid, Pi, ZCode, Reasonix, Trae]

[codex-inline, Kilo, Antigravity, Devin]

Do the research in the main session directly and write findings into `{TASK_DIR}/research/`. (For `codex-inline` this avoids the `fork_turns="none"` isolation that prevents `trellis-research` sub-agents from resolving the active task path.)

[/codex-inline, Kilo, Antigravity, Devin]

**Research artifact conventions**:
- One file per research topic (e.g. `research/auth-library-comparison.md`)
- Record third-party library usage examples, API references, version constraints in files
- Note relevant spec file paths you discovered for later reference

Brainstorm and research can interleave freely — pause to research a technical question, then return to talk with the user.

**Key principle**: Research output must be written to files, not left only in the chat. Conversations get compacted; files don't.

#### 1.3 Configure context `[required · once]`

[Claude Code, Cursor, OpenCode, codex-sub-agent, Kiro, Gemini, Qoder, CodeBuddy, Copilot, Droid, Pi, ZCode, Reasonix, Trae]

Curate `implement.jsonl` and `check.jsonl` so the Phase 2 sub-agents get the right spec/research context. These files were seeded on `task create` with a single self-describing `_example` line; your job here is to fill in real entries.

**Location**: `{TASK_DIR}/implement.jsonl` and `{TASK_DIR}/check.jsonl` (already exist).

**Format**: one JSON object per line — `{"file": "<path>", "reason": "<why>"}`. Paths are repo-root relative.

**What to put in**:
- **Spec files** — `.trellis/spec/<package>/<layer>/index.md` and any specific guideline files (`error-handling.md`, `conventions.md`, etc.) relevant to this task
- **Research files** — `{TASK_DIR}/research/*.md` that the sub-agent will need to consult

**What NOT to put in**:
- Code files (`src/**`, `packages/**/*.ts`, etc.) — those are read by the sub-agent during implementation, not pre-registered here
- Files you're about to modify — same reason

**Split between the two files**:
- `implement.jsonl` → specs + research the implement sub-agent needs to write code correctly
- `check.jsonl` → specs for the check sub-agent (quality guidelines, check conventions, same research if needed)

These manifests do not replace `implement.md`. `implement.md` is the human-readable execution plan for a complex task; jsonl files only list context files to inject or load.

**How to discover relevant specs**:

```bash
python3 ./.trellis/scripts/get_context.py --mode packages
```

Lists every package + its spec layers with paths. Pick the entries that match this task's domain.

**How to append entries**:

Either edit the jsonl file directly in your editor, or use:

```bash
python3 ./.trellis/scripts/task.py add-context "$TASK_DIR" implement "<path>" "<reason>"
python3 ./.trellis/scripts/task.py add-context "$TASK_DIR" check "<path>" "<reason>"
```

Delete the seed `_example` line once real entries exist (optional — it's skipped automatically by consumers).

Ready gate: both `implement.jsonl` and `check.jsonl` must contain at least one real `{"file": "...", "reason": "..."}` entry before `task.py start`. The seed `_example` row alone is not ready.

Skip this step only when both files already have real curated entries.

[/Claude Code, Cursor, OpenCode, codex-sub-agent, Kiro, Gemini, Qoder, CodeBuddy, Copilot, Droid, Pi, ZCode, Reasonix, Trae]

[codex-inline, Kilo, Antigravity, Devin]

Skip this step. Context is loaded directly by the `trellis-before-dev` skill in Phase 2.

[/codex-inline, Kilo, Antigravity, Devin]

#### 1.4 Activate task `[required · once]`

After artifact review, flip the task status to `in_progress`:

```bash
python3 ./.trellis/scripts/task.py start <task-dir>
```

For lightweight tasks, `prd.md` can be enough. For complex tasks, `prd.md`, `design.md`, and `implement.md` must exist and be reviewed before start. On sub-agent-dispatch platforms, `implement.jsonl` and `check.jsonl` must both have real curated entries before start. Runtime consumers tolerate missing or seed-only manifests for compatibility, but that tolerance is not a planning-ready state.

After this command succeeds, the breadcrumb auto-switches to `[workflow-state:in_progress]`, and the rest of Phase 2 / 3 follows.

If `task.py start` errors with a session-identity message (no context key from hook input, `TRELLIS_CONTEXT_ID`, or platform-native session env), follow the hint in the error to set up session identity, then retry.

#### 1.5 Completion criteria

| Condition | Required |
|------|:---:|
| `prd.md` exists | ✅ |
| User confirms task should enter implementation | ✅ |
| `task.py start` has been run (status = in_progress) | ✅ |
| `research/` has artifacts (complex tasks) | recommended |
| `design.md` exists (complex tasks) | ✅ |
| `implement.md` exists (complex tasks) | ✅ |

[Claude Code, Cursor, OpenCode, codex-sub-agent, Kiro, Gemini, Qoder, CodeBuddy, Copilot, Droid, Pi, ZCode, Reasonix, Trae]

| `implement.jsonl` and `check.jsonl` each contain at least one real curated entry (seed row does not count) | ✅ |

[/Claude Code, Cursor, OpenCode, codex-sub-agent, Kiro, Gemini, Qoder, CodeBuddy, Copilot, Droid, Pi, ZCode, Reasonix, Trae]

---

## Phase 2: Execute

Goal: turn reviewed planning artifacts into code that passes quality checks.

<!-- BEGIN skill-garden patch workflow-phase-2-implement v0.6 -->
#### 2.1 Implement `[required · repeatable]`

Implementation requires an active `in_progress` task and reviewed planning artifacts. Run `trellis-route(target=implement)` before editing or dispatching.

Follow the validated route result:

- `inline`: load `trellis-before-dev`, read the active task artifacts and referenced context, then implement and run focused verification.
- `subagent`: dispatch the selected implement agent with `Active task: <task path>` as the first prompt line; the agent implements directly and must not recursively dispatch implement/check agents.

Route preference recovery, fallback choices, and runtime evidence belong to `trellis-route`; do not reproduce them here.
<!-- END skill-garden patch workflow-phase-2-implement v0.6 -->
<!-- BEGIN skill-garden patch workflow-phase-2-check v0.6 -->
#### 2.2 Quality check `[required · repeatable]`

Run `trellis-route(target=check)`, then execute the unified `trellis-check-all` entry using the validated inline/subagent route.

Check-All selects light/full depth from intent, actual diff, risk, and runtime context. It is audit-only and collect-all by default: report all findings and stop before code changes until the user confirms the repair scope, unless a validated auto-loop owns the continuation.

After authorized repairs, return through the same route and re-run Check-All. The final pre-commit pass must cover the whole task and cannot be downgraded to light.
<!-- END skill-garden patch workflow-phase-2-check v0.6 -->
#### 2.3 Rollback `[on demand]`

- `check` reveals a prd defect → return to Phase 1, fix `prd.md`, then redo 2.1
- Implementation went wrong → revert code, redo 2.1
- Need more research → research (same as Phase 1.2), write findings into `research/`

---

## Phase 3: Finish

Goal: ensure code quality, capture lessons, record the work.

#### 3.2 Debug retrospective `[on demand]`

If this task involved repeated debugging (the same issue was fixed multiple times), load the `trellis-break-loop` skill to:
- Classify the root cause
- Explain why earlier fixes failed
- Propose prevention

The goal is to capture debugging lessons so the same class of issue doesn't recur.

<!-- BEGIN skill-garden patch workflow-phase-3-update-spec v0.6 -->
#### 3.3 Spec update `[required · once]`

Load `trellis-update-spec` and let it decide whether the task produced executable knowledge that must be recorded.

- `no-op`: continue without creating a spec change.
- `written`: include the necessary spec changes in the task's work batch.
- `needs-review`: stop for the single focused decision returned by the skill.

Do not ask a separate generic “update spec?” question before invoking the skill.
<!-- END skill-garden patch workflow-phase-3-update-spec v0.6 -->
<!-- BEGIN skill-garden patch workflow-phase-3-commit v0.6 -->
#### 3.4 Commit changes `[required · once]`

Load `trellis-push`. It owns dirty-file classification, exact file/message planning, one-shot confirmation, Git safety checks, ordinary commit + push, and current-task progress synchronization.

Ordinary mode defaults to commit and push. Commit-only is allowed only when the user explicitly requests a local commit or a validated auto-loop supplies its scoped preauthorization.

Do not run bare `git add`, `git commit`, or `git push` as a substitute for this phase.
<!-- END skill-garden patch workflow-phase-3-commit v0.6 -->
#### 3.5 Wrap-up reminder

After the above, remind the user they can run `/finish-work` to wrap up (archive the task, record the session).

---

## Customizing Trellis (for forks)

This section is for developers who want to modify the Trellis workflow itself. All customization is done by editing this file; the scripts are parsers only.

### Changing what a step means

Edit the corresponding step's walkthrough body in the Phase 1 / 2 / 3 sections above. Critical invariants:
<!-- BEGIN skill-garden patch workflow-customization-intent-invariant v0.6 -->
- No active task must infer the current request intent first; high-confidence reversible routing proceeds directly, while material ambiguity and independent safety boundaries still require confirmation.
<!-- END skill-garden patch workflow-customization-intent-invariant v0.6 -->
- Planning must distinguish lightweight PRD-only tasks from complex tasks that require `prd.md`, `design.md`, and `implement.md` before start.
- Every required execution path must keep the Phase 3.4 commit reminder reachable before `/trellis:finish-work`.

All tag blocks live in the `## Phase Index` section above, immediately after each phase summary:

| Scope | Corresponding tag |
|---|---|
| No active task (before Phase 1) | `[workflow-state:no_task]` (after the Phase Index ASCII art) |
| All of Phase 1 (task created → ready for implementation) | `[workflow-state:planning]` (after Phase 1 summary) |
| Codex inline Phase 1 | `[workflow-state:planning-inline]` |
| Phase 2 + Phase 3.2–3.4 (implementation + check + wrap-up) | `[workflow-state:in_progress]` (after Phase 2 summary) |
| Codex inline Phase 2 + Phase 3.2–3.4 | `[workflow-state:in_progress-inline]` |
| After Phase 3.5 (archived) | `[workflow-state:completed]` (after Phase 3 summary; **currently DEAD**) |

### Changing the per-turn prompt text

Directly edit the body of the corresponding `[workflow-state:STATUS]` block. After editing, run `trellis update` (if you're a template maintainer) or restart your AI session (if you're customizing your own project) — no script changes required.

### Adding a custom status

Add a new block:

```
[workflow-state:my-status]
your per-turn prompt text
[/workflow-state:my-status]
```

Constraints:
- STATUS charset: `[A-Za-z0-9_-]+` (underscores and hyphens allowed, e.g. `in-review`, `blocked-by-team`)
- A lifecycle hook must write `task.json.status` to your custom value, otherwise the tag is never read
- Lifecycle hooks live in `task.json.hooks.after_*` and bind to one of `after_create / after_start / after_finish / after_archive`

### Adding a lifecycle hook

Add a `hooks` field to your `task.json`:

```json
{
  "hooks": {
    "after_finish": [
      "your-script-or-command-here"
    ]
  }
}
```

Supported events: `after_create / after_start / after_finish / after_archive`. Note that `after_finish` ≠ a status change (it only clears the active-task pointer); use `after_archive` for "task is done" notifications.

### Full contract

For the workflow state machine's runtime contract, the locations of all status writers, pseudo-statuses (`no_task` / `stale_<source_type>`), the hook reachability matrix, and other deep details, see:

- `.trellis/spec/cli/backend/workflow-state-contract.md` — runtime contract + writer table + test invariants
- `.trellis/scripts/inject-workflow-state.py` — actual parser (reads workflow.md only, no embedded text)
