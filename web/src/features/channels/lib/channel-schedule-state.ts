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
  ChannelBudgetPreviewRow,
  ChannelBudgetSchedule,
} from '../period-types'

/** 时段在某一时刻的解析状态。 */
export interface ChannelScheduleState {
  /** 已启用且处于时段内；无法判断（新时段尚未落盘且无法推算）时为 null。 */
  active: boolean | null
  /** 下一次开始时间（秒）；进行中、已结束或无法判断时为 0。 */
  nextStartAt: number
  /** 指定日期时段已经结束。 */
  ended: boolean
}

const WEEK_MINUTES = 7 * 1440
const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

/**
 * 取某时刻在服务器时区内距周一 00:00 的分钟数。
 * @param now 时刻（秒）。
 * @param timezone 服务器时区名；无法识别（例如未设置 TZ 时的 "Local"）时退回浏览器时区。
 * @returns 周内分钟数；两种时区都无法解析时为 null。
 */
function weekMinutesAt(now: number, timezone: string): number | null {
  for (const timeZone of [timezone, undefined]) {
    try {
      const parts = new Intl.DateTimeFormat('en-US', {
        timeZone,
        weekday: 'short',
        hour: '2-digit',
        minute: '2-digit',
        hourCycle: 'h23',
      }).formatToParts(new Date(now * 1000))
      const part = (type: string) =>
        parts.find((item) => item.type === type)?.value ?? ''
      const weekday = WEEKDAYS.indexOf(part('weekday'))
      const hour = Number(part('hour'))
      const minute = Number(part('minute'))
      if (weekday >= 0 && Number.isInteger(hour) && Number.isInteger(minute)) {
        return weekday * 1440 + hour * 60 + minute
      }
    } catch {
      // 时区名无效时换浏览器时区重试。
    }
  }
  return null
}

/** @param value HH:mm。 @returns 当天分钟数；格式非法时为 null。 */
function minutesOf(value: string): number | null {
  const match = /^(\d{2}):(\d{2})$/.exec(value)
  if (!match) return null
  const hour = Number(match[1])
  const minute = Number(match[2])
  if (hour > 23 || minute > 59) return null
  return hour * 60 + minute
}

/**
 * 由一次出现的起止推导状态。
 * @param startAt 开始（秒）。 @param endAt 结束（秒）。 @param now 时刻（秒）。
 * @param dated 是否为指定日期时段（结束后不再有下一次）。
 * @returns 时段状态。
 */
function windowState(
  startAt: number,
  endAt: number,
  now: number,
  dated: boolean
): ChannelScheduleState {
  if (startAt <= 0 || endAt <= 0) {
    return { active: null, nextStartAt: 0, ended: false }
  }
  if (now >= endAt) return { active: false, nextStartAt: 0, ended: dated }
  if (now >= startAt) return { active: true, nextStartAt: 0, ended: false }
  return { active: false, nextStartAt: startAt, ended: false }
}

/**
 * 与后端 channelPeriodOccurrence 同语义：起止落在本周或上周的一次出现命中即进行中，否则下次开始取最近的未来起点。
 * @param schedule 每周重复时段。 @param now 时刻（秒）。 @param timezone 服务器时区。
 * @returns 时段状态。
 */
function weeklyState(
  schedule: ChannelBudgetSchedule,
  now: number,
  timezone: string
): ChannelScheduleState {
  const current = weekMinutesAt(now, timezone)
  const startTime = minutesOf(schedule.start_time)
  const endTime = minutesOf(schedule.end_time)
  if (current === null || startTime === null || endTime === null) {
    return { active: null, nextStartAt: 0, ended: false }
  }
  const start = schedule.start_weekday * 1440 + startTime
  let end = schedule.end_weekday * 1440 + endTime
  // 结束早于开始表示跨周，后端会把结束点推到下一周。
  if (end <= start) end += WEEK_MINUTES
  const elapsed = (current - start + WEEK_MINUTES) % WEEK_MINUTES
  if (elapsed < end - start) {
    return { active: true, nextStartAt: 0, ended: false }
  }
  // 分钟粒度推算：先对齐到当前分钟的整点，再加上距离下次开始的分钟数。
  const wait = WEEK_MINUTES - elapsed
  return {
    active: false,
    nextStartAt: now - (now % 60) + wait * 60,
    ended: false,
  }
}

/**
 * 解析每个时段是否进行中以及下次开始时间。
 * 优先使用预览接口返回的出现区间（服务器时区、含已保存行引用的时段）；没有引用时指定日期时段按起止推算，每周时段按服务器时区在前端推算。
 * @param schedules 草稿中的全部时段。
 * @param previewRows 权威配置的预览行，其 source 携带被引用时段的当前或下次出现区间。
 * @param now 服务器当前时刻（秒）。
 * @param timezone 服务器时区名。
 * @returns 按时段 id 索引的状态；停用的时段 active 一律为 false。
 */
export function resolveChannelScheduleStates(
  schedules: ChannelBudgetSchedule[],
  previewRows: ChannelBudgetPreviewRow[],
  now: number,
  timezone: string
): Map<string, ChannelScheduleState> {
  const sources = new Map<string, { start_at: number; end_at: number }>()
  for (const row of previewRows) {
    const source = row.source
    if (!source.schedule_id || !source.start_at || !source.end_at) continue
    if (!sources.has(source.schedule_id)) {
      sources.set(source.schedule_id, {
        start_at: source.start_at,
        end_at: source.end_at,
      })
    }
  }
  const states = new Map<string, ChannelScheduleState>()
  for (const schedule of schedules) {
    const dated = schedule.kind === 'date_range'
    const source = sources.get(schedule.id)
    let state: ChannelScheduleState
    if (source) {
      state = windowState(source.start_at, source.end_at, now, dated)
    } else if (dated) {
      state = windowState(schedule.start_at, schedule.end_at, now, true)
    } else {
      state = weeklyState(schedule, now, timezone)
    }
    if (!schedule.enabled && state.active !== null) {
      state = { ...state, active: false }
    }
    states.set(schedule.id, state)
  }
  return states
}
