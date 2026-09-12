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
import type { ChannelPeriodMetric } from '../period-types'

/**
 * 生成预算行的展示标签，供指标卡片与按行选择器共用。
 * @param metric 预算名、范围、周期与模型。
 * @param t 翻译函数。
 * @returns 形如 "gpt-6-astra · 池子 · 今日" 的标签。
 */
export function channelPeriodMetricLabel(
  metric: Pick<
    ChannelPeriodMetric,
    'budget_name' | 'scope' | 'period' | 'models'
  >,
  t: (key: string) => string
): string {
  const periods = {
    daily: t('Today'),
    weekly: t('This week'),
    occurrence: t('Whole period'),
  }
  const scope = metric.scope === 'pool' ? t('Pool') : t('User')
  const models = metric.models.length > 0 ? metric.models.join(', ') : ''
  return [metric.budget_name, scope, periods[metric.period], models]
    .filter(Boolean)
    .join(' · ')
}
