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
- Git commits `b74db1362`, `10127a30e`, `2c76f9235`

## Drift Check

Missing `release.md`; generated from current task artifacts and committed implementation evidence.

## SQL Changes

- No manual SQL is required.
- Main-database AutoMigrate creates or updates `message_audit_requests`, `message_audit_blobs`, `message_audit_items`, and `message_audit_states` on SQLite, MySQL, or PostgreSQL.
- Existing audit records are not backfilled when the final model-name rule changes; only newly finalized requests use the consumption-log-aligned model name.

## Configuration Changes

- Configure the same `MESSAGE_AUDIT_SECRET` value, at least 32 bytes long, on every application node before enabling message auditing.
- Do not change or remove the secret while retained audit data still needs to be decrypted. The first version does not support online key rotation or multiple read keys.
- `MessageAuditEnabled` remains disabled until a root administrator enables it. `MessageAuditRetentionDays` must remain within `1-30` days.

## Batch / Deployment Scripts / Data Repair

- No one-time script or automatic data repair is required.
- Use the root-only asynchronous cleanup task before database-level maintenance when old audit payloads must be deleted.
- Deleting rows can leave reusable allocated table space; physical shrinking remains a separate database maintenance operation.

## External Systems / Dependent Platforms

None.

## Release Order

1. Configure and verify the same `MESSAGE_AUDIT_SECRET` on every node.
2. Deploy the application code and allow main-database AutoMigrate to complete.
3. Verify `/api/message-audit/status` reports the encryption key as configured.
4. Enable message auditing from the root log-maintenance settings.
5. Verify new requests are captured before relying on the audit page for operational review.

## Rollback Notes

- Disable message auditing first and allow the asynchronous queue to drain before rolling back application code.
- The audit tables and options can remain after code rollback; older code ignores them.
- If data removal is required, run the cleanup task using the deployed version before database-level table maintenance.

## Post-release Verification

- With a root account, verify auditing can be enabled and disabled and that non-root users cannot access the page or API.
- Verify list filtering, inferred-session grouping, compression count, compressed-boundary highlighting, and in-detail session-history switching.
- Compare a newly finalized audit request with its consumption log by request ID and confirm the displayed model names match.
- Verify role/content-type menus open without Base UI runtime errors and preserve original message order after filtering.
- Verify `payload_bytes` decreases after cleanup while `storage_bytes` may remain allocated, and requests arriving after the cleanup cutoff remain available.
- Verify decryption succeeds on every node to detect inconsistent `MESSAGE_AUDIT_SECRET` values.
