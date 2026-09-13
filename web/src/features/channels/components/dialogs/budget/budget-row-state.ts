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
  channelBudgetEffectiveLimit,
  deriveChannelBudgetStatus,
  groupChannelBudgetCounters,
  type ChannelBudgetStatus,
} from '../../../lib/channel-budget-status'
import type { ChannelScheduleState } from '../../../lib/channel-schedule-state'
import type {
  ChannelBudgetRow,
  ChannelBudgetUsageSummaryItem,
  ChannelPeriodConfig,
} from '../../../period-types'

/** 新时段与新预算行的临时 id 前缀；服务端保存时替换为稳定 id。 */
export const NEW_ID_PREFIX = 'new-'

/** @param id 时段或预算行 id。 @returns 是否为尚未保存的临时身份。 */
export function isNewId(id: string): boolean {
  return id === '' || id.startsWith(NEW_ID_PREFIX)
}

/** 预算表一行的派生展示状态。 */
export interface BudgetRowState {
  status: ChannelBudgetStatus
  isNew: boolean
  changed: boolean
  scheduleActive: boolean | null
  summary?: ChannelBudgetUsageSummaryItem
  sharedWith?: ChannelBudgetRow
  effectiveLimit: number
}

/** 预算表筛选项。 */
export type BudgetFilter = 'all' | 'active' | 'attention' | 'off'

/** @param status 行状态。 @param filter 筛选项。 @returns 该行是否满足筛选。 */
export function matchesBudgetFilter(
  status: ChannelBudgetStatus,
  filter: BudgetFilter
): boolean {
  if (filter === 'all') return true
  if (filter === 'active') return status === 'active' || status === 'pending'
  if (filter === 'attention') return status === 'near' || status === 'exceeded'
  return status === 'off'
}

/**
 * 为当前草稿的每一行计算状态、变更标记、聚合用量与共用计数。
 * @param draft 当前草稿。
 * @param original 权威配置，用于标记已修改行。
 * @param summaries 聚合用量（按行 id）。
 * @param scheduleStates 各时段是否进行中（按时段 id），用于判断行是否未到时段。
 * @returns 按行 id 索引的状态。
 */
export function computeBudgetRowStates(
  draft: ChannelPeriodConfig,
  original: ChannelPeriodConfig,
  summaries: Map<string, ChannelBudgetUsageSummaryItem>,
  scheduleStates: Map<string, ChannelScheduleState>
): Map<string, BudgetRowState> {
  const originalRows = new Map(original.budgets.map((row) => [row.id, row]))
  const shared = groupChannelBudgetCounters(draft.budgets)
  const rowsById = new Map(draft.budgets.map((row) => [row.id, row]))
  const states = new Map<string, BudgetRowState>()
  for (const row of draft.budgets) {
    const isNew = isNewId(row.id)
    const previous = originalRows.get(row.id)
    const summary = summaries.get(row.id)
    const effectiveLimit = channelBudgetEffectiveLimit(row, summary)
    const scheduleActive = row.schedule_id
      ? (scheduleStates.get(row.schedule_id)?.active ?? null)
      : null
    const sharedWithId = shared.get(row.id)
    states.set(row.id, {
      status: deriveChannelBudgetStatus({
        enabled: row.enabled,
        scheduleActive,
        used: summary?.used_quota ?? 0,
        limit: effectiveLimit,
      }),
      isNew,
      changed:
        !isNew &&
        !!previous &&
        JSON.stringify(previous) !== JSON.stringify(row),
      scheduleActive,
      summary,
      sharedWith: sharedWithId ? rowsById.get(sharedWithId) : undefined,
      effectiveLimit,
    })
  }
  return states
}
