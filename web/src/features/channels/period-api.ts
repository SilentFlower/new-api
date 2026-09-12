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
import { isAxiosError } from 'axios'

import { api } from '@/lib/api'

import type {
  ChannelBudgetPreview,
  ChannelBudgetUsageView,
  ChannelBudgetUserOverrideItem,
  ChannelPeriodConfig,
  ChannelPeriodTarget,
  ChannelPeriodView,
} from './period-types'
import type { ChannelUserLimitStatus } from './types'

const requestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
  disableDuplicate: true,
}
const unavailable = 'Period policy is unavailable. Reload and try again.'

/**
 * 将稳定状态转换为可翻译错误，不依赖后端的原始中文错误。
 * @param error 请求错误。
 * @returns 错误翻译键。
 */
export function channelPeriodErrorKey(error: unknown): string {
  if (isAxiosError(error)) {
    if (error.response?.data?.committed === true) {
      return 'Saved, but status refresh failed. Reload before editing again.'
    }
    if (error.response?.status === 409) {
      return 'The policy has changed. Reload before editing again.'
    }
    if (error.response?.status === 400) {
      return 'Check schedules, overlapping budgets, and quota values.'
    }
  }
  return unavailable
}

async function unwrap<T>(
  promise: Promise<{ data: { success: boolean; data: T } }>
): Promise<T> {
  const res = await promise
  if (!res.data.success) throw new Error(unavailable)
  return res.data.data
}

/** @param channelId 渠道 ID。 @returns 权威预算策略。 */
export function getChannelPeriodPolicy(
  channelId: number
): Promise<ChannelPeriodView> {
  return unwrap(
    api.get(`/api/channel/${channelId}/period-policy`, requestConfig)
  )
}

/** @param channelId 渠道 ID。 @returns 含本渠道的降级候选。 */
export function getChannelPeriodTargets(
  channelId: number
): Promise<ChannelPeriodTarget[]> {
  return unwrap(
    api.get(`/api/channel/${channelId}/period-policy/targets`, requestConfig)
  )
}

/** @param channelId 渠道 ID。 @param revision 预期版本。 @param config 配置。 @returns 只读预览。 */
export function previewChannelPeriodPolicy(
  channelId: number,
  revision: number,
  config: ChannelPeriodConfig
): Promise<ChannelBudgetPreview> {
  return unwrap(
    api.request({
      ...requestConfig,
      method: 'POST',
      url: `/api/channel/${channelId}/period-policy/preview`,
      data: { expected_revision: revision, config },
    })
  )
}

/** @param channelId 渠道 ID。 @param revision 预期版本。 @param config 配置。 @returns 权威视图。 */
export function saveChannelPeriodPolicy(
  channelId: number,
  revision: number,
  config: ChannelPeriodConfig
): Promise<ChannelPeriodView> {
  return unwrap(
    api.request({
      ...requestConfig,
      method: 'PUT',
      url: `/api/channel/${channelId}/period-policy`,
      data: { expected_revision: revision, config },
    })
  )
}

/** @param channelId 渠道 ID。 @param budgetId 预算行。 @param params 范围与分页。 @returns 当前窗口用量。 */
export function getChannelBudgetUsage(
  channelId: number,
  budgetId: string,
  params: { scope: 'user' | 'pool'; p: number; page_size: number }
): Promise<ChannelBudgetUsageView> {
  return unwrap(
    api.get(`/api/channel/${channelId}/budgets/${budgetId}/usage`, {
      ...requestConfig,
      params,
    })
  )
}

/** @param channelId 渠道 ID。 @param budgetId 预算行。 @param input 范围、用户与目标已用额度。 @returns 调整结果。 */
export function setChannelBudgetUsage(
  channelId: number,
  budgetId: string,
  input: { scope: 'user' | 'pool'; user_id: number; used_quota: number }
): Promise<{ used_quota: number }> {
  return unwrap(
    api.request({
      ...requestConfig,
      method: 'PUT',
      url: `/api/channel/${channelId}/budgets/${budgetId}/usage`,
      data: input,
    })
  )
}

/** @param channelId 渠道 ID。 @param budgetId 预算行。 @param userId 用户 ID。 @param input 提额或 null 撤销。 @returns 权威用户状态。 */
export function saveChannelBudgetOverride(
  channelId: number,
  budgetId: string,
  userId: number,
  input: { limit: number; expires_at: number } | null
): Promise<ChannelUserLimitStatus> {
  return unwrap(
    api.request({
      ...requestConfig,
      method: input === null ? 'DELETE' : 'PUT',
      url: `/api/channel/${channelId}/budgets/${budgetId}/user-overrides/${userId}`,
      data: input ?? undefined,
    })
  )
}

/** @param channelId 渠道 ID。 @param params 分页。 @returns 当前有效的行级提额。 */
export function getChannelBudgetUserOverrides(
  channelId: number,
  params: { p: number; page_size: number }
): Promise<{
  total: number
  page: number
  page_size: number
  items: ChannelBudgetUserOverrideItem[]
}> {
  return unwrap(
    api.get(`/api/channel/${channelId}/budget-user-overrides`, {
      ...requestConfig,
      params,
    })
  )
}
