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
  ChannelBudgetAction,
  ChannelBudgetRow,
  ChannelBudgetSchedule,
  ChannelPeriodConfig,
} from '../../../period-types'

/** 一条待保存的改动：预算或时段名、字段名、原值与新值，全部为可直接展示的文本。 */
export interface ChannelPolicyChange {
  key: string
  subject: string
  field: string
  from: string
  to: string
}

/** 把字段值转成展示文本的翻译器；由调用方用 t() 提供，保持本模块无 React 依赖。 */
export interface ChannelPolicyChangeFormat {
  scope: (value: ChannelBudgetRow['scope']) => string
  window: (value: ChannelBudgetRow['window']) => string
  schedule: (id: string) => string
  action: (value: ChannelBudgetAction) => string
  amount: (value: number) => string
  models: (value: string[]) => string
  enabled: (value: boolean) => string
  kind: (value: ChannelBudgetSchedule['kind']) => string
  field: (name: string) => string
  added: string
  removed: string
  policy: string
  untitled: string
}

function scheduleRange(schedule: ChannelBudgetSchedule): string {
  if (schedule.kind === 'date_range') {
    return `${schedule.start_local} → ${schedule.end_local}`
  }
  return `${schedule.start_weekday} ${schedule.start_time} → ${schedule.end_weekday} ${schedule.end_time}`
}

/**
 * 对比权威配置与当前草稿，产出逐字段改动清单，供保存栏计数与预览 diff。
 * @param before 权威配置。
 * @param after 当前草稿。
 * @param format 字段值翻译器。
 * @returns 改动清单；顺序为策略级、时段、预算。
 */
export function diffChannelPeriodConfig(
  before: ChannelPeriodConfig,
  after: ChannelPeriodConfig,
  format: ChannelPolicyChangeFormat
): ChannelPolicyChange[] {
  const changes: ChannelPolicyChange[] = []
  const push = (
    key: string,
    subject: string,
    field: string,
    from: string,
    to: string
  ) => {
    if (from !== to) changes.push({ key, subject, field, from, to })
  }
  push(
    'default_on_exceed',
    format.policy,
    format.field('default_on_exceed'),
    format.action(before.default_on_exceed),
    format.action(after.default_on_exceed)
  )
  push(
    'model_usage_tracking_enabled',
    format.policy,
    format.field('model_usage_tracking_enabled'),
    format.enabled(before.model_usage_tracking_enabled ?? false),
    format.enabled(after.model_usage_tracking_enabled ?? false)
  )
  const beforeSchedules = new Map(
    before.schedules.map((item) => [item.id, item])
  )
  for (const schedule of after.schedules) {
    const subject = schedule.name || format.untitled
    const previous = beforeSchedules.get(schedule.id)
    if (!previous) {
      changes.push({
        key: `schedule:${schedule.id}`,
        subject,
        field: format.field('schedule'),
        from: '',
        to: format.added,
      })
      continue
    }
    push(
      `schedule:${schedule.id}:name`,
      subject,
      format.field('name'),
      previous.name,
      schedule.name
    )
    push(
      `schedule:${schedule.id}:enabled`,
      subject,
      format.field('enabled'),
      format.enabled(previous.enabled),
      format.enabled(schedule.enabled)
    )
    push(
      `schedule:${schedule.id}:kind`,
      subject,
      format.field('kind'),
      format.kind(previous.kind),
      format.kind(schedule.kind)
    )
    push(
      `schedule:${schedule.id}:range`,
      subject,
      format.field('range'),
      scheduleRange(previous),
      scheduleRange(schedule)
    )
  }
  const afterScheduleIds = new Set(after.schedules.map((item) => item.id))
  for (const schedule of before.schedules) {
    if (!afterScheduleIds.has(schedule.id)) {
      changes.push({
        key: `schedule:${schedule.id}`,
        subject: schedule.name || format.untitled,
        field: format.field('schedule'),
        from: '',
        to: format.removed,
      })
    }
  }
  const beforeBudgets = new Map(before.budgets.map((item) => [item.id, item]))
  for (const row of after.budgets) {
    const subject = row.name || format.untitled
    const previous = beforeBudgets.get(row.id)
    if (!previous) {
      changes.push({
        key: `budget:${row.id}`,
        subject,
        field: format.field('budget'),
        from: '',
        to: format.added,
      })
      continue
    }
    push(
      `budget:${row.id}:name`,
      subject,
      format.field('name'),
      previous.name,
      row.name
    )
    push(
      `budget:${row.id}:enabled`,
      subject,
      format.field('enabled'),
      format.enabled(previous.enabled),
      format.enabled(row.enabled)
    )
    push(
      `budget:${row.id}:scope`,
      subject,
      format.field('scope'),
      format.scope(previous.scope),
      format.scope(row.scope)
    )
    push(
      `budget:${row.id}:window`,
      subject,
      format.field('window'),
      format.window(previous.window),
      format.window(row.window)
    )
    push(
      `budget:${row.id}:schedule_id`,
      subject,
      format.field('schedule_id'),
      format.schedule(previous.schedule_id),
      format.schedule(row.schedule_id)
    )
    push(
      `budget:${row.id}:models`,
      subject,
      format.field('models'),
      format.models(previous.models),
      format.models(row.models)
    )
    push(
      `budget:${row.id}:limit`,
      subject,
      format.field('limit'),
      format.amount(previous.limit),
      format.amount(row.limit)
    )
    push(
      `budget:${row.id}:on_exceed`,
      subject,
      format.field('on_exceed'),
      format.action(previous.on_exceed),
      format.action(row.on_exceed)
    )
  }
  const afterBudgetIds = new Set(after.budgets.map((item) => item.id))
  for (const row of before.budgets) {
    if (!afterBudgetIds.has(row.id)) {
      changes.push({
        key: `budget:${row.id}`,
        subject: row.name || format.untitled,
        field: format.field('budget'),
        from: '',
        to: format.removed,
      })
    }
  }
  return changes
}
