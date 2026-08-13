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

import { Skeleton } from '@/components/ui/skeleton'
import { formatLogQuota } from '@/lib/format'

import { DEFAULT_STAT } from '../constants'
import { formatCompactNumber } from '../lib/chart'
import type { TokenLogStat } from '../types'
import { StatBadgeCard } from './stat-badge-card'

export function TokenLogStatsCards(props: {
  stat?: TokenLogStat
  isLoading: boolean
  usageAvailable: boolean
}) {
  const { t } = useTranslation()
  const stat = props.stat ?? DEFAULT_STAT
  const usageDescription = props.usageAvailable
    ? undefined
    : t('Usage statistics are only available for consumption logs')

  if (props.isLoading) {
    return (
      <div className='grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
        {[0, 1, 2, 3].map((item) => (
          <Skeleton key={item} className='h-[88px] rounded-lg' />
        ))}
      </div>
    )
  }

  return (
    <div className='grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
      <StatBadgeCard
        label={t('Requests')}
        value={formatCompactNumber(stat.count)}
        accent='bg-emerald-500/70'
      />
      <StatBadgeCard
        label={t('Usage')}
        value={formatLogQuota(stat.quota)}
        description={usageDescription}
        accent='bg-sky-500/70'
      />
      <StatBadgeCard
        label={t('Tokens')}
        value={formatCompactNumber(
          stat.total_tokens || stat.prompt_tokens + stat.completion_tokens
        )}
        description={
          props.usageAvailable
            ? `${formatCompactNumber(stat.prompt_tokens)} / ${formatCompactNumber(stat.completion_tokens)}`
            : usageDescription
        }
        accent='bg-amber-500/75'
      />
      <StatBadgeCard
        label='RPM / TPM'
        value={`${formatCompactNumber(stat.rpm)} / ${formatCompactNumber(stat.tpm)}`}
        description={
          props.usageAvailable
            ? undefined
            : t('TPM only applies to consumption logs')
        }
        accent='bg-violet-500/70'
      />
    </div>
  )
}
