---
name: trellis-worktree
description: "Prepare and diagnose branch-local Trellis usage inside linked Git worktrees. Use when the user mentions worktree, linked worktree, worktree development, missing .trellis in a worktree, or parallel branch development."
---

# Trellis Worktree

Use this skill before normal Trellis routing for linked worktrees or parallel branch development. Each worktree loads its own branch-local Trellis, platform files, and runtime. Never share whole directories between worktrees.

## Create A Workspace

1. Resolve the selected project root before choosing a repository. When local Trellis is available, load:

```bash
python3 ./.trellis/scripts/get_context.py --mode packages
```

The default package identifies the project root, not the business submodule currently being discussed. Display each additional independent Git package and correlate Git packages with the engine's selected-commit submodule inventory. Only the selected root gets a new branch; never infer a child repository base from the root branch. Carry forward child bases/branches already specified by the user. Ask only for missing child choices when that repository is actually needed.

2. Request a read-only plan before writing task planning files. Omit `--base` to use the selected source branch; detached sources use `HEAD`. Task arguments are an optional pair:

```bash
flower-trellis worktree create --target <path> --branch <branch> [--base <ref>] \
  [--task-title <title> --task-slug <slug>] --json
```

3. Show one compact view: selected repository, resolved base, target branch/path, optional task, planned local writes, preparation issues, and any child repository decisions. Source dirty content is excluded from the base; summarize it without making cleanup or committing an upgrade a prerequisite. Explain that recoverable tool setup issues keep the new Git workspace.
4. Ask once to confirm the displayed plan, change its inputs, or cancel. On confirmation use the same plan fingerprint:

```bash
flower-trellis worktree create --target <path> --branch <branch> [--base <ref>] \
  [--task-title <title> --task-slug <slug>] \
  --yes --plan-fingerprint <fingerprint> --json
```

A `create-plan-changed` result requires review of the actual changed execution inputs. Unrelated source dirty changes and unselected child branch changes do not require confirmation. Never reuse a fingerprint for changed base commits or additional writes.
5. Read the result, including `setup.status`, rather than inferring full readiness from exit code 0:
   - `created`: Git and requested preparation completed.
   - `created-needs-prepare`: Git exists; report pending steps and use the preparation path below. Do not delete/recreate it or invent a native Git workaround.
   - `error`: report the stable reason and affected paths. If `worktreeCreated=true`, preserve that directory and its evidence.
6. Hand off once to a new AI session whose cwd/workspace root is `handoff.cwd`. Include task path/request, pending preparation, and already authorized child bases. Do not continue the source session inside the new worktree. Child preparation can use the user's existing authorization without asking again for the same base.

## Prepare Or Resume

For an existing directory, diagnose first:

```bash
flower-trellis worktree status --target <target> --json
```

- `ready-local` means local Trellis is usable. Trellis readiness does not prove Flower installation completeness; inspect `setup` and `flower` separately.
- `needs-prepare`: initialize or resume target-local state:

```bash
flower-trellis worktree prepare --target <target> [--developer <name>] --json
```

Prepare handles local identity/runtime and the target installation only. It never creates or resumes a task. For a task failure, preserve its files and run the returned remaining commands in the target session. Add `--inherit-route-prefs` only when the user wants that optional preference transfer.
- `needs-init`: the target branch lacks usable Trellis content. Keep the Git workspace and use the normal branch-local initialization flow; do not copy another branch's Trellis or upgrade the source to unblock creation.
- `needs-migration`: preview with `worktree migrate --target <target> --dry-run` before migration.
- `blocked`: stop the affected writes for real path, registry, or lock conflicts. Report paths and preserve existing content.

Default Flower preparation reads only the target's declaration, lock, and state; it never falls back to source records. It may refresh state or generate a missing ignored managed configuration only when the exact fixed package proves existing content needs no change. A stale source state or different source version is not a Git creation prerequisite. Missing packages, incompatible content, or additional approvals remain explicit preparation issues; never report them as an installed/up-to-date project.

Explicit record inheritance is still available when needed:

```bash
flower-trellis worktree prepare --target <target> --inherit-flower --source <source> --json
```

Use only a verified same-repository source. This explicit path retains strict source/target installation checks and preserves existing target records. Do not assemble records from unrelated branches, copy private platform configuration, rewrite hashes to approve arbitrary content, or install an update to bypass a conflict.

The registry stores the existing workspace mapping, never a setup request or task recovery cursor. Report each issue and the prerequisite for its recovery. Retry prepare after that prerequisite changes; do not loop the same failing command. Flower-only pending metadata does not prevent valid branch-local Trellis planning. Stop only work that actually depends on the unavailable tool or requires a new user decision.

## Boundaries

- `status` and create without `--yes` do not write project state. Creation preflight checks Git dependencies; target installation preparation runs after checkout without an external repository mirror.
- Source dirty content, task/session pointers, pre-check/untracked/auto-loop/Ralph state, caches, transactions, backups, and control/detached state are not inherited.
- Personal route/update policy inheritance remains normalized and limited to the same developer; preserve target preferences.
- Actual record inheritance requires the same canonical Git common dir and matching managed content. Target preparation uses the existing Plugin transaction and preserves existing versioned files.
- Submodules and independent Git packages are inventory only until separately selected; do not fetch, checkout, or copy every child repository automatically.
- Migration uses target HEAD content, never legacy sourceRoot content. Existing remove safety checks and branch preservation remain unchanged.
- Creating/preparing a workspace never approves task start, implementation, check, commit, merge, or push.
