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
import { useTranslation } from 'react-i18next'

import { formatQuota, formatTimestampToDate } from '@/lib/format'

import type {
  ChannelPeriodSource,
  ChannelPeriodStatus,
} from '../../period-types'

/** @param props 服务端指标来源。 @returns 本地化来源名称。 */
export function ChannelPeriodSourceLabel(props: {
  source: ChannelPeriodSource
}) {
  const { t } = useTranslation()
  const labels = {
    default: t('Default'),
    weekly: t('Weekly schedule'),
    date_range: t('Specific dates'),
    personal: t('Personal override'),
  }
  return (
    <>
      {labels[props.source.kind]}
      {props.source.rule_name ? ` · ${props.source.rule_name}` : ''}
    </>
  )
}

/** @param props 权威周期状态。 @returns 个人、池子与整段指标及统计起点。 */
export function ChannelPeriodMetrics(props: { status?: ChannelPeriodStatus }) {
  const { t } = useTranslation()
  if (!props.status) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('Period status is not available on this server.')}
      </p>
    )
  }
  const periods = {
    daily: t('Daily quota'),
    weekly: t('Weekly quota'),
    custom: t('Whole-period quota'),
  }
  return (
    <section
      className='space-y-3 text-sm'
      aria-label={t('Period quota status')}
    >
      <p>
        {t('Server timezone: {{timezone}}', {
          timezone: props.status.timezone,
        })}
      </p>
      {props.status.storage_mode === 'memory' && (
        <p className='text-muted-foreground'>
          {t('Single-instance usage resets after a restart.')}
        </p>
      )}
      {props.status.next_change_at > 0 && (
        <p>
          {t('Next rule change: {{time}}', {
            time: formatTimestampToDate(props.status.next_change_at),
          })}
        </p>
      )}
      <div className='grid gap-3 sm:grid-cols-2'>
        {props.status.metrics.map((metric) => (
          <div
            key={`${metric.scope}:${metric.period}:${metric.source.rule_id ?? ''}`}
            className='space-y-1 rounded-lg border p-3'
          >
            <div className='font-medium'>
              {metric.scope === 'pool' ? t('Pool') : t('User')} ·{' '}
              {periods[metric.period]}
            </div>
            <div>
              {formatQuota(metric.used)} /{' '}
              {metric.limit > 0 ? formatQuota(metric.limit) : t('Unlimited')}
            </div>
            <div>
              {t('Remaining')}:{' '}
              {metric.remaining === null
                ? t('Unlimited')
                : formatQuota(metric.remaining)}
            </div>
            <div className='text-muted-foreground'>
              <ChannelPeriodSourceLabel source={metric.source} />
              {!metric.enforced &&
                ` · ${t('Overridden by a higher-priority rule')}`}
            </div>
            <div>
              {t('Resets at {{time}}', {
                time: formatTimestampToDate(metric.reset_at),
              })}
            </div>
            {!!metric.source.expires_at && (
              <div>
                {t('Override expires: {{time}}', {
                  time: formatTimestampToDate(metric.source.expires_at),
                })}
              </div>
            )}
            {metric.tracking_since > 0 && (
              <p className='text-muted-foreground'>
                {t('Tracking since {{time}}; earlier usage is excluded.', {
                  time: formatTimestampToDate(metric.tracking_since),
                })}
              </p>
            )}
            {metric.coverage === 'incomplete' && (
              <p className='text-destructive'>
                {t(
                  'Some usage could not be recorded. Totals may be incomplete.'
                )}
              </p>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}
