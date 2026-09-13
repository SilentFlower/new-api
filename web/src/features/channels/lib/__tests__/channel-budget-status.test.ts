/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'

import type { ChannelBudgetRow } from '../../period-types'
import {
  channelBudgetEffectiveLimit,
  channelBudgetIdentityKey,
  deriveChannelBudgetStatus,
  groupChannelBudgetCounters,
} from '../channel-budget-status'

const testModule = 'bun:test'
const { test } = (await import(testModule)) as {
  test: typeof import('node:test').test
}

function row(overrides: Partial<ChannelBudgetRow>): ChannelBudgetRow {
  return {
    id: 'b1',
    name: '行',
    enabled: true,
    scope: 'pool',
    window: 'daily',
    schedule_id: '',
    models: [],
    limit: 100,
    on_exceed: { mode: 'inherit', channel_id: 0, model: '' },
    created_at: 10,
    ...overrides,
  }
}

test('状态推导按 已停用 > 未到时段 > 已超限 > 接近上限 > 生效中 的优先级，0 上限不视为超限', () => {
  assert.equal(
    deriveChannelBudgetStatus({
      enabled: false,
      scheduleActive: true,
      used: 0,
      limit: 100,
    }),
    'off'
  )
  assert.equal(
    deriveChannelBudgetStatus({
      enabled: true,
      scheduleActive: false,
      used: 100,
      limit: 100,
    }),
    'pending'
  )
  assert.equal(
    deriveChannelBudgetStatus({
      enabled: true,
      scheduleActive: null,
      used: 100,
      limit: 100,
    }),
    'exceeded'
  )
  assert.equal(
    deriveChannelBudgetStatus({
      enabled: true,
      scheduleActive: true,
      used: 85,
      limit: 100,
    }),
    'near'
  )
  assert.equal(
    deriveChannelBudgetStatus({
      enabled: true,
      scheduleActive: null,
      used: 84,
      limit: 100,
    }),
    'active'
  )
  assert.equal(
    deriveChannelBudgetStatus({
      enabled: true,
      scheduleActive: null,
      used: 999,
      limit: 0,
    }),
    'active'
  )
})

test('日窗口忽略时段：带时段的日行与无时段日行共用计数并指向创建最早的已保存行', () => {
  const shared = groupChannelBudgetCounters([
    row({ id: 'weekend', schedule_id: 's1', created_at: 30 }),
    row({ id: 'plain', created_at: 20 }),
    row({ id: 'new', created_at: 0 }),
  ])
  assert.equal(shared.get('weekend'), 'plain')
  assert.equal(shared.get('new'), 'plain')
  assert.equal(shared.has('plain'), false)
})

test('整段窗口按时段区分计数，模型集合顺序不影响身份', () => {
  assert.equal(
    channelBudgetIdentityKey(
      row({ window: 'occurrence', schedule_id: 's1', models: ['b', 'a'] })
    ),
    'occurrence|s1|a,b'
  )
  const shared = groupChannelBudgetCounters([
    row({ id: 'o1', window: 'occurrence', schedule_id: 's1' }),
    row({ id: 'o2', window: 'occurrence', schedule_id: 's2' }),
    row({ id: 'm1', models: ['a', 'b'] }),
    row({ id: 'm2', models: ['b', 'a'], created_at: 5 }),
    row({ id: 'u1', scope: 'user', models: ['a', 'b'] }),
  ])
  // 个人行读取用户字段，与同身份的池子行不视为共用。
  assert.equal(shared.size, 1)
  assert.equal(shared.get('m1'), 'm2')
  assert.equal(shared.has('u1'), false)
})

test('个人行生效上限取最高用量用户的生效上限，池子行取行上限', () => {
  const summary = {
    budget_id: 'b1',
    scope: 'user' as const,
    window_start: 0,
    window_end: 0,
    tracking_since: 0,
    used_quota: 8,
    pool_used_quota: 8,
    top_user: {
      user_id: 1,
      username: 'root',
      display_name: '',
      used_quota: 8,
      effective_limit: 16,
      override: true,
    },
  }
  assert.equal(channelBudgetEffectiveLimit(row({ scope: 'user' }), summary), 16)
  assert.equal(
    channelBudgetEffectiveLimit(row({ scope: 'pool' }), summary),
    100
  )
  assert.equal(channelBudgetEffectiveLimit(row({ scope: 'user' })), 100)
})
