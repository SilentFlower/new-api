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
import { z } from 'zod'

/** 父渠道关系的安全视图，限制值用于子渠道只读展示。 */
export const channelLimitParentSchema = z
  .object({
    channel_id: z.number().int().positive(),
    parent_channel_id: z.number().int().nonnegative(),
    revision: z.number().int().nonnegative(),
    read_only: z.boolean(),
    concurrency_limit: z.number().int().min(0).max(1000),
    parent: z
      .object({ id: z.number().int().positive(), name: z.string() })
      .optional(),
  })
  .refine(
    (value) =>
      value.read_only === value.parent_channel_id > 0 &&
      (!value.read_only || value.parent?.id === value.parent_channel_id)
  )

/** 父渠道关系与当前生效的基础并发。 */
export type ChannelLimitParent = z.infer<typeof channelLimitParentSchema>

/** 不含密钥的父渠道候选。 */
export type ChannelLimitParentOption = {
  id: number
  name: string
  status: number
}

/** 关联变更的确认快照。 */
export type ChannelLimitParentChange = {
  channelId: number
  expected_revision: number
  parent_channel_id: number
}

/** 安全候选分页。 */
export const channelLimitParentOptionsSchema = z.object({
  items: z.array(
    z.object({
      id: z.number().int().positive(),
      name: z.string(),
      status: z.number().int(),
    })
  ),
  total: z.number().int().nonnegative(),
  page: z.number().int().positive(),
  page_size: z.number().int().positive().max(100),
})

/** 父渠道安全候选的分页合同。 */
export type ChannelLimitParentOptions = z.infer<
  typeof channelLimitParentOptionsSchema
>
