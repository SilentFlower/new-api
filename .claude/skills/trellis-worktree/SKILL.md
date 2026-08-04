---
name: trellis-worktree
description: "Prepare and diagnose Trellis usage inside linked Git worktrees. Use when the user mentions worktree, linked worktree, worktree development, missing .trellis in a worktree, or asks to make .claude/.codex/.agents/Trellis files work from a worktree."
---

# Trellis Worktree

Use this skill before normal Trellis routing when the user's current request is about using Trellis from a linked Git worktree.

The problem is usually not task or untracked routing itself: linked worktrees often do not contain the untracked `.trellis`, `.agents`, `.codex`, or `.claude` directories that AI tools need before any Trellis hook or skill can run.

## Workflow

1. Identify the target worktree. Use the user's explicit path if present; otherwise use the current working directory.
2. Locate a source worktree in the same Git worktree set that contains `.trellis/scripts/worktree_setup.py`.
3. Run read-only diagnosis first:

```bash
python3 <source-worktree>/.trellis/scripts/worktree_setup.py status --target <target-worktree> --json
```

4. If status is `needs-prepare`, run:

```bash
python3 <source-worktree>/.trellis/scripts/worktree_setup.py prepare --target <target-worktree> --json
```

5. If status is `blocked` or `error`, stop and report the reason and conflict paths. Do not overwrite or delete user files.
6. After `ready` or `prepared`, return to the user's original Trellis intent and follow the normal workflow state.

## Source Worktree Discovery

If `<source-worktree>` is not obvious, inspect Git worktrees:

```bash
git -C <target-worktree> worktree list --porcelain
```

Choose a listed worktree that contains `.trellis/scripts/worktree_setup.py`. If none exists, report that this project has not been updated with `trellis-worktree` support yet.

## Safety Rules

- The helper only projects source paths that already exist.
- MVP projection paths are `.trellis`, `.agents`, `.codex`, and `.claude`.
- Projection uses symlinks and records `<target-worktree>/.trellis-worktree.json`.
- Existing non-managed `.codex`, `.claude`, `.agents`, or `.trellis` paths are conflicts.
- Do not hand-copy platform directories. Copying creates drift between worktrees.
- Do not treat successful setup as task approval, check approval, or push approval.

## Expected Results

- `ready`: target worktree already has valid Trellis entry points.
- `needs-prepare`: target worktree is safe to prepare; run `prepare`.
- `prepared`: symlinks and manifest were created or repaired.
- `blocked`: user-owned paths would be overwritten; stop and report `conflicts`.
- `error`: Git or source-root resolution failed; stop and report `reason`.
