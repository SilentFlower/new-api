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
import type { UsageLog } from '@/features/usage-logs/data/schema'

/**
 * 公共 API Key 日志查询参数。
 */
export interface TokenLogQueryParams {
  p?: number
  page_size?: number
  type?: number
  model_name?: string
  request_id?: string
  start_timestamp?: number
  end_timestamp?: number
}

/**
 * 公共 API Key 日志统计。
 */
export interface TokenLogStat {
  count: number
  quota: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  rpm: number
  tpm: number
}

/**
 * 公共 API Key 模型调用分布项。
 */
export interface TokenModelStat {
  model_name: string
  count: number
}

/**
 * 公共 API Key 按时间聚合的额度数据。
 */
export interface TokenQuotaDataItem {
  model_name?: string
  created_at: number
  quota?: number
  token_used?: number
  count?: number
}

/**
 * 公共 API Key 图表响应数据。
 */
export interface TokenLogChartData {
  model_stats: TokenModelStat[]
  quota_data: TokenQuotaDataItem[]
}

/**
 * 管理 API 通用业务响应。
 */
export interface TokenLogApiResponse<T> {
  success: boolean
  message?: string
  data?: T
}

/**
 * 分页日志数据。
 */
export interface TokenLogPageData {
  items?: UsageLog[]
  total: number
  page: number
  page_size: number
}

/**
 * 公共日志过滤状态。
 */
export interface TokenLogFilters {
  startTime?: Date
  endTime?: Date
  model?: string
  token?: string
  group?: string
  requestId?: string
  type: string
}
