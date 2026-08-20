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
- Git commits `977cf6dd4` and `94bfac30b`

## Drift Check

Missing release.md. The task documents consistently identify AI Fund as the downstream consumer.

## SQL Changes

None.

## Configuration Changes

None.

## Batch / Deployment Scripts / Data Repair

None.

## External Systems / Dependent Platforms

- `[admin-user-billing-summary]` AI Fund downstream task `/root/project/ai-fund/.trellis/tasks/08-20-admin-user-subscription-optimization` must consume `POST /api/user/billing-summary`. This task does not deploy or modify AI Fund.

## Release Order

1. Deploy NewAPI commit `977cf6dd4` and confirm the administrator batch endpoint is available.
2. Deploy the AI Fund downstream change that replaces per-user requests and separately displays portal and NewAPI status.

## Rollback Notes

- No database rollback is required.
- If the AI Fund consumer is already deployed, roll it back before removing the NewAPI batch endpoint.
- The NewAPI change can then be reverted by removing the additive route and related implementation files.

## Post-release Verification

- Verify an administrator can call `POST /api/user/billing-summary` and unauthorized or ordinary users are rejected.
- Verify `remote_status`, `remote_role`, wallet quota, subscription summary and `sort_key` match the documented contract.
- Verify AI Fund displays portal status and NewAPI status separately and list refresh does not trigger account synchronization.
