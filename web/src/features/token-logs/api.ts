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
import axios, { type AxiosInstance } from 'axios'

import type {
  TokenLogApiResponse,
  TokenLogChartData,
  TokenLogPageData,
  TokenLogQueryParams,
  TokenLogStat,
  TokenUsageResponse,
} from './types'

/**
 * 创建仅用于公共 API Key 日志查看器的客户端，避免触发全局登录态拦截器。
 */
export function createTokenLogClient(apiKey: string): AxiosInstance {
  return axios.create({
    baseURL: '',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      'Cache-Control': 'no-store',
    },
  })
}

/**
 * 将日志查询参数序列化为 URLSearchParams。
 */
export function buildTokenLogSearchParams(
  params: TokenLogQueryParams
): URLSearchParams {
  const searchParams = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    searchParams.set(key, String(value))
  })
  return searchParams
}

/**
 * 查询当前 API Key 的统计数据。
 */
export async function getTokenLogStat(
  client: AxiosInstance,
  params: Omit<TokenLogQueryParams, 'p' | 'page_size'> = {}
): Promise<TokenLogApiResponse<TokenLogStat>> {
  const searchParams = buildTokenLogSearchParams(params)
  const suffix = searchParams.size > 0 ? `?${searchParams.toString()}` : ''
  const res = await client.get<TokenLogApiResponse<TokenLogStat>>(
    `/api/log/token/stat${suffix}`
  )
  return res.data
}

/**
 * 查询当前 API Key 的图表数据。
 */
export async function getTokenLogChartData(
  client: AxiosInstance,
  params: Omit<TokenLogQueryParams, 'p' | 'page_size'>
): Promise<TokenLogApiResponse<TokenLogChartData>> {
  const searchParams = buildTokenLogSearchParams(params)
  const res = await client.get<TokenLogApiResponse<TokenLogChartData>>(
    `/api/log/token/data?${searchParams.toString()}`
  )
  return res.data
}

/**
 * 轻量查询当前 API Key 的基础使用信息，用于认证验证。
 */
export async function getTokenUsage(
  client: AxiosInstance
): Promise<TokenUsageResponse> {
  const res = await client.get<TokenUsageResponse>('/api/usage/token/')
  return res.data
}

/**
 * 查询当前 API Key 的分页日志。
 */
export async function getTokenLogs(
  client: AxiosInstance,
  params: TokenLogQueryParams
): Promise<TokenLogApiResponse<TokenLogPageData>> {
  const searchParams = buildTokenLogSearchParams(params)
  const res = await client.get<TokenLogApiResponse<TokenLogPageData>>(
    `/api/log/token?${searchParams.toString()}`
  )
  return res.data
}
