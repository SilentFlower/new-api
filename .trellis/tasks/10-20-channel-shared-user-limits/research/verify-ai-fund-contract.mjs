import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

import {
  getNewApiChannel,
  getNewApiChannelUserConcurrency,
  getNewApiChannelUserLimitStatus,
  getNewApiChannelPeriodPolicy,
  getNewApiChannelBudgetUsage,
  getNewApiChannelBudgetUsageSummary,
  getNewApiChannelUserLimitOverrides,
  getNewApiChannelBudgetOverrides,
  updateNewApiChannelUserLimits,
  setNewApiChannelUserLimitOverride,
  setNewApiChannelBudgetOverride,
  setNewApiChannelPeriodPolicy,
} from '/root/project/ai-fund/worker/src/newapi_client.js'

// 仅重放真实 Controller 生成的安全夹具，所有网络请求由本地桩接管。
const fixture = JSON.parse(await readFile(new URL('./ai-fund-parent-contract.json', import.meta.url), 'utf8'))
const channelID = fixture.channel.id
const budgetID = fixture.policy.config.budgets[0].id
const env = {
  NEWAPI_BASE_URL: 'https://newapi.test',
  NEWAPI_ADMIN_USER_ID: '1',
  NEWAPI_ADMIN_ACCESS_TOKEN: 'local-contract-token',
}
const reads = new Map([
  [`/api/channel/${channelID}`, fixture.channel],
  [`/api/channel/${channelID}/user-limit-status/77`, fixture.status],
  [`/api/channel/${channelID}/period-policy`, fixture.policy],
  [`/api/channel/${channelID}/user-concurrency?p=1&page_size=20`, fixture.concurrency],
  [`/api/channel/${channelID}/budgets/usage-summary`, fixture.usage_summary],
  [`/api/channel/${channelID}/budgets/${budgetID}/usage?scope=user&p=1&page_size=20`, fixture.usage],
  [`/api/channel/${channelID}/user-limit-overrides?p=1&page_size=20`, fixture.overrides],
  [`/api/channel/${channelID}/budget-user-overrides?p=1&page_size=20`, fixture.budget_overrides],
])
const writes = ['concurrency_update', 'override_update', 'budget_override_update', 'policy_update'].map((key) => fixture[key])
const seen = []
const originalFetch = globalThis.fetch
globalThis.fetch = async (input, init = {}) => {
  const url = new URL(input)
  assert.equal(url.origin, 'https://newapi.test')
  const path = url.pathname + url.search
  const method = init.method ?? 'GET'
  let data
  if (method === 'GET') {
    assert.equal(reads.has(path), true, `未预期的读取路径 ${path}`)
    data = reads.get(path)
  } else {
    assert.equal(method, 'PUT')
    const expected = writes.find((item) => item.path === path)
    assert.ok(expected, `未预期的写入路径 ${path}`)
    assert.deepEqual(JSON.parse(init.body), expected.body)
    assert.equal(init.redirect, 'manual')
    data = expected.response
  }
  seen.push(`${method} ${path}`)
  return new Response(JSON.stringify({ success: true, data }), { headers: { 'Content-Type': 'application/json' } })
}

try {
  const channel = await getNewApiChannel(env, channelID)
  assert.equal(channel.user_concurrency_limit, 4)
  const status = await getNewApiChannelUserLimitStatus(env, channelID, 77)
  assert.equal(status.channel_id, channelID)
  assert.equal(status.concurrency.base_limit, 4)
  assert.ok(status.period_limits.metrics[0].used > 0)
  const policy = await getNewApiChannelPeriodPolicy(env, channelID)
  assert.equal(policy.revision, fixture.policy.revision)
  const concurrency = await getNewApiChannelUserConcurrency(env, channelID)
  assert.equal(concurrency.limit, 4)
  const summary = await getNewApiChannelBudgetUsageSummary(env, channelID)
  assert.equal(summary.items[0].used_quota, status.period_limits.metrics[0].used)
  const usage = await getNewApiChannelBudgetUsage(env, channelID, budgetID)
  assert.equal(usage.items[0].used_quota, summary.items[0].used_quota)
  await getNewApiChannelUserLimitOverrides(env, channelID)
  await getNewApiChannelBudgetOverrides(env, channelID)
  assert.equal((await updateNewApiChannelUserLimits(env, channelID, 5)).channel_id, channelID)
  const override = await setNewApiChannelUserLimitOverride(env, channelID, 77, 7, 0)
  assert.equal(override.concurrency.effective_limit, 7)
  const budgetOverride = await setNewApiChannelBudgetOverride(env, channelID, budgetID, 77, { limit: 2000000, expires_at: 0 })
  assert.equal(budgetOverride.period_limits.metrics[0].limit, 2000000)
  const saved = await setNewApiChannelPeriodPolicy(env, channelID, { expected_revision: policy.revision, config: policy.config })
  assert.equal(saved.revision, policy.revision + 1)
  assert.equal(seen.length, 12)
  console.log(JSON.stringify({ passed: true, checks: seen, sourceChanged: false }, null, 2))
} finally {
  globalThis.fetch = originalFetch
}
