# Release Operations

## Conclusion

Release operations exist.

## Evidence Checked

- `task.json`, `prd.md`, `design.md`, `implement.md`, `brief.md`
- `implement.jsonl`, `check.jsonl`
- Git commits `b9af4ba00`, `3276b7d54`
- NewAPI database migration, channel management API, relay limit enforcement, and administrator UI changes

## Drift Check

Missing `release.md`; this file records the migration, deployment order, rollback order, and post-release verification required by the implemented code.

## SQL Changes

- No manual SQL is required.
- Deploying NewAPI runs the existing GORM migration path to add the channel weekly-limit field and the personal limit override table.
- The migration must remain compatible with SQLite, MySQL, and PostgreSQL; new fields default to unlimited behavior and existing data does not require backfill.

## Configuration Changes

None. The feature reuses the existing database, Redis, administrator authentication, and channel configuration mechanisms.

## Batch / Deployment Scripts / Data Repair

- Deploy the NewAPI build containing weekly limits and personal limit overrides before deploying the dependent ai-fund Worker and frontend.
- No one-time batch or data repair is required.

## External Systems / Dependent Platforms

- ai-fund depends on the new weekly usage, unified user status, user search, and personal override management APIs.
- Existing ai-fund daily quota and concurrency behavior remains compatible while NewAPI is upgraded first.

## Release Order

1. Deploy NewAPI and allow its database migration to complete.
2. Verify existing ai-fund daily quota and concurrency reads still work.
3. Deploy the ai-fund Worker and frontend containing weekly limits and personal override management.

## Rollback Notes

- Roll back ai-fund before rolling back NewAPI because the new ai-fund version depends on the new management APIs.
- The new table and columns may remain in the database; older NewAPI versions ignore them.
- Do not delete weekly usage or personal override records during rollback.

## Post-release Verification

- Verify unlimited, daily-only, weekly-only, concurrency-only, and combined daily/weekly configurations.
- Verify daily and weekly limits independently return `429` when exceeded.
- Verify a personal override applies immediately, supports optional expiration, and falls back to the channel default after expiration.
- Verify administrators can search and configure a user with no prior request record.
- Verify daily/weekly usage adjustments do not change wallet quota, subscriptions, logs, or personal overrides.
- Verify ai-fund Worker responses do not expose administrator PATs, channel keys, complete channel objects, or D1 internals.
