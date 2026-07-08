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
import {
  LOG_TYPE_ALL_VALUE,
  LOG_TYPE_ENUM,
} from '@/features/usage-logs/constants'

import type { TokenLogFilters, TokenLogQueryParams } from './types'

/**
 * 生成公共日志查看器的默认时间范围：当天零点到当前时间后一小时。
 */
export function getDefaultTokenLogTimeRange(): { start: Date; end: Date } {
  const now = new Date()
  const start = new Date(now)
  start.setHours(0, 0, 0, 0)
  const end = new Date(now.getTime() + 3600 * 1000)
  return { start, end }
}

/**
 * 日期对象转秒级时间戳。
 */
export function dateToTimestampSeconds(date: Date | undefined): number {
  return date ? Math.floor(date.getTime() / 1000) : 0
}

/**
 * 构造公共日志过滤器默认值。
 */
export function buildDefaultTokenLogFilters(): TokenLogFilters {
  const { start, end } = getDefaultTokenLogTimeRange()
  return {
    startTime: start,
    endTime: end,
    type: LOG_TYPE_ALL_VALUE,
  }
}

/**
 * 将本地过滤状态转换为后端查询参数。
 */
export function buildTokenLogQueryParams(
  filters: TokenLogFilters,
  page: number,
  pageSize: number
): TokenLogQueryParams {
  return {
    ...buildTokenLogFilterParams(filters),
    p: page,
    page_size: pageSize,
  }
}

/**
 * 将本地过滤状态转换为统计和图表共用的后端查询参数。
 */
export function buildTokenLogFilterParams(
  filters: TokenLogFilters
): Omit<TokenLogQueryParams, 'p' | 'page_size'> {
  const logType = Number(filters.type)
  return {
    type: Number.isFinite(logType) ? logType : 0,
    model_name: filters.model?.trim() || undefined,
    request_id: filters.requestId?.trim() || undefined,
    start_timestamp: dateToTimestampSeconds(filters.startTime),
    end_timestamp: dateToTimestampSeconds(filters.endTime),
  }
}

/**
 * 判断当前日志类型筛选是否支持用量类统计。
 */
export function isTokenLogUsageStatAvailable(
  filters: TokenLogFilters
): boolean {
  const logType = Number(filters.type)
  return (
    !Number.isFinite(logType) ||
    logType === Number(LOG_TYPE_ALL_VALUE) ||
    logType === LOG_TYPE_ENUM.CONSUME
  )
}

/**
 * 判断过滤器是否包含非默认筛选项。
 */
export function hasTokenLogFilters(filters: TokenLogFilters): boolean {
  return Boolean(
    filters.model?.trim() ||
    filters.token?.trim() ||
    filters.group?.trim() ||
    filters.requestId?.trim() ||
    filters.type !== LOG_TYPE_ALL_VALUE
  )
}
