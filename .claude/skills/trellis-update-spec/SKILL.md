---
name: trellis-update-spec
description: "Captures executable contracts and coding conventions into .trellis/spec/ documents. Use when learning something valuable from debugging, implementing, or discussion that should be preserved for future sessions."
---

# Update Code-Spec - Capture Executable Contracts

When you learn something valuable (from debugging, implementing, or discussion), use this to update the relevant code-spec documents.

**Timing**: After completing a task, fixing a bug, or discovering a new pattern

---

## Code-Spec First Rule (CRITICAL)

In this project, "spec" for implementation work means **code-spec**:
- Executable contracts (not principle-only text)
- Concrete signatures, payload fields, env keys, and boundary behavior
- Testable validation/error behavior

If the change touches infra or cross-layer contracts, code-spec depth is mandatory.

### Mandatory Triggers

Apply code-spec depth when the change includes any of:
- New/changed command or API signature
- Cross-layer request/response contract change
- Database schema/migration change
- Infra integration (storage, queue, cache, secrets, env wiring)

<!-- BEGIN skill-garden patch trellis-update-spec-examples-output v0.6 -->
### Mandatory Output (6 Sections)

For triggered tasks, include all sections below:
1. Scope / Trigger
2. Signatures (command/API/DB)
3. Contracts (request/response/env)
4. Validation & Error Matrix
5. Scenarios and Examples (normal/base cases and at least one incorrect use with its correction)
6. Tests Required (with assertion points)
<!-- END skill-garden patch trellis-update-spec-examples-output v0.6 -->

---

<!-- BEGIN skill-garden patch trellis-update-spec-prune-learning-guidance v0.6 -->
<!-- END skill-garden patch trellis-update-spec-prune-learning-guidance v0.6 -->

## Update Templates

<!-- BEGIN skill-garden patch trellis-update-spec-examples-template v0.6 -->
### Mandatory Template for Infra/Cross-Layer Work

```markdown
## Scenario: <name>

### 1. Scope / Trigger
- Trigger: <why this requires code-spec depth>

### 2. Signatures
- Backend command/API/DB signature(s)

### 3. Contracts
- Request fields (name, type, constraints)
- Response fields (name, type, constraints)
- Environment keys (required/optional)

### 4. Validation & Error Matrix
- <condition> -> <error>

### 5. Scenarios and Examples
- Normal: ...
- Base/boundary: ...
- Incorrect use: ...
- Correct handling: ...

### 6. Tests Required
- Unit/Integration/E2E with assertion points
```
<!-- END skill-garden patch trellis-update-spec-examples-template v0.6 -->

<!-- BEGIN skill-garden patch trellis-update-spec-prune-writing-examples v0.6 -->
<!-- END skill-garden patch trellis-update-spec-prune-writing-examples v0.6 -->

<!-- BEGIN skill-garden patch trellis-update-spec-autonomous-evaluation v0.6 -->
## Autonomous Spec Evaluation

This section replaces the interactive “whether to update” decision. The Code-Spec First Rule remains authoritative when an update is necessary. Package/layer code-specs hold executable contracts; shared guides hold short thinking checklists and pointers, not duplicate contract details.

### Result Contract

Every invocation must evaluate the available evidence autonomously and return exactly one result:

```yaml
spec_update_result:
  status: no-op | written | needs-review
  reason: string
  evidence: [string]
  changed_files: [path]
  validation: [string]
```

- `no-op`: no reusable executable contract was learned, the existing specs already cover it, the change is only a one-off implementation/copy/formatting detail, or the user explicitly asked to skip spec updates in the current request.
- `written`: code or test evidence supports a new executable contract, one authoritative target spec is unambiguous, and the minimal write plus focused validation completed successfully.
- `needs-review`: the target spec, business semantics, conflict resolution, or a validation failure cannot be resolved uniquely from repository evidence.

Do not enter the upstream Interactive Mode and do not ask whether a spec should be updated. Only `needs-review` may stop the workflow, and it may ask only one question that is necessary to resolve the current ambiguity.

### Evidence Order

Reuse already-read, unchanged evidence in this order; read missing or changed material. Do not decide from the chat summary, task title, or intuition alone:

1. For task context, the current task's `implement.jsonl` / `check.jsonl` and every file they reference.
2. For task context, the current task's `prd.md`, `design.md`, and `implement.md`.
3. For untracked context, `untracked_flow.py status --verbose` for work id, summary, and stage; obtain validation evidence from the current Check-All result and current change set rather than the cursor helper.
4. The final Check-All conclusion and its actual validation evidence, or, for an eligible quick direct edit, the actual successful focused validation commands, results, and covered scope.
5. The current subject's actual diff, source code, tests, and commit evidence.
6. Existing specs and their indexes returned by `spec_router.py`.

When there is no active task, use a valid current-session untracked state when present; otherwise accept an explicit Update-Spec invocation, a passed Check-All continuation authorized by its owner, or the quick direct edit completion authorized by `workflow-state:no_task`. For quick entry, verify that the actual diff still meets `Request Triage` eligibility and that current successful focused validation covers it; missing, failed, or stale evidence cannot substitute for this handoff. Use the current request, actual diff, source/tests, and existing specs. Do not invent task evidence or a Check-All pass.

### Minimal Write Boundary

Capture the current dirty baseline before writing. `written` requires all of the following:

- Every change made by this Update-Spec invocation is under `.trellis/spec/**`. Do not modify business code, tests, workflow, skills, task artifacts, or any other file.
- Modify the smallest required section in the fewest files. Do not opportunistically rewrite, expand, reorganize, or format unrelated content.
- Prefer an existing authoritative spec. Create a new file only when no suitable spec exists, and update the corresponding index in the same invocation.
- Do not write a generic principle merely to avoid `no-op`. New content must provide a concrete executable contract such as signatures, fields, boundaries, error matrices, examples, or test assertions, while following the Code-Spec First Rule.

After writing, reread the spec diff and reverse-check it against source code and tests. At minimum run:

```bash
git diff --check -- .trellis/spec
```

When applicable, also validate indexes/links, code signatures, or project-specific spec checks. Fix uniquely resolvable validation failures inside this skill and rerun validation. Return `needs-review` for failures that cannot be resolved uniquely. If this invocation creates changes outside `.trellis/spec/**`, return `needs-review` with `reason=boundary-violation`, stop immediately, and do not proceed to Push. A completed `written` result does not trigger another manual Check-All.

### Workflow Disposition

- Interactive: follow the continuation decision made by Check-All's `Interactive Post-Check Stop Gate`, including strict pass and accepted-risk pass. A passed check defaults to this skill in the same turn after the Check-All report or brief acceptance acknowledgment; an explicit check-only or stop-after-check request keeps the chain stopped. When the user later says “下一步”, “继续”, `next`, `continue`, or an equivalent continuation intent, resume the deliberately stopped chain. Reuse a currently valid `spec_update_result` instead of evaluating again.
- Quick direct edit: accept the eligible no-task handoff above without requiring a Check-All report, task, or untracked cursor. Evaluate the focused validation and actual diff, then use the same result and Push continuation contract; an explicit user instruction to stop or defer completion keeps the chain stopped.
- Interactive completion, including direct Git: when the gate permits continuation, a `no-op` or `written` result must load `trellis-push` in the same turn and present its single confirmation plan; if there are no deliverable changes, report that no commit is needed instead of manufacturing an empty commit. Only `no-op` or `written` may proceed to `trellis-push`; `needs-review` stops and must not generate a Push plan. Reuse valid acceptance evidence internally without repeating unchanged risks. Default continuation does not authorize Git writes; do not infer risk acceptance or Git write authorization from unrelated history, summaries, dirty state, or an auto-loop internal `commit-only`. Do not treat an agent-authored "no commit/push" restriction in task artifacts or ordinary implementation-plan approval as an explicit user stop instruction; stopping this handoff requires an explicit user instruction, and pending Git write authorization does not block presenting the Push plan.
- Validated auto-loop: for `no-op` or `written`, execute `record --action run_spec_update --result ok`; only after a successful record immediately run `next`. If record returns `status=retryable` with an artifact recovery diagnosis, follow `trellis-auto-loop` recovery instructions in the same turn, then resubmit the original truthful record; do not advance or request user continuation. For `needs-review`, execute `record --action run_spec_update --result blocked --failure-type spec-needs-review`; never disguise it as `no-op`.
- Untracked: keep the cursor at `spec` for `needs-review`. For `no-op` or `written`, run `untracked_flow.py advance --stage push`. Any later product edit returns the cursor to `implement`; the helper does not validate or preserve owner evidence.

Do not ask again or rerun when a currently valid `no-op` or `written` result already exists. Re-evaluate after the actual diff, Check-All or focused validation conclusion, or the user's spec intent changes.
<!-- END skill-garden patch trellis-update-spec-autonomous-evaluation v0.6 -->
## Quality Checklist

Before finishing your code-spec update:

- [ ] Is the content specific and actionable?
- [ ] Did you include a code example?
- [ ] Did you explain WHY, not just WHAT?
- [ ] Did you include executable signatures/contracts?
- [ ] Did you include validation and error matrix?
<!-- BEGIN skill-garden patch trellis-update-spec-examples-checklist v0.6 -->
- [ ] Did Scenarios and Examples cover normal/base cases and incorrect use with its correction?
<!-- END skill-garden patch trellis-update-spec-examples-checklist v0.6 -->
- [ ] Did you include required tests with assertion points?
- [ ] Is it in the right code-spec file?
- [ ] Does it duplicate existing content?
- [ ] Would a new team member understand it?

---

<!-- BEGIN skill-garden patch trellis-update-spec-command-relationship v0.6 -->
## Relationship to Other Capabilities

```
Development Flow:
  Learn something → `update-spec` (Trellis capability) → Knowledge captured
       ↑                                  ↓
  `break-loop` (Trellis capability) ←──────────────────── Future sessions benefit
  (deep bug analysis)
```

- `break-loop` analyzes bugs deeply and often reveals spec updates needed.
- `update-spec` evaluates and writes durable project guidance.
- Check-All decides whether the current result may continue to Update-Spec and Push.
<!-- END skill-garden patch trellis-update-spec-command-relationship v0.6 -->
<!-- BEGIN skill-garden patch trellis-update-spec-prune-philosophy v0.6 -->
<!-- END skill-garden patch trellis-update-spec-prune-philosophy v0.6 -->
