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
import type {
  ChannelBudgetRow,
  ChannelBudgetUsageSummaryItem,
} from '../period-types'

/** 预算行的展示状态；优先级从高到低：已停用 > 未到时段 > 已超限 > 接近上限 > 生效中。 */
export type ChannelBudgetStatus =
  | 'off'
  | 'pending'
  | 'exceeded'
  | 'near'
  | 'active'

/** 已用达到生效上限的该比例即视为接近上限。 */
export const NEAR_LIMIT_RATIO = 0.85

/**
 * 计数身份：与后端 identityKey 一致，日/周窗口不含时段，整段才按时段区分。
 * @param row 预算行。
 * @returns 身份键。
 */
export function channelBudgetIdentityKey(
  row: Pick<ChannelBudgetRow, 'window' | 'schedule_id' | 'models'>
): string {
  const schedule = row.window === 'occurrence' ? row.schedule_id : ''
  return `${row.window}|${schedule}|${[...row.models].sort().join(',')}`
}

/**
 * 找出与其他行共用计数的预算行，供表格用文字说明替代重复的进度条。
 * 个人行与池子行虽然同属一个计数身份，但读取的是不同字段，因此只在同范围内视为共用。
 * @param rows 全部预算行（含未保存行）。
 * @returns 从共用行 id 到主行 id 的映射；主行是同范围同身份中创建最早的已保存行，全为新行时取顺序第一条。
 */
export function groupChannelBudgetCounters(
  rows: ChannelBudgetRow[]
): Map<string, string> {
  const groups = new Map<string, ChannelBudgetRow[]>()
  for (const row of rows) {
    const key = `${row.scope}|${channelBudgetIdentityKey(row)}`
    groups.set(key, [...(groups.get(key) ?? []), row])
  }
  const shared = new Map<string, string>()
  for (const group of groups.values()) {
    if (group.length < 2) continue
    const primary = group.reduce((best, row) => {
      if (best.created_at === 0) return row.created_at > 0 ? row : best
      return row.created_at > 0 && row.created_at < best.created_at ? row : best
    }, group[0])
    for (const row of group) {
      if (row !== primary) shared.set(row.id, primary.id)
    }
  }
  return shared
}

/**
 * 行的生效上限：个人行取用量最高用户的生效上限（含提额），池子行取行上限。
 * @param row 预算行。
 * @param summary 该行的聚合用量。
 * @returns 生效上限；0 表示不限。
 */
export function channelBudgetEffectiveLimit(
  row: Pick<ChannelBudgetRow, 'scope' | 'limit'>,
  summary?: ChannelBudgetUsageSummaryItem
): number {
  if (row.scope === 'user' && summary?.top_user) {
    return summary.top_user.effective_limit
  }
  return row.limit
}

/**
 * 由数据推导预算行状态。
 * @param input 启用、时段是否活跃（无时段传 null）、已用与生效上限。
 * @returns 展示状态。
 */
export function deriveChannelBudgetStatus(input: {
  enabled: boolean
  scheduleActive: boolean | null
  used: number
  limit: number
}): ChannelBudgetStatus {
  if (!input.enabled) return 'off'
  if (input.scheduleActive === false) return 'pending'
  if (input.limit > 0 && input.used >= input.limit) return 'exceeded'
  if (input.limit > 0 && input.used / input.limit >= NEAR_LIMIT_RATIO) {
    return 'near'
  }
  return 'active'
}
