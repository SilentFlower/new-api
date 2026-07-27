# Release Operations

## Conclusion

Release operations exist.

## Evidence Checked

- `task.json`
- `prd.md`
- `design.md`
- `implement.md`
- `implement.jsonl`
- `check.jsonl`
- Git commits `8b74cf21c` and `38b08dad0`
- Current Git status and changed-file evidence

## Drift Check

Missing `release.md`; this file records the deployment, configuration, rollback, and production verification requirements found in the task artifacts and Git evidence.

## SQL Changes

- No manual SQL is required.
- Deploying the feature runs the existing GORM `AutoMigrate` path, which adds the nullable integer column `channels.user_concurrency_limit` on SQLite, MySQL, and PostgreSQL.
- After deployment, verify that the column exists and historical channels continue to behave as unlimited when the value is `NULL` or `0`.

## Configuration Changes

- Keep production Redis available before enabling a positive limit. When Redis is configured but unavailable, new limited requests fail closed with `503 channel_user_concurrency_unavailable`.
- Set channel `80` field `user_concurrency_limit` to `4` and verify the management API, database, and channel cache return the same value.
- Keep `ERROR_LOG_ENABLED=true` so `429/503` concurrency errors are persisted and searchable in the management error log. Runtime warnings remain available when this switch is disabled, but database error-log records do not.

## Batch / Deployment Scripts / Data Repair

None.

## External Systems / Dependent Platforms

None.

## Release Order

1. Deploy the feature and error-log patch, including commits `8b74cf21c` and `38b08dad0`.
2. Confirm database migration, Redis availability, and `ERROR_LOG_ENABLED=true`.
3. Confirm channel `80` is configured with `user_concurrency_limit=4` and its cache is refreshed.
4. Run the post-release verification below before considering the rollout complete.

## Rollback Notes

- First set channel `80` field `user_concurrency_limit` to `0`; this disables the limit immediately without dropping the database column.
- If code rollback is required, retain the nullable column because older code ignores it.
- Redis lease keys expire automatically through their TTL and do not require bulk deletion.

## Post-release Verification

- Hold four concurrent upstream requests for the same user on channel `80`; the fifth request must return `429 channel_user_concurrency_exceeded` without reaching the upstream or charging quota.
- Confirm another user can still enter channel `80`, and multiple API tokens belonging to the same user share the limit of four.
- Confirm the management error log contains exactly one record for the rejected request, with `channel_id=80`, `limit=4`, the stable error code, and the matching request ID under administrator-only information.
- Confirm the concurrency rejection does not disable channel `80` or trigger channel retry.
- Verify Redis lease members and TTL are created, renewed, and released during the request lifecycle.
- During a controlled Redis outage, confirm new limited requests return `503 channel_user_concurrency_unavailable`; restore Redis and confirm normal acquisition resumes.
