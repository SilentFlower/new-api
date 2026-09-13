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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatQuota, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { resolveChannelScheduleStates } from '../../../lib/channel-schedule-state'
import {
  channelPeriodErrorKey,
  getChannelBudgetUsageSummary,
  getChannelPeriodPolicy,
  previewChannelPeriodPolicy,
} from '../../../period-api'
import type {
  ChannelBudgetRow,
  ChannelPeriodConfig,
} from '../../../period-types'
import { computeBudgetRowStates, type BudgetRowState } from './budget-row-state'
import { BudgetUsageSheet } from './budget-usage-sheet'

/** @param status 行状态。 @returns 给 Progress 指示条上色的任意变体类名。 */
function progressTone(status: BudgetRowState['status'] | undefined) {
  if (status === 'exceeded') {
    return '[&_[data-slot=progress-indicator]]:bg-destructive'
  }
  if (status === 'near') return '[&_[data-slot=progress-indicator]]:bg-warning'
  return undefined
}

/** @param props 渠道与操作权限。 @returns 一次列出全部预算行当前窗口用量的页签；按行打开明细或调整。 */
export function ChannelBudgetUsageTab(props: {
  channelId: number
  canOperate: boolean
}) {
  const { t } = useTranslation()
  const [row, setRow] = useState<ChannelBudgetRow | null>(null)
  const policy = useQuery({
    queryKey: ['channels', props.channelId, 'period-policy'],
    queryFn: () => getChannelPeriodPolicy(props.channelId),
    retry: false,
  })
  const summary = useQuery({
    queryKey: ['channels', props.channelId, 'budget-usage-summary'],
    queryFn: () => getChannelBudgetUsageSummary(props.channelId),
    retry: false,
    refetchInterval: 30_000,
  })
  const revision = policy.data?.revision
  const effective = useQuery({
    queryKey: [
      'channels',
      props.channelId,
      'period-policy-effective',
      revision,
    ],
    queryFn: () =>
      previewChannelPeriodPolicy(
        props.channelId,
        policy.data?.revision ?? 0,
        policy.data?.config as ChannelPeriodConfig
      ),
    enabled: !!policy.data,
    refetchOnWindowFocus: false,
    retry: false,
  })
  const rows = policy.data?.config.budgets ?? []
  const summaries = new Map(
    (summary.data?.items ?? []).map((item) => [item.budget_id, item])
  )
  const states = policy.data
    ? computeBudgetRowStates(
        policy.data.config,
        policy.data.config,
        summaries,
        resolveChannelScheduleStates(
          policy.data.config.schedules,
          effective.data?.rows ?? [],
          policy.data.now,
          policy.data.timezone
        )
      )
    : new Map()
  const windowLabels = {
    daily: t('Daily'),
    weekly: t('Weekly'),
    occurrence: t('Whole period'),
  }
  const scopeLabels = { user: t('Per user'), pool: t('Pool') }
  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Current window usage for every budget, loaded in one request and refreshed every 30 seconds.'
          )}
        </p>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={summary.isFetching}
          onClick={() => void summary.refetch()}
        >
          {t('Refresh')}
        </Button>
      </div>
      {(policy.isPending || summary.isPending) && (
        <p role='status'>{t('Loading...')}</p>
      )}
      {(policy.isError || summary.isError) && (
        <p role='alert' className='text-destructive text-sm'>
          {t(channelPeriodErrorKey(policy.error ?? summary.error))}
        </p>
      )}
      {policy.data && rows.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t('No budgets configured.')}
        </p>
      )}
      {policy.data && summary.data && rows.length > 0 && (
        <div className='overflow-x-auto rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Budget')}</TableHead>
                <TableHead>{t('Window')}</TableHead>
                <TableHead className='text-right'>{t('Limit')}</TableHead>
                <TableHead>{t('Used')}</TableHead>
                <TableHead className='text-right'>{t('Remaining')}</TableHead>
                <TableHead>{t('Resets')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((item) => {
                const state = states.get(item.id) as BudgetRowState | undefined
                const usage = summaries.get(item.id)
                const limit = state?.effectiveLimit ?? item.limit
                const used = usage?.used_quota ?? 0
                const percent =
                  limit > 0
                    ? Math.min(100, Math.round((used / limit) * 100))
                    : 0
                const schedule = policy.data?.config.schedules.find(
                  (schedule) => schedule.id === item.schedule_id
                )
                return (
                  <TableRow
                    key={item.id}
                    data-status={state?.status}
                    className={
                      state?.status === 'off'
                        ? 'text-muted-foreground'
                        : undefined
                    }
                  >
                    <TableCell>
                      <div className='font-medium'>{item.name}</div>
                      <div className='text-muted-foreground text-xs'>
                        {scopeLabels[item.scope]} ·{' '}
                        {item.models.length
                          ? item.models.join(', ')
                          : t('All models')}
                      </div>
                    </TableCell>
                    <TableCell>
                      {windowLabels[item.window]}
                      {schedule ? ` · ${schedule.name}` : ''}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {item.limit > 0
                        ? formatQuota(item.limit)
                        : t('Unlimited')}
                    </TableCell>
                    <TableCell>
                      {state?.sharedWith ? (
                        <span className='text-muted-foreground text-xs'>
                          {t('Shares the counter with {{name}}', {
                            name: state.sharedWith.name,
                          })}
                        </span>
                      ) : (
                        <div className='flex min-w-40 items-center gap-2'>
                          {limit > 0 && (
                            <Progress
                              value={percent}
                              aria-label={t('Usage of {{name}}', {
                                name: item.name,
                              })}
                              className={cn(
                                'w-24',
                                progressTone(state?.status)
                              )}
                            />
                          )}
                          <span className='tabular-nums'>
                            {formatQuota(used)}
                          </span>
                          {item.scope === 'user' && usage?.top_user && (
                            <span className='text-muted-foreground text-xs'>
                              {usage.top_user.display_name ||
                                usage.top_user.username}
                            </span>
                          )}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {limit > 0 ? formatQuota(Math.max(0, limit - used)) : '—'}
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs'>
                      {usage ? formatTimestampToDate(usage.window_end) : '—'}
                    </TableCell>
                    <TableCell className='text-right'>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() => setRow(item)}
                      >
                        {item.scope === 'user'
                          ? t('User details')
                          : t('Adjust')}
                      </Button>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}
      <BudgetUsageSheet
        channelId={props.channelId}
        row={row}
        canOperate={props.canOperate}
        onClose={() => setRow(null)}
      />
    </div>
  )
}
