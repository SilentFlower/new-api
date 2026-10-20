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
  channelLimitParentSchema,
  channelLimitParentOptionsSchema,
  type ChannelLimitParent,
  type ChannelLimitParentChange,
  type ChannelLimitParentOptions,
} from './limit-parent-types'

const requestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
  disableDuplicate: true,
}

/** @param channelId 物理渠道 ID。 @returns 关系查询键。 */
export function channelLimitParentQueryKey(channelId: number) {
  return ['channels', channelId, 'limit-parent'] as const
}

/** @param channelId 物理渠道 ID。 @returns 权威关系及生效并发。 */
export async function getChannelLimitParent(
  channelId: number
): Promise<ChannelLimitParent> {
  const response = await api.get(
    `/api/channel/${channelId}/limit-parent`,
    requestConfig
  )
  if (!response.data.success) throw new Error('parent channel unavailable')
  const value = channelLimitParentSchema.parse(response.data.data)
  if (value.channel_id !== channelId) {
    throw new Error('parent channel identity mismatch')
  }
  return value
}

/** @param channelId 当前渠道。 @param keyword 名称或 ID。 @param page 页码。 @returns 安全父渠道候选。 */
export async function getChannelLimitParentOptions(
  channelId: number,
  keyword: string,
  page: number
): Promise<ChannelLimitParentOptions> {
  const response = await api.get(
    `/api/channel/${channelId}/limit-parent/options`,
    {
      ...requestConfig,
      params: { keyword, p: page, page_size: 20 },
    }
  )
  if (!response.data.success) {
    throw new Error('parent channel options unavailable')
  }
  const value = channelLimitParentOptionsSchema.parse(response.data.data)
  if (value.page !== page || value.page_size !== 20) {
    throw new Error('parent channel page mismatch')
  }
  return value
}

/** @param input 确认时捕获的目标与旧版本。 @returns 保存后的权威关系。 */
export async function saveChannelLimitParent(
  input: ChannelLimitParentChange
): Promise<ChannelLimitParent> {
  const response = await api.put(
    `/api/channel/${input.channelId}/limit-parent`,
    {
      expected_revision: input.expected_revision,
      parent_channel_id: input.parent_channel_id,
    },
    requestConfig
  )
  if (!response.data.success) throw new Error('parent channel update failed')
  const value = channelLimitParentSchema.parse(response.data.data)
  if (
    value.channel_id !== input.channelId ||
    value.revision <= input.expected_revision
  ) {
    throw new Error('parent channel identity mismatch')
  }
  return value
}
