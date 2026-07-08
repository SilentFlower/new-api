# Release Operations

## Conclusion
Release operations exist.

## Evidence Checked
- task.json
- prd.md
- design.md / implement.md / implement.jsonl / check.jsonl
- release.md: missing before finish-work
- git commits / changed files: 8471e8a6 changed main.go, model/log.go, model/usedata.go, model/token_statistics.go, model/token_statistics_test.go, web/classic/src/pages/LogViewer/index.jsx

## Drift Check
Missing release.md. This file records the deployment and data repair operations implied by the task requirements and implementation.

## SQL Changes
No schema changes.

## Configuration Changes
None.

## Batch / Deployment Scripts / Data Repair
- Deploy new-api code containing commit 8471e8a6 or later.
- On service startup, `model.StartQuotaDataTokenUsedMigration()` runs on the master node and performs a versioned background delta migration for historical `quota_data.token_used`.
- Verify logs for either:
  - `quota_data token_used stats migrated to version 2, buckets=...`
  - `failed to migrate quota_data token_used stats: ...`
- Verify the options table / runtime option map records `QuotaDataTokenUsedStatsVersion = 2` after a successful migration.
- The migration only adds cache delta to existing `quota_data` buckets. It does not rebuild missing historical buckets.

## External Systems / Dependent Platforms
- ai-fund frontend depends on new-api `/api/log/token/stat` returning `total_tokens`.
- ai-fund Pages was updated separately in commit 5a768cf to display total/input/output/cache consistently. Before new-api is deployed, ai-fund may show `缓存 0` because it falls back to `prompt_tokens + completion_tokens`.

## Release Order
1. Deploy ai-fund frontend if not already deployed.
2. Deploy new-api commit 8471e8a6 or later.
3. Restart new-api and let the master node run the quota_data migration.
4. Verify new-api token stat APIs and ai-fund log page.

## Rollback Notes
- Rolling back code does not undo `quota_data.token_used` values already migrated to the new statistic口径.
- If the old statistic口径 must be restored, rebuild or repair `quota_data.token_used` back to `prompt_tokens + completion_tokens` from logs.
- User quota balance and billing amounts are not changed by this task.

## Post-release Verification
- Call `/api/log/token/stat` through ai-fund and confirm `total_tokens >= prompt_tokens + completion_tokens`.
- Confirm ai-fund top card shows non-zero cache when `total_tokens` includes Anthropic cache tokens.
- Confirm old UI token charts and export Sheet 1/2 use the new total Token口径.
- Confirm Sheet 3 and log details still show ordinary input, cache read, cache write, and output separately.
