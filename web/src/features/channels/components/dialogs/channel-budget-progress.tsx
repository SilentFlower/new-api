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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { formatQuota } from '@/lib/format'

import { getChannelUserLimitStatus } from '../../api'
import { channelPeriodErrorKey, getChannelBudgetUsage } from '../../period-api'
import type { ChannelBudgetRow } from '../../period-types'

/** @param props 渠道与已保存的预算行。 @returns 池子或最高用量用户的已用、剩余和进度。 */
export function ChannelBudgetProgress(props: {
  channelId: number
  row: ChannelBudgetRow
}) {
  const { t } = useTranslation()
  const usage = useQuery({
    queryKey: [
      'channels',
      props.channelId,
      'budget-usage',
      props.row.id,
      props.row.scope,
      1,
    ],
    queryFn: () =>
      getChannelBudgetUsage(props.channelId, props.row.id, {
        scope: props.row.scope,
        p: 1,
        page_size: 20,
      }),
    retry: false,
    staleTime: 5000,
  })
  const user = props.row.scope === 'user' ? usage.data?.items?.[0] : undefined
  const status = useQuery({
    queryKey: [
      'channels',
      props.channelId,
      'budget-progress-user',
      user?.user_id,
    ],
    queryFn: () =>
      getChannelUserLimitStatus(props.channelId, user?.user_id ?? 0),
    enabled: user !== undefined,
    retry: false,
    staleTime: 5000,
  })
  const metric = status.data?.period_limits?.metrics.find(
    (item) => item.budget_id === props.row.id
  )
  if (usage.isError || status.isError) {
    return (
      <div className='space-y-1 text-xs'>
        <span role='status'>
          {t(channelPeriodErrorKey(usage.error ?? status.error))}
        </span>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={() => {
            void usage.refetch()
            if (user) void status.refetch()
          }}
        >
          {t('Retry')}
        </Button>
      </div>
    )
  }
  if (usage.isPending || (user && status.isPending)) {
    return <span role='status'>{t('Loading...')}</span>
  }
  // 无当前生效指标时只展示该行真实计数，不能把被时段覆盖的基础额度冒充有效剩余。
  const used =
    props.row.scope === 'pool'
      ? (usage.data?.used_quota ?? 0)
      : (user?.used_quota ?? 0)
  const limit = user ? metric?.limit : props.row.limit
  const remaining =
    limit !== undefined && limit > 0 ? Math.max(0, limit - used) : null
  let remainingLabel = t('Inactive now')
  if (limit !== undefined) {
    remainingLabel =
      remaining === null ? t('Unlimited') : formatQuota(remaining)
  }
  return (
    <div className='min-w-40 space-y-1 text-xs'>
      {user && (
        <div>
          {t('Highest usage: {{user}}', {
            user: user.display_name || user.username || `#${user.user_id}`,
          })}
        </div>
      )}
      <div>
        {t('Used')}: {formatQuota(used)}
      </div>
      <div>
        {t('Remaining')}: {remainingLabel}
      </div>
      {limit !== undefined && limit > 0 && (
        <Progress
          aria-label={t('Usage')}
          value={Math.min(100, (used / limit) * 100)}
        />
      )}
    </div>
  )
}
