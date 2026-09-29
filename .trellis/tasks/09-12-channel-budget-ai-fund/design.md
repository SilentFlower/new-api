# Design — ai-fund 迁移到预算 v2

以父任务 `design.md` §9 为准。文件落点：`worker/src/newapi_period_policy.js`、`pool_period_limits.js`、`pool_limits.js`、`newapi_client.js`、`index.js`、`fixtures/channel-period-contract.json`；前端 `components/PoolPeriodPolicyEditor.vue`、`PoolLimitAdminModal.vue`、`PoolLimitPanel.vue`、新增 `PoolBudgetRowEditor.vue`、`PoolBudgetUsageEditor.vue`、`api/index.js`。金额换算沿用 `displayQuotaToInternalNonNegative` 与 `MAX_PERIOD_QUOTA`。
