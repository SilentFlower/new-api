# Release Operations

## Conclusion

Needs human review. Release operations exist for database migration, runtime configuration, review-channel availability, and production MySQL tuning.

## Evidence Checked

- task.json: `message-audit-ai-review`, no child tasks.
- prd.md / design.md / implement.md / implement.jsonl / check.jsonl.
- Existing release.md: missing before this audit.
- Git evidence:
  - `b74db1362 feat(audit): 增加入站消息持久化审计 [build]`
  - `10127a30e feat(audit): 增强会话追溯与存储观测 [build]`
  - `2c76f9235 feat(audit): 完善压缩会话审计与模型对齐 [build]`
  - `50cc07632 feat(audit): 增加消息审计 AI 辅助审核 [build]`
  - `638bb20af perf(audit): 降低消息审计数据库压力 [build]`
  - `fb672fdac fix(audit): 完善消息审计清理与 AI 重审 [build]`
  - `2cc1e2d2a feat(audit): 支持图片生成与编辑请求审计 [build]`
  - `df3028009 feat(audit): 完善 AI 审核工具与调用诊断 [build]`
  - `8e394df5c fix(audit): 完善 AI 审核上下文边界与语义角色 [build]`
  - `bc287db4d fix(audit): 优化 AI 审核详情与工具调用 [build]`
  - `caf44f862 fix(audit): 完善 AI 审核渠道日志与并行工具 [build]`
  - `b9da2d366 fix(audit): 稳定 AI 审核上下文与输出 [build]`

## Drift Check

Missing release.md. Task artifacts and commits show release-sensitive changes. Production-only database parameter changes and external review-channel behavior cannot be fully verified from repository evidence, so this audit keeps `Needs human review`.

## SQL Changes

- Application migration / AutoMigrate impact exists for message audit and system task models:
  - `message_audit_requests` adds nullable storage accounting fields such as captured plaintext bytes and stored payload bytes.
  - `message_audit_states` stores cleanup / purge watermark state.
  - `message_audit_reviews` stores the current encrypted AI review result and review metadata.
  - `message_audit_review_sources` stores fixed source request references for each successful review.
  - `system_tasks` adds scoped `active_key` behavior for per-session review task idempotency.
- No standalone SQL migration file was added. Before production rollout, back up the database and verify the application migration path on the target dialect, especially MySQL.
- Manual clear now uses a fast clear path for audit tables. Treat it as an admin destructive operation and verify retention / backup policy before use.

## Configuration Changes

- `MESSAGE_AUDIT_SECRET` must be configured consistently on every application node before enabling message audit or reading encrypted audit/review data. It must satisfy the service minimum length check.
- `MessageAuditEnabled` controls message audit capture.
- `MessageAuditRetentionDays` controls retention cleanup.
- `message_audit_review.config` is the fixed AI review configuration:
  - `channel_id`: enabled review channel ID.
  - `model`: model available on that channel.
  - `context_mode`: `merged` by default, or `tool`.
  - `tool_call_limit`: positive integer, default 24, no fixed hard maximum.
- The review channel and model must be available after deploy. Do not rely on automatic fallback to another channel.
- Production MySQL tuning needs human review:
  - Set application SQL pool within MySQL capacity, e.g. `SQL_MAX_OPEN_CONNS=80`, `SQL_MAX_IDLE_CONNS=20`, `SQL_MAX_LIFETIME=300` when suitable for the target host.
  - Increase InnoDB buffer pool to match observed data size, e.g. 2 GiB on the inspected host if memory budget allows.
  - Enable slow query logging at around 1 second for post-release observation.
  - Keep transaction / binlog durability settings unchanged unless separately approved.

## Batch / Deployment Scripts / Data Repair

- No one-off data repair script is required by the repository changes.
- Deployment must allow the application to run migrations before Root users rely on new AI review screens.
- Existing historical audit rows may have null values for newly added storage fields and may not contain recoverable plaintext when they were previously stored as metadata-only.
- Existing successful review result history is not preserved as a list; the model keeps only the latest successful result per inferred session.

## External Systems / Dependent Platforms

- AI review calls use the configured gateway channel internally with zero-quota diagnostics. The selected upstream provider must accept the chosen request mode and return valid structured JSON.
- API/channel logs may contain safe zero-quota review diagnostics but must not contain audit正文、Tool 参数、Tool 结果、模型输出或上游原始错误正文.
- MySQL is an external operational dependency for this rollout because audit capture, cleanup, status statistics, and fast clear behavior were optimized around observed production pressure.

## Release Order

1. Back up the production database.
2. Deploy the code and restart nodes so application migrations can run.
3. Confirm `MESSAGE_AUDIT_SECRET`, `MessageAuditEnabled`, and `MessageAuditRetentionDays`.
4. Configure `message_audit_review.config` from Root system settings: select channel, model, context mode, and Tool call limit.
5. Apply reviewed MySQL / application SQL pool tuning if approved.
6. Smoke test as Root:
   - message audit list / detail;
   - failed request safe reason display;
   - manual refresh behavior;
   - AI review in default merged mode;
   - optional Tool mode with diagnostics;
   - image generation / edit request capture;
   - manual clear performance on a controlled dataset.

## Rollback Notes

- Roll back application code if runtime behavior is unsafe.
- Prefer leaving added audit/review tables and columns in place during code rollback, or restore from the pre-release backup if a schema rollback is mandatory.
- Disable `MessageAuditEnabled` and clear `message_audit_review.config` if review capture or AI review must be stopped without code rollback.
- Revert MySQL / SQL pool tuning using the pre-change configuration values if production metrics regress.

## Post-release Verification

- Verify Root-only access for audit detail, review trigger, review options, and review result APIs.
- Verify AI review calls do not create recursive message audit rows and do not charge user quota.
- Verify review failure diagnostics show stable stage / HTTP status without upstream raw body.
- Verify list and detail show the same finalized model after manual refresh.
- Verify `content_reduced` / `metadata_only` storage sizes are distinguishable in the UI.
- Verify retention cleanup and manual clear remove review sources / review results with the corresponding audit records.
- Verify MySQL CPU, buffer pool hit rate, active connections, slow queries, and table growth after traffic resumes.
