# Release Operations

## Conclusion

Release operations exist. The rc.24 merge changes authentication sessions, database schema, frontend layout, and deployment configuration. Complete the configuration and migration checks below before rolling out all nodes.

## Evidence Checked

- `task.json`, `prd.md`, `design.md`, `implement.md`
- `implement.jsonl`, `check.jsonl`, `merge-report.md`
- Merge commit `9af37f5b28dd61fc99e01465e580640b6e908ceb`
- `model/main.go`, authentication models, `.env.example`, `docker-compose.yml`
- `docs/authentication.md`

## Drift Check

Missing `release.md`; this file records the release requirements identified from the task and Git evidence.

## SQL Changes

- No manual SQL script is required.
- The master node runs GORM migrations at startup. The rollout adds or updates the authentication schema, including `users.auth_version`, `user_sessions`, `auth_flows`, and `external_identity_claims`.
- Startup also initializes user authentication versions and backfills legacy Telegram bindings into `external_identity_claims`. Ambiguous duplicate legacy bindings fail migration and must be resolved before continuing the rollout.
- Back up the primary database and validate the migration against a production-like copy before the production rollout. SQLite, MySQL, and PostgreSQL remain supported.

## Configuration Changes

- Production and every node must use the same high-entropy `SESSION_SECRET`. Changing it invalidates existing login sessions and temporary authentication flows.
- HTTPS deployments must set `SESSION_COOKIE_SECURE=true` and list every exact public HTTPS origin in `SESSION_COOKIE_TRUSTED_URL`. Wildcards and paths are not accepted.
- Set `TRUSTED_PROXIES` to the actual reverse-proxy IPs or CIDRs. Use `none` only for direct connections with no trusted proxy.
- Multi-node deployments sharing Redis must also use the same `CRYPTO_SECRET` on every node.
- Review the defaults for `USER_SESSION_ACTIVE_LIMIT`, `USER_SESSION_ISSUANCE_LIMIT`, `USER_SESSION_ISSUANCE_WINDOW_SECONDS`, `USER_SESSION_REVOKED_RETENTION_DAYS`, and `USER_SESSION_HOURLY_ALERT_THRESHOLD` before production rollout.
- `SQL_SLOW_THRESHOLD_MS` is optional and only controls slow-query logging.

## Batch / Deployment Scripts / Data Repair

- No separate batch or deployment script is required.
- Before deployment, inspect legacy Telegram bindings for ambiguous ownership. If startup reports an external identity backfill conflict, stop the rollout and correct the conflicting legacy data through the normal audited data-change process.
- Do not delete the new authentication tables or columns during ordinary rollback.

## External Systems / Dependent Platforms

- `/root/project/ai-fund` requires no code change. After deployment, verify its PAT request and API-key log queries against the upgraded service using read-only checks.
- Existing PAT values remain valid. The legacy `New-Api-User` header is no longer required and does not participate in authentication.

## Release Order

1. Back up the primary database and confirm authentication configuration is consistent across nodes.
2. In a multi-node deployment, upgrade one master node first so it can complete GORM migration and identity backfill.
3. Confirm startup and migration logs contain no schema, identity-claim, trusted-proxy, or cookie-origin errors.
4. Upgrade the remaining nodes, keeping `SESSION_SECRET` and shared-Redis `CRYPTO_SECRET` identical.
5. Run the post-release verification before completing the rollout.

## Rollback Notes

- Roll back the application to the pre-merge image or revert merge commit `9af37f5b28dd61fc99e01465e580640b6e908ceb` through the normal confirmed Git process.
- Leave additive authentication tables and columns in place during an application rollback; the previous application ignores them. Restore the database backup only when migration or backfill changed data incorrectly and after explicit operational approval.
- A rollback or `SESSION_SECRET` change may require browser users to sign in again.

## Post-release Verification

- Verify the service starts and reports successful database migration on the master node.
- Verify browser sign-in, access-token refresh, logout, session listing, and session revocation over the production HTTPS origin.
- Verify reverse-proxy client IP handling and confirm no unexpected `TRUSTED_PROXIES` warning or startup failure.
- Run a read-only `/root/project/ai-fund` PAT request and API-key queries for `/api/log/token`, `/api/log/token/stat`, and `/api/log/token/data`.
- Verify the flattened `web/` frontend loads and that no `web/default` or Classic frontend deployment path remains.
- Monitor authentication 401/409 responses, session issuance-limit responses, database migration errors, and relay billing/error logs during rollout.
