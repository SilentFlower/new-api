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
import { formatTimestampToDate } from '@/lib/format'

import type { TokenLogChartData } from '../types'

const maxTrendBars = 48

export interface TrendDataPoint {
  created_at: number
  end_at: number
  quota: number
  token_used: number
  count: number
}

export function buildTrendDataPoints(
  quotaData: TokenLogChartData['quota_data']
): TrendDataPoint[] {
  const rows = new Map<number, TrendDataPoint>()
  quotaData.forEach((item) => {
    const createdAt = item.created_at || 0
    if (createdAt === 0) return
    const row = rows.get(createdAt) ?? {
      created_at: createdAt,
      end_at: createdAt,
      quota: 0,
      token_used: 0,
      count: 0,
    }
    row.quota += item.quota || 0
    row.token_used += item.token_used || 0
    row.count += item.count || 0
    rows.set(createdAt, row)
  })
  const points = [...rows.values()].sort((a, b) => a.created_at - b.created_at)
  if (points.length <= maxTrendBars) return points

  const chunkSize = Math.ceil(points.length / maxTrendBars)
  const compacted: TrendDataPoint[] = []
  for (let i = 0; i < points.length; i += chunkSize) {
    const chunk = points.slice(i, i + chunkSize)
    compacted.push(
      chunk.reduce<TrendDataPoint>(
        (acc, item) => ({
          created_at: acc.created_at,
          end_at: item.end_at,
          quota: acc.quota + item.quota,
          token_used: acc.token_used + item.token_used,
          count: acc.count + item.count,
        }),
        {
          created_at: chunk[0]?.created_at ?? 0,
          end_at: chunk[0]?.end_at ?? 0,
          quota: 0,
          token_used: 0,
          count: 0,
        }
      )
    )
  }
  return compacted
}

export function getTrendLabelStep(count: number): number {
  if (count <= 8) return 1
  if (count <= 16) return 2
  if (count <= 24) return 3
  return Math.ceil(count / 8)
}

export function formatTrendTimeRange(item: TrendDataPoint): string {
  const start = formatTimestampToDate(item.created_at)
  if (item.end_at === item.created_at) return start
  return `${start} - ${formatTimestampToDate(item.end_at)}`
}

export function formatCompactNumber(value: number): string {
  return Number(value || 0).toLocaleString()
}
