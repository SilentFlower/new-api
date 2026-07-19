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
import { describe, test } from 'node:test'

import { getRollingDateRange } from '@/lib/time'

import {
  detectDashboardCalendarTimeRange,
  getDashboardCalendarTimeRange,
} from './calendar-time-ranges'

function assertLocalDate(
  actual: Date,
  expected: [number, number, number, number, number, number, number]
) {
  assert.deepEqual(
    [
      actual.getFullYear(),
      actual.getMonth(),
      actual.getDate(),
      actual.getHours(),
      actual.getMinutes(),
      actual.getSeconds(),
      actual.getMilliseconds(),
    ],
    expected
  )
}

describe('dashboard calendar time ranges', () => {
  const sunday = new Date(2026, 6, 19, 14, 30, 20, 123)

  test('uses local day boundaries for today', () => {
    const range = getDashboardCalendarTimeRange('today', sunday)

    assertLocalDate(range.start, [2026, 6, 19, 0, 0, 0, 0])
    assertLocalDate(range.end, [2026, 6, 19, 23, 59, 59, 999])
    assert.equal(range.granularity, 'hour')
  })

  test('uses Monday through Sunday for week ranges', () => {
    const current = getDashboardCalendarTimeRange('this_week', sunday)
    const previous = getDashboardCalendarTimeRange('last_week', sunday)

    assertLocalDate(current.start, [2026, 6, 13, 0, 0, 0, 0])
    assertLocalDate(current.end, [2026, 6, 19, 23, 59, 59, 999])
    assertLocalDate(previous.start, [2026, 6, 6, 0, 0, 0, 0])
    assertLocalDate(previous.end, [2026, 6, 12, 23, 59, 59, 999])
    assert.equal(current.granularity, 'day')
    assert.equal(previous.granularity, 'day')
  })

  test('handles month and cross-year boundaries', () => {
    const leapMonth = getDashboardCalendarTimeRange(
      'this_month',
      new Date(2024, 1, 10, 8)
    )
    const previousMonth = getDashboardCalendarTimeRange(
      'last_month',
      new Date(2026, 0, 10, 8)
    )

    assertLocalDate(leapMonth.start, [2024, 1, 1, 0, 0, 0, 0])
    assertLocalDate(leapMonth.end, [2024, 1, 29, 23, 59, 59, 999])
    assertLocalDate(previousMonth.start, [2025, 11, 1, 0, 0, 0, 0])
    assertLocalDate(previousMonth.end, [2025, 11, 31, 23, 59, 59, 999])
    assert.equal(leapMonth.granularity, 'week')
    assert.equal(previousMonth.granularity, 'week')
  })

  test('detects calendar ranges before rolling ranges', () => {
    const today = getDashboardCalendarTimeRange('today', sunday)
    const rolling = getRollingDateRange(1, sunday)

    assert.equal(
      detectDashboardCalendarTimeRange(
        {
          start_timestamp: today.start,
          end_timestamp: today.end,
        },
        sunday
      ),
      'today'
    )
    assert.equal(
      detectDashboardCalendarTimeRange(
        {
          start_timestamp: rolling.start,
          end_timestamp: rolling.end,
        },
        sunday
      ),
      null
    )
  })
})
