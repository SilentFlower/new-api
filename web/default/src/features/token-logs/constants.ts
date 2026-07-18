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
import { LOG_TYPE_ENUM } from '@/features/usage-logs/constants'

import type {
  TokenLogChartData,
  TokenLogPageData,
  TokenLogStat,
} from './types'

export const DEFAULT_PAGE_DATA: TokenLogPageData = {
  items: [],
  page: 1,
  page_size: 20,
  total: 0,
}

export const DEFAULT_STAT: TokenLogStat = {
  count: 0,
  quota: 0,
  prompt_tokens: 0,
  completion_tokens: 0,
  total_tokens: 0,
  rpm: 0,
  tpm: 0,
}

export const DEFAULT_CHART_DATA: TokenLogChartData = {
  model_stats: [],
  quota_data: [],
}

export const logTypeRowTint: Record<number, string> = {
  [LOG_TYPE_ENUM.ERROR]: 'bg-rose-50/40 dark:bg-rose-950/20',
  [LOG_TYPE_ENUM.REFUND]: 'bg-blue-50/30 dark:bg-blue-950/15',
}

export const chartAccentClasses = [
  'bg-sky-500',
  'bg-emerald-500',
  'bg-amber-500',
  'bg-rose-500',
  'bg-violet-500',
  'bg-cyan-500',
]
