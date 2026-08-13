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
import type { DashboardFilters } from '@/features/dashboard/types'
import { getEndOfDay, getStartOfDay, type TimeGranularity } from '@/lib/time'

/** 数据看板支持的自然周期标识。 */
export type DashboardCalendarRangeId =
  | 'today'
  | 'yesterday'
  | 'this_week'
  | 'last_week'
  | 'this_month'
  | 'last_month'

/** 数据看板自然周期选项。 */
export interface DashboardCalendarRangeOption {
  id: DashboardCalendarRangeId
  label: string
  granularity: TimeGranularity
}

/** 自然周期计算结果。 */
export interface DashboardCalendarTimeRange {
  start: Date
  end: Date
  granularity: TimeGranularity
}

/** 数据看板自然周期选项列表。 */
export const DASHBOARD_CALENDAR_RANGES: readonly DashboardCalendarRangeOption[] =
  [
    { id: 'today', label: 'Today', granularity: 'hour' },
    { id: 'yesterday', label: 'Yesterday', granularity: 'hour' },
    { id: 'this_week', label: 'This Week', granularity: 'day' },
    { id: 'last_week', label: 'Last Week', granularity: 'day' },
    { id: 'this_month', label: 'This Month', granularity: 'week' },
    { id: 'last_month', label: 'Last Month', granularity: 'week' },
  ]

/**
 * 按本地时区计算指定自然周期。
 *
 * @param id 自然周期标识
 * @param fromDate 计算基准时间，默认使用当前时间
 * @returns 周一作为一周起点的自然周期起止时间与推荐粒度
 */
export function getDashboardCalendarTimeRange(
  id: DashboardCalendarRangeId,
  fromDate: Date = new Date()
): DashboardCalendarTimeRange {
  const baseDate = getStartOfDay(fromDate)
  const option = DASHBOARD_CALENDAR_RANGES.find((item) => item.id === id)

  if (!option) {
    throw new Error(`Unsupported dashboard calendar range: ${id}`)
  }

  if (id === 'today' || id === 'yesterday') {
    const start = new Date(baseDate)
    if (id === 'yesterday') {
      start.setDate(baseDate.getDate() - 1)
    }

    return {
      start,
      end: getEndOfDay(start),
      granularity: option.granularity,
    }
  }

  if (id === 'this_month' || id === 'last_month') {
    const monthOffset = id === 'last_month' ? -1 : 0
    const start = new Date(
      baseDate.getFullYear(),
      baseDate.getMonth() + monthOffset,
      1
    )
    const end = new Date(start.getFullYear(), start.getMonth() + 1, 0)

    return {
      start: getStartOfDay(start),
      end: getEndOfDay(end),
      granularity: option.granularity,
    }
  }

  // 将 JavaScript 的周日零值转换为周一零值，避免跨周计算偏移。
  const daysSinceMonday = (baseDate.getDay() + 6) % 7
  const weekOffset = id === 'last_week' ? -7 : 0
  const start = new Date(baseDate)
  start.setDate(baseDate.getDate() - daysSinceMonday + weekOffset)
  const end = new Date(start)
  end.setDate(start.getDate() + 6)

  return {
    start: getStartOfDay(start),
    end: getEndOfDay(end),
    granularity: option.granularity,
  }
}

/**
 * 检测筛选条件是否精确匹配当前自然周期。
 *
 * @param filters 当前数据看板筛选条件
 * @param fromDate 检测基准时间，默认使用当前时间
 * @returns 匹配的自然周期标识，未匹配时返回 null
 */
export function detectDashboardCalendarTimeRange(
  filters: DashboardFilters | undefined,
  fromDate: Date = new Date()
): DashboardCalendarRangeId | null {
  const start = filters?.start_timestamp
  const end = filters?.end_timestamp
  if (!start || !end) return null

  for (const option of DASHBOARD_CALENDAR_RANGES) {
    const range = getDashboardCalendarTimeRange(option.id, fromDate)
    if (
      start.getTime() === range.start.getTime() &&
      end.getTime() === range.end.getTime()
    ) {
      return option.id
    }
  }

  return null
}
