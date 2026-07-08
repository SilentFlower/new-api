# Finish Work

### HIGHEST PRIORITY: skill-garden finish-work release operations override

<!-- BEGIN skill-garden skill override trellis-finish-work v0.6 -->

> Source: github.com/SilentFlower/skill-garden. This block is an injected override for the local Trellis finish-work entry; it is not a maintained copy of the upstream `trellis-finish-work` skill.

#### Release Operations Inference Step

Run this step after finish-work Step 2 dirty-path classification succeeds and before finish-work Step 3 archives task(s).

This step is non-blocking. Do not add an extra user confirmation question, and do not block finish-work solely because `release.md` is absent.

This step must be evidence-based. If the conversation was compacted, resumed, or interrupted, do not rely on memory from the earlier context. Re-read the task files and git evidence before deciding whether release operations exist.

For the active task, read the available task files: `task.json`, `prd.md`, `design.md`, `implement.md`, `implement.jsonl`, `check.jsonl`, and any existing `release.md`. Also use recent work commits, `git log --oneline --name-only`, `git show --name-only <hash>` when a work commit is known, `git diff --name-only`, and the dirty-path classification already gathered during finish-work preflight.

If `<task>/release.md` already exists, compare it with the task requirements, implementation plan, check context, changed files, and commit evidence. Preserve it when it is still accurate. Update it only when the current task context shows an obvious missing release operation or obvious document drift. If drift is plausible but not certain, keep the risk visible and mark the conclusion as `Needs human review`.

If no `release.md` exists:

- High-confidence release work exists: write `<task>/release.md`.
- High-confidence no release work exists after re-reading task files and git evidence: do not create `release.md`; mention in the final finish-work report that no release operations were identified.
- Signals are uncertain but release risk exists: write `<task>/release.md` and mark the conclusion as `Needs human review`.

Release-operation signals, with the `release.md` section each maps to: SQL or migrations (`SQL Changes`); configuration, environment variables, feature flags, permissions, secrets, or external endpoints (`Configuration Changes`); deployment scripts, one-off commands, data repair, scheduled task triggers, background job reruns, or other batch operations (`Batch / Deployment Scripts / Data Repair`); and external systems or dependent platforms outside the current repository that must be released or coordinated, such as H0 API relay / gateway platforms, messaging platforms, or third-party admin consoles (`External Systems / Dependent Platforms`).

When writing or updating `release.md`, use this structure:

```markdown
# Release Operations

## Conclusion
Release operations exist. / No release operations identified. / Needs human review.

## Evidence Checked
- task.json
- prd.md
- design.md / implement.md / implement.jsonl / check.jsonl
- release.md
- git commits / changed files

## Drift Check
Existing release.md is accurate. / Missing release.md. / Drift suspected. / Needs human review.

## SQL Changes
None

## Configuration Changes
None

## Batch / Deployment Scripts / Data Repair
None

## External Systems / Dependent Platforms
None

## Release Order
No special order.

## Rollback Notes
Rollback code only.

## Post-release Verification
Verify according to task acceptance criteria.
```

Do not write `None`, `No release operations identified`, or `Rollback code only` out of habit. Use those defaults only after checking the task files and git evidence. If the task changed deployment scripts, configuration, SQL, external platforms, permissions, scheduled jobs, or data, record the concrete item and source.

If multiple tasks will be archived in the same finish-work run, process the active task at minimum. Process extra archived tasks only when Step 1 provides enough local context to infer safely; do not add per-task confirmation prompts.

#### Finish Bookkeeping Auto-push Step

Run this step after finish-work Step 4 records the session journal and produces the `chore: record journal` commit, before the final finish-work report.

Read the active task's `last_push_snapshot.push_mode` with `python3 ./.trellis/scripts/push_snapshot.py status --json` before archive moves the task. If `push_mode` is `"commit-only"` or missing, do not auto-push finish-work's archive/journal commits; report that local bookkeeping commits remain ahead.

If `push_mode` is not `"commit-only"`, Step 4 left `git status --porcelain` clean, and the current branch has an upstream, push the current branch:

```bash
git push origin <current_branch>
```

Never force push. If push fails, stop and report the failure.

<!-- END skill-garden skill override trellis-finish-work v0.6 -->

Wrap up the current session: archive the active task (and any other completed-but-unarchived tasks the user wants to clean up) and record the session journal. Code commits are NOT done here — those happen in workflow Phase 3.4 before you invoke this command.

## Step 1: Survey current state

```bash
python3 ./.trellis/scripts/get_context.py --mode record
```

This prints:

- **My active tasks** — review whether any besides the current one are actually done (code merged, AC met) and should be archived this round.
- **Git status** — quick visual on what's dirty.
- **Recent commits** — you'll need their hashes in Step 4 for `--commit`.

If `--mode record` surfaces other completed tasks not tied to the current session, surface them to the user with a one-shot confirmation: "These N tasks look done — archive them too in this round? [y/N]". Default is no; the current active task is always archived in Step 3 regardless.

## Step 2: Sanity check — classify dirty paths

Run:

```bash
git status --porcelain
```

Filter out paths under `.trellis/workspace/` and `.trellis/tasks/` — those are managed by `add_session.py` and `task.py archive` auto-commits and will appear dirty as part of this skill's own work.

For each remaining dirty path, decide whether it belongs to **the current task** or to **other parallel work** (e.g., another terminal window editing the same repo). Heuristics:

- Paths referenced in the current task's `prd.md` / `implement.jsonl` / `check.jsonl` → current task
- Paths in code areas matching the task's stated scope, or that you remember editing this session → current task
- Paths in unrelated areas you have no recollection of touching this session → other parallel work

Then route:

- **Any remaining path looks like current-task work** — bail out with:
  > "Working tree has uncommitted code changes from this task: `<list>`. Return to workflow Phase 3.4 to commit them before running `/trellis:finish-work`."

  Do NOT run `git commit` here. Do NOT prompt the user to commit. The user goes back to Phase 3.4 and the AI drives the batched commit there.
- **All remaining paths look unrelated** (other parallel-window work) — report them once and continue to Step 3:
  > "FYI, dirty files outside this task's scope — leaving them for the other window: `<list>`."
- **Genuinely unsure** — ask the user once: "Are `<list>` this task's work I forgot to commit, or another window's? (commit / ignore)" — then route per their answer.

## Step 3: Archive task(s)

```bash
python3 ./.trellis/scripts/task.py archive <task-name>
```

At minimum: the current active task (if any). Plus any extra tasks the user confirmed in Step 1. Each archive produces a `chore(task): archive ...` commit via the script's auto-commit.

If there is no active task and the user did not confirm any cleanup archives, skip this step.

## Step 4: Record session journal

```bash
python3 ./.trellis/scripts/add_session.py \
  --title "Session Title" \
  --commit "hash1,hash2" \
  --summary "Brief summary"
```

Use the work-commit hashes produced in Phase 3.4 (visible in Step 1's `Recent commits` list, or via `git log --oneline`) for `--commit`. Do not include the archive commit hashes from Step 3. This produces a `chore: record journal` commit.

Final git log order: `<work commits from 3.4>` → `chore(task): archive ...` (one or more) → `chore: record journal`.
