# Release Operations

## Conclusion

Release operations exist and were completed on 2026-07-22.

## Evidence Checked

- `task.json`
- `prd.md`
- `design.md`
- `implement.md`
- `implement.jsonl`
- `check.jsonl`
- Business commit `186c123ff`
- Task progress commit `b9314bbcc`
- Production Docker image revision and post-deployment database verification

## Drift Check

Missing `release.md` was added. The task PRD excludes historical data repair from the code change, while the production correction was completed separately during delivery and is recorded here for operational traceability.

## SQL Changes

None in the repository. No schema migration was introduced.

## Configuration Changes

None.

## Batch / Deployment Scripts / Data Repair

- Deployed `ghcr.io/silentflower/new-api:build-bak-latest` with image revision `186c123ff`.
- Production container started at 2026-07-22 12:01:49 Asia/Shanghai; the last old-version abnormal consume log was at 12:00:07.
- Corrected 36 `type=2` records matching `client_gone + local_count_tokens=true`, totaling 39,908,031 quota.
- Refunded user and token remaining quota, reverted user/channel/token used quota, and reduced user request count by 36.
- Reverted `quota_data` by count 36, quota 39,908,031, and token_used 11,185,826.
- Converted the affected consume logs to `type=5`, `quota=0`, retaining original usage and the correction ID `refund_client_gone_local_20260722_postdeploy` under `admin_info.manual_correction`.
- Cleared Redis `user:*` and `token:*` quota caches after the transaction.
- Rollback snapshots remain in production MySQL tables named `repair_cglu_20260722_*`.
- The correction is complete and must not be run again against the same log IDs.

## External Systems / Dependent Platforms

- GitHub Actions built the image from the commit carrying `[build]`.
- The production Docker deployment and MySQL/Redis correction were performed on `www.havefun.eu.cc`.

## Release Order

1. Build image from revision `186c123ff`.
2. Deploy the new image and verify the running image revision.
3. Freeze the old-version abnormal log set and create rollback snapshots.
4. Apply the transactional data correction.
5. Clear quota caches and run post-correction verification.

All steps are complete.

## Rollback Notes

- Rolling back the application image would restore the billing defect and is not recommended.
- The `repair_cglu_20260722_*` tables are rollback evidence. Do not overwrite current user, channel, token, or quota_data rows directly from snapshots because valid traffic continued after the snapshot.
- Any data rollback must reverse the correction deltas by the frozen log IDs while preserving later legitimate traffic.

## Post-release Verification

- Running production image revision: `186c123ff`.
- Remaining matching abnormal `type=2` records: 0.
- Corrected error audit records: 36; original quota total: 39,908,031.
- User, channel, and token balances matched the snapshot plus correction and post-snapshot legitimate traffic.
- Old-node `quota_data` delta matched count 36, quota 39,908,031, and token_used 11,185,826.
- No new matching abnormal consumption was observed through 2026-07-22 12:15:34 Asia/Shanghai.
