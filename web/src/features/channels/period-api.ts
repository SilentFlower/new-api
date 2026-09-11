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
      return 'Check rule times, overlapping limits, and quota values.'
    }
  }
  return 'Period policy is unavailable. Reload and try again.'
}

/** @param channelId 渠道 ID。 @returns 权威周期策略。 */
export async function getChannelPeriodPolicy(
  channelId: number
): Promise<ChannelPeriodView> {
  const res = await api.get<{ success: boolean; data: ChannelPeriodView }>(
    `/api/channel/${channelId}/period-policy`,
    requestConfig
  )
  if (!res.data.success) {
    throw new Error('Period policy is unavailable. Reload and try again.')
  }
  return res.data.data
}

/** @param channelId 渠道 ID。 @returns 安全降级选项。 */
export async function getChannelPeriodTargets(
  channelId: number
): Promise<ChannelPeriodTarget[]> {
  const res = await api.get<{ success: boolean; data: ChannelPeriodTarget[] }>(
    `/api/channel/${channelId}/period-policy/targets`,
    requestConfig
  )
  if (!res.data.success) {
    throw new Error('Period policy is unavailable. Reload and try again.')
  }
  return res.data.data
}

/** @param channelId 渠道 ID。 @param revision 预期版本。 @param config 配置。 @param preview 只读预览。 @returns 权威视图。 */
export async function saveChannelPeriodPolicy(
  channelId: number,
  revision: number,
  config: ChannelPeriodConfig,
  preview = false
): Promise<ChannelPeriodView> {
  const body = { expected_revision: revision, config }
  const path = `/api/channel/${channelId}/period-policy${preview ? '/preview' : ''}`
  const res = await api.request<{ success: boolean; data: ChannelPeriodView }>({
    ...requestConfig,
    method: preview ? 'POST' : 'PUT',
    url: path,
    data: body,
  })
  if (!res.data.success) {
    throw new Error('Period policy is unavailable. Reload and try again.')
  }
  return res.data.data
}

/** @param channelId 渠道 ID。 @param ruleId 规则 ID。 @param userId 用户 ID。 @param input 特批或 null 撤销。 @returns 权威用户状态。 */
export async function saveChannelPeriodOverride(
  channelId: number,
  ruleId: string,
  userId: number,
  input: { user_period_quota_limit: number; expires_at: number } | null
): Promise<ChannelUserLimitStatus> {
  const res = await api.request<{
    success: boolean
    data: ChannelUserLimitStatus
  }>({
    ...requestConfig,
    method: input === null ? 'DELETE' : 'PUT',
    url: `/api/channel/${channelId}/period-rules/${ruleId}/user-overrides/${userId}`,
    data: input ?? undefined,
  })
  if (!res.data.success) {
    throw new Error('Period policy is unavailable. Reload and try again.')
  }
  return res.data.data
}
