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

import type { QuotaDataItem } from '@/features/dashboard/types'

import { processUserChartData } from './charts'

function createUserData(userCount: number): QuotaDataItem[] {
  return Array.from({ length: userCount }, (_, index) => ({
    username: `user-${String(index + 1).padStart(2, '0')}`,
    created_at: 1_720_000_000 + index * 3600,
    quota: userCount - index,
  }))
}

function chartValues(
  spec: Record<string, unknown>
): Array<Record<string, unknown>> {
  const data = spec.data as Array<{
    values: Array<Record<string, unknown>>
  }>
  return data[0].values
}

describe('processUserChartData', () => {
  test('keeps all fifty selected ranking users in chart data', () => {
    const result = processUserChartData(
      createUserData(60),
      'day',
      undefined,
      50
    )
    const rankValues = chartValues(result.spec_user_rank)

    assert.equal(result.rankUserCount, 50)
    assert.equal(rankValues.length, 50)
  })

  test('returns the actual active user count without padding', () => {
    const result = processUserChartData(
      createUserData(13),
      'day',
      undefined,
      20
    )
    const rankValues = chartValues(result.spec_user_rank)

    assert.equal(result.rankUserCount, 13)
    assert.equal(rankValues.length, 13)
  })

  test('uses the same top users for ranking and trend data', () => {
    const result = processUserChartData(
      createUserData(25),
      'day',
      undefined,
      20
    )
    const rankUsers = new Set(
      chartValues(result.spec_user_rank).map((item) => item.User)
    )
    const trendUsers = new Set(
      chartValues(result.spec_user_trend).map((item) => item.User)
    )

    assert.deepEqual(trendUsers, rankUsers)
  })
})
