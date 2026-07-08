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
  DASHBOARD_CHART_PREFERENCES_STORAGE_KEY,
  DEFAULT_DASHBOARD_CHART_PREFERENCES,
  DEFAULT_TIME_GRANULARITY,
  EMPTY_DASHBOARD_FILTERS,
  TIME_GRANULARITY_STORAGE_KEY,
  TIME_RANGE_PRESETS,
  TIME_RANGE_BY_GRANULARITY,
} from '@/features/dashboard/constants'
import type {
  ConsumptionDistributionChartType,
  DashboardChartPreferences,
  DashboardFilters,
  DashboardQueryParams,
  DashboardTokenOption,
  ModelAnalyticsChartTab,
} from '@/features/dashboard/types'
import { getRollingDateRange, type TimeGranularity } from '@/lib/time'

const DASHBOARD_TOKEN_VALUE_SEPARATOR = '\u0000'

function isTimeGranularity(value: unknown): value is TimeGranularity {
  return value === 'hour' || value === 'day' || value === 'week'
}

function getLegacySavedGranularity(): TimeGranularity {
  if (typeof window === 'undefined') return DEFAULT_TIME_GRANULARITY
  const saved = localStorage.getItem(TIME_GRANULARITY_STORAGE_KEY)
  return isTimeGranularity(saved) ? saved : DEFAULT_TIME_GRANULARITY
}

function isConsumptionDistributionChartType(
  value: unknown
): value is ConsumptionDistributionChartType {
  return value === 'bar' || value === 'area'
}

function isModelAnalyticsChartTab(
  value: unknown
): value is ModelAnalyticsChartTab {
  return value === 'trend' || value === 'proportion' || value === 'top'
}

function isTimeRangePresetDays(value: unknown): value is number {
  return TIME_RANGE_PRESETS.some((preset) => preset.days === value)
}

export function cleanFilters<T extends Record<string, unknown>>(
  filters: T
): Partial<T> {
  const cleaned: Partial<T> = {}
  for (const [key, value] of Object.entries(filters)) {
    if (value === undefined || value === null) continue
    if (typeof value === 'string') {
      const trimmed = value.trim()
      if (trimmed) cleaned[key as keyof T] = trimmed as T[keyof T]
      continue
    }
    if (Array.isArray(value)) {
      const normalized = Array.from(
        new Set(
          value
            .map((item) => String(item).trim())
            .filter((item) => item.length > 0)
        )
      )
      if (normalized.length > 0) {
        cleaned[key as keyof T] = normalized as T[keyof T]
      }
      continue
    }
    cleaned[key as keyof T] = value as T[keyof T]
  }
  return cleaned
}

export function getSavedGranularity(
  override?: TimeGranularity
): TimeGranularity {
  if (override) return override
  return getSavedChartPreferences().defaultTimeGranularity
}

export function saveGranularity(granularity: TimeGranularity): void {
  if (typeof window === 'undefined') return
  saveChartPreferences({
    ...getSavedChartPreferences(),
    defaultTimeGranularity: granularity,
  })
  localStorage.setItem(TIME_GRANULARITY_STORAGE_KEY, granularity)
}

export function getSavedChartPreferences(): DashboardChartPreferences {
  if (typeof window === 'undefined') return DEFAULT_DASHBOARD_CHART_PREFERENCES

  const fallbackPreferences = {
    ...DEFAULT_DASHBOARD_CHART_PREFERENCES,
    defaultTimeGranularity: getLegacySavedGranularity(),
  }

  try {
    const raw = localStorage.getItem(DASHBOARD_CHART_PREFERENCES_STORAGE_KEY)
    if (!raw) return fallbackPreferences

    const parsed = JSON.parse(raw) as Partial<DashboardChartPreferences>
    return {
      consumptionDistributionChart: isConsumptionDistributionChartType(
        parsed.consumptionDistributionChart
      )
        ? parsed.consumptionDistributionChart
        : fallbackPreferences.consumptionDistributionChart,
      modelAnalyticsChart: isModelAnalyticsChartTab(parsed.modelAnalyticsChart)
        ? parsed.modelAnalyticsChart
        : fallbackPreferences.modelAnalyticsChart,
      defaultTimeRangeDays: isTimeRangePresetDays(parsed.defaultTimeRangeDays)
        ? parsed.defaultTimeRangeDays
        : fallbackPreferences.defaultTimeRangeDays,
      defaultTimeGranularity: isTimeGranularity(parsed.defaultTimeGranularity)
        ? parsed.defaultTimeGranularity
        : fallbackPreferences.defaultTimeGranularity,
    }
  } catch {
    return fallbackPreferences
  }
}

export function saveChartPreferences(
  preferences: DashboardChartPreferences
): void {
  if (typeof window === 'undefined') return
  localStorage.setItem(
    DASHBOARD_CHART_PREFERENCES_STORAGE_KEY,
    JSON.stringify(preferences)
  )
}

export function getDefaultDays(granularity?: TimeGranularity): number {
  if (!granularity) return getSavedChartPreferences().defaultTimeRangeDays
  return TIME_RANGE_BY_GRANULARITY[getSavedGranularity(granularity)]
}

export function buildDefaultDashboardFilters(
  preferences: DashboardChartPreferences = getSavedChartPreferences()
): DashboardFilters {
  const { start, end } = getRollingDateRange(preferences.defaultTimeRangeDays)
  return {
    ...EMPTY_DASHBOARD_FILTERS,
    start_timestamp: start,
    end_timestamp: end,
    time_granularity: preferences.defaultTimeGranularity,
  }
}

export function buildQueryParams(
  timeRange: { start_timestamp: number; end_timestamp: number },
  filters?: {
    time_granularity?: TimeGranularity
    username?: string
    groups?: string[]
    token_names?: string[]
  }
): DashboardQueryParams {
  return {
    ...timeRange,
    default_time: getSavedGranularity(filters?.time_granularity),
    ...(filters?.username && { username: filters.username }),
    ...(filters?.groups?.length ? { groups: filters.groups } : {}),
    ...(filters?.token_names?.length
      ? { token_names: filters.token_names }
      : {}),
  }
}

export function normalizeSelectValues(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.map((item) => String(item)).filter(Boolean)
  }
  if (value === undefined || value === null || value === '') {
    return []
  }
  return [String(value)]
}

export function buildDashboardTokenOptionValue(
  tokenName: string,
  username = '',
  group = ''
): string {
  return [tokenName || '', username || '', group || ''].join(
    DASHBOARD_TOKEN_VALUE_SEPARATOR
  )
}

export function extractDashboardTokenName(value: string): string {
  return String(value || '').split(DASHBOARD_TOKEN_VALUE_SEPARATOR)[0].trim()
}

function getDashboardTokenOptionGroup(option: DashboardTokenOption): string {
  if (option.group != null) return String(option.group)
  return (
    String(option.value || '').split(DASHBOARD_TOKEN_VALUE_SEPARATOR)[2] || ''
  )
}

export function filterDashboardTokenOptionsByGroups(
  options: DashboardTokenOption[],
  groups: string[] | undefined
): DashboardTokenOption[] {
  const selectedGroups = normalizeSelectValues(groups)
  if (selectedGroups.length === 0) return options
  const groupSet = new Set(selectedGroups)
  return options.filter((option) =>
    groupSet.has(getDashboardTokenOptionGroup(option))
  )
}

export function filterDashboardTokenValuesByGroups(
  values: string[] | undefined,
  groups: string[] | undefined,
  options: DashboardTokenOption[]
): string[] {
  const selectedValues = normalizeSelectValues(values)
  const selectedGroups = normalizeSelectValues(groups)
  if (selectedGroups.length === 0) return selectedValues
  const allowedValues = new Set(
    filterDashboardTokenOptionsByGroups(options, selectedGroups).map(
      (option) => option.value
    )
  )
  return selectedValues.filter((value) => allowedValues.has(value))
}

export function appendDashboardFilterParams(
  params: URLSearchParams,
  name: 'groups' | 'token_names',
  values: string[] | undefined
): void {
  const seen = new Set<string>()
  for (const value of normalizeSelectValues(values)) {
    const normalized =
      name === 'token_names' ? extractDashboardTokenName(value) : value.trim()
    if (!normalized || seen.has(normalized)) continue
    seen.add(normalized)
    params.append(name, normalized)
  }
}

export function buildDashboardSearchParams(
  params: DashboardQueryParams
): URLSearchParams {
  const searchParams = new URLSearchParams()
  searchParams.set('start_timestamp', String(params.start_timestamp))
  searchParams.set('end_timestamp', String(params.end_timestamp))
  if (params.default_time) searchParams.set('default_time', params.default_time)
  if (params.username?.trim()) searchParams.set('username', params.username)
  appendDashboardFilterParams(searchParams, 'groups', params.groups)
  appendDashboardFilterParams(searchParams, 'token_names', params.token_names)
  return searchParams
}
