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

import type {
  ChannelBudgetPreviewRow,
  ChannelBudgetSchedule,
} from '../../period-types'
import { resolveChannelScheduleStates } from '../channel-schedule-state'

const testModule = 'bun:test'
const { test } = (await import(testModule)) as {
  test: typeof import('node:test').test
}

/** 2030-03-18 01:46:40 Asia/Shanghai，周一。 */
const NOW = 1900000000
const TZ = 'Asia/Shanghai'
/** 该周周一 00:00 Asia/Shanghai。 */
const MONDAY = NOW - (1 * 3600 + 46 * 60 + 40)

function schedule(
  overrides: Partial<ChannelBudgetSchedule>
): ChannelBudgetSchedule {
  return {
    id: 's1',
    name: '时段',
    enabled: true,
    kind: 'weekly',
    start_local: '',
    end_local: '',
    start_at: 0,
    end_at: 0,
    start_weekday: 0,
    end_weekday: 1,
    start_time: '00:00',
    end_time: '00:00',
    created_at: 1,
    ...overrides,
  }
}

test('指定日期时段按起止判定：未开始给出开始时间，进行中不给，结束后标记已结束', () => {
  const states = resolveChannelScheduleStates(
    [
      schedule({ kind: 'date_range', start_at: NOW + 600, end_at: NOW + 7200 }),
      schedule({
        id: 's2',
        kind: 'date_range',
        start_at: NOW - 600,
        end_at: NOW + 7200,
      }),
      schedule({
        id: 's3',
        kind: 'date_range',
        start_at: NOW - 7200,
        end_at: NOW - 600,
      }),
    ],
    [],
    NOW,
    TZ
  )
  assert.deepEqual(states.get('s1'), {
    active: false,
    nextStartAt: NOW + 600,
    ended: false,
  })
  assert.deepEqual(states.get('s2'), {
    active: true,
    nextStartAt: 0,
    ended: false,
  })
  assert.deepEqual(states.get('s3'), {
    active: false,
    nextStartAt: 0,
    ended: true,
  })
})

test('被已保存行引用的时段优先采用预览来源的出现区间，而不是前端推算', () => {
  const rows: ChannelBudgetPreviewRow[] = [
    {
      budget_id: 'b1',
      active: false,
      enforced: false,
      source: {
        kind: 'weekly',
        schedule_id: 's1',
        start_at: NOW + 3600,
        end_at: NOW + 7200,
      },
    },
  ]
  // 按前端推算周一 00:00 → 周六 00:00 应为进行中；有预览来源时以来源为准。
  const states = resolveChannelScheduleStates(
    [schedule({ start_weekday: 0, end_weekday: 5 })],
    rows,
    NOW,
    TZ
  )
  assert.deepEqual(states.get('s1'), {
    active: false,
    nextStartAt: NOW + 3600,
    ended: false,
  })
})

test('未被引用的每周时段按服务器时区推算：跨周窗口命中为进行中，否则给出本周内的下次开始', () => {
  const states = resolveChannelScheduleStates(
    [
      // 周六 22:00 → 周一 02:00，周一 01:46 在窗口内。
      schedule({
        start_weekday: 5,
        end_weekday: 0,
        start_time: '22:00',
        end_time: '02:00',
      }),
      // 周五 18:00 → 周六 23:30。
      schedule({
        id: 's2',
        start_weekday: 4,
        end_weekday: 5,
        start_time: '18:00',
        end_time: '23:30',
      }),
    ],
    [],
    NOW,
    TZ
  )
  assert.deepEqual(states.get('s1'), {
    active: true,
    nextStartAt: 0,
    ended: false,
  })
  assert.deepEqual(states.get('s2'), {
    active: false,
    nextStartAt: MONDAY + 4 * 86400 + 18 * 3600,
    ended: false,
  })
})

test('停用的时段即使处于窗口内也不算进行中，但仍保留下次开始时间', () => {
  const states = resolveChannelScheduleStates(
    [
      schedule({
        enabled: false,
        kind: 'date_range',
        start_at: NOW - 600,
        end_at: NOW + 600,
      }),
      schedule({
        id: 's2',
        enabled: false,
        kind: 'date_range',
        start_at: NOW + 600,
        end_at: NOW + 1200,
      }),
    ],
    [],
    NOW,
    TZ
  )
  assert.equal(states.get('s1')?.active, false)
  assert.equal(states.get('s2')?.nextStartAt, NOW + 600)
})

test('时区名无法识别时退回浏览器时区继续判断；未落盘的指定日期时段没有起止时为未知', () => {
  const states = resolveChannelScheduleStates(
    [schedule({}), schedule({ id: 's2', kind: 'date_range' })],
    [],
    NOW,
    'Local'
  )
  assert.notEqual(states.get('s1')?.active, null)
  assert.deepEqual(states.get('s2'), {
    active: null,
    nextStartAt: 0,
    ended: false,
  })
})
