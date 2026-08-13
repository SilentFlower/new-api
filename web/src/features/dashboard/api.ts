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
import { api } from '@/lib/api'

import {
  buildDashboardSearchParams,
  buildDashboardTokenOptionValue,
} from './lib/filters'
import type {
  DashboardQueryParams,
  DashboardTokenNameItem,
  DashboardTokenOption,
  FlowQuotaDataItem,
  QuotaDataItem,
  UptimeGroupResult,
} from './types'

// ============================================================================
// Dashboard APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Quota & Usage Data
// ----------------------------------------------------------------------------

// Get user quota data within a time range
// Admin users get all users' data by default.
export async function getUserQuotaDates(
  params: DashboardQueryParams,
  isAdmin = false
) {
  const endpoint = isAdmin ? '/api/data' : '/api/data/self'
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    `${endpoint}?${buildDashboardSearchParams(params).toString()}`
  )
  return res.data
}

// ----------------------------------------------------------------------------
// System Monitoring
// ----------------------------------------------------------------------------

export async function getUserQuotaDataByUsers(params: DashboardQueryParams) {
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    `/api/data/users?${buildDashboardSearchParams(params).toString()}`
  )
  return res.data
}

export async function getFlowQuotaDates(
  params: {
    start_timestamp: number
    end_timestamp: number
    default_time?: string
    username?: string
  },
  isAdmin = false
) {
  const endpoint = isAdmin ? '/api/data/flow' : '/api/data/flow/self'
  const res = await api.get<{
    success: boolean
    data?: FlowQuotaDataItem[]
    message?: string
  }>(endpoint, { params })
  return res.data
}

export async function getDashboardGroups(): Promise<string[]> {
  const res = await api.get<{ success: boolean; data?: string[] }>(
    '/api/group/'
  )
  return res.data.success ? (res.data.data ?? []) : []
}

export async function getDashboardTokenOptions(
  isAdmin: boolean
): Promise<DashboardTokenOption[]> {
  if (isAdmin) {
    const res = await api.get<{
      success: boolean
      data?: DashboardTokenNameItem[]
    }>('/api/data/token-names')
    if (!res.data.success || !res.data.data) return []
    return res.data.data.map((item) => ({
      value: buildDashboardTokenOptionValue(
        item.name,
        item.username,
        item.group
      ),
      label: item.username
        ? `${item.name} (${item.username}${item.group ? ` / ${item.group}` : ''})`
        : item.name,
      group: item.group || '',
    }))
  }

  const res = await api.get<{
    success: boolean
    data?: { items?: Array<{ name: string }> }
  }>('/api/token/?p=1&size=100')
  if (!res.data.success || !res.data.data?.items) return []
  return res.data.data.items.map((token) => ({
    value: token.name,
    label: token.name,
  }))
}

export async function exportDashboardReport(
  params: DashboardQueryParams
): Promise<void> {
  const res = await api.get(
    `/api/data/export?${buildDashboardSearchParams(params).toString()}`,
    {
      responseType: 'blob',
      disableDuplicate: true,
      skipBusinessError: true,
    }
  )

  const contentType = String(res.headers['content-type'] || '')
  if (contentType.includes('application/json')) {
    const text = await (res.data as Blob).text()
    const errorData = JSON.parse(text) as { message?: string }
    throw new Error(errorData.message || 'Export failed')
  }

  const disposition = String(res.headers['content-disposition'] || '')
  let fileName = 'dashboard-report.xlsx'
  const filenameMatch = disposition.match(/filename\*?=(?:UTF-8'')?(.+)/i)
  if (filenameMatch?.[1]) {
    fileName = decodeURIComponent(filenameMatch[1])
  }

  const blob = new Blob([res.data], {
    type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  })
  const url = window.URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = fileName
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  window.URL.revokeObjectURL(url)
}

// Get uptime monitoring status for all services
export async function getUptimeStatus() {
  const res = await api.get<{ success: boolean; data: UptimeGroupResult[] }>(
    '/api/uptime/status'
  )
  return res.data
}
