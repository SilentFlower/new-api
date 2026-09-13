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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { Switch } from '@/components/ui/switch'
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

import type { ChannelScheduleState } from '../../../lib/channel-schedule-state'
import type {
  ChannelBudgetRow,
  ChannelBudgetSchedule,
} from '../../../period-types'
import {
  type BudgetFilter,
  type BudgetRowState,
  matchesBudgetFilter,
} from './budget-row-state'
import { BudgetStatusBadge } from './budget-status-badge'

/** @param props 预算行、派生状态、时段状态、后端点名的出错行、筛选与行操作回调。 @returns 摘要条、筛选、工具栏与预算表。 */
export function BudgetTable(props: {
  rows: ChannelBudgetRow[]
  states: Map<string, BudgetRowState>
  schedules: ChannelBudgetSchedule[]
  scheduleStates: Map<string, ChannelScheduleState>
  errorRows: string[]
  errorDetail: string
  filter: BudgetFilter
  onFilterChange: (filter: BudgetFilter) => void
  nextChangeAt: number
  editingIndex: number | null
  disabled: boolean
  summaryState: 'loading' | 'error' | 'ready'
  onRetrySummary: () => void
  toolbarEnd?: ReactNode
  onToggle: (index: number, enabled: boolean) => void
  onEdit: (index: number) => void
  onUsage: (row: ChannelBudgetRow) => void
  onOverride: (row: ChannelBudgetRow) => void
  actionSummary: (row: ChannelBudgetRow) => string
}) {
  const { t } = useTranslation()
  const scopeLabels = { user: t('Per user'), pool: t('Pool') }
  const windowLabels = {
    daily: t('Daily'),
    weekly: t('Weekly'),
    occurrence: t('Whole period'),
  }
  const counts = {
    all: 0,
    active: 0,
    attention: 0,
    off: 0,
    near: 0,
    exceeded: 0,
  }
  for (const row of props.rows) {
    const status = props.states.get(row.id)?.status ?? 'active'
    counts.all += 1
    if (status === 'active' || status === 'pending') counts.active += 1
    if (status === 'near') counts.near += 1
    if (status === 'exceeded') counts.exceeded += 1
    if (status === 'near' || status === 'exceeded') counts.attention += 1
    if (status === 'off') counts.off += 1
  }
  const filters: { key: BudgetFilter; label: string; count: number }[] = [
    { key: 'all', label: t('All'), count: counts.all },
    { key: 'active', label: t('Active now'), count: counts.active },
    { key: 'attention', label: t('Needs attention'), count: counts.attention },
    { key: 'off', label: t('Disabled'), count: counts.off },
  ]
  const visible = props.rows
    .map((row, index) => ({ row, index }))
    .filter(({ row }) =>
      matchesBudgetFilter(
        props.states.get(row.id)?.status ?? 'active',
        props.filter
      )
    )
  const renderUsage = (row: ChannelBudgetRow, state: BudgetRowState) => {
    if (state.isNew) {
      return (
        <span className='text-muted-foreground text-xs'>
          {t('Save to view usage')}
        </span>
      )
    }
    if (state.sharedWith) {
      return (
        <div className='text-muted-foreground space-y-0.5 text-xs'>
          <div>
            {t('Shares the counter with {{name}}', {
              name: state.sharedWith.name || t('Untitled budget'),
            })}
          </div>
          <div className='tabular-nums'>
            {t('Current {{used}} · row limit {{limit}}', {
              used: formatQuota(state.summary?.used_quota ?? 0),
              limit: row.limit > 0 ? formatQuota(row.limit) : t('Unlimited'),
            })}
          </div>
        </div>
      )
    }
    if (props.summaryState === 'loading') {
      return (
        <span role='status' className='text-muted-foreground text-xs'>
          {t('Loading...')}
        </span>
      )
    }
    if (props.summaryState === 'error' || !state.summary) {
      return (
        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={props.onRetrySummary}
        >
          {t('Retry')}
        </Button>
      )
    }
    const used = state.summary.used_quota
    const limit = state.effectiveLimit
    const percent =
      limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0
    const remaining = limit > 0 ? Math.max(0, limit - used) : null
    // Progress 自带轨道与指示条；通过任意变体给指示条上色，不再自行嵌套子元素。
    let tone: string | undefined
    if (state.status === 'exceeded') {
      tone = '[&_[data-slot=progress-indicator]]:bg-destructive'
    } else if (state.status === 'near') {
      tone = '[&_[data-slot=progress-indicator]]:bg-warning'
    }
    return (
      <div className='min-w-44 space-y-1'>
        <div className='flex items-baseline justify-between gap-2 text-xs tabular-nums'>
          <span className='font-medium'>{formatQuota(used)}</span>
          <span className='text-muted-foreground'>
            {remaining === null
              ? t('Unlimited')
              : `${t('Remaining')} ${formatQuota(remaining)} · ${percent}%`}
          </span>
        </div>
        {limit > 0 && (
          <Progress
            value={percent}
            className={tone}
            aria-label={t('Usage of {{name}}', { name: row.name })}
          />
        )}
        {row.scope === 'user' && (
          <div className='text-muted-foreground text-xs'>
            {state.summary.top_user
              ? t('Highest usage: {{user}}', {
                  user:
                    state.summary.top_user.display_name ||
                    state.summary.top_user.username ||
                    `#${state.summary.top_user.user_id}`,
                }) +
                (state.summary.top_user.override
                  ? ` · ${t('Raised to {{limit}}', {
                      limit: formatQuota(
                        state.summary.top_user.effective_limit
                      ),
                    })}`
                  : '')
              : t('No user usage in this window yet')}
          </div>
        )}
      </div>
    )
  }
  return (
    <section className='space-y-3' aria-label={t('Budgets')}>
      <div className='bg-muted/50 flex flex-wrap items-center gap-x-5 gap-y-1 rounded-lg px-3 py-2 text-sm'>
        <span>
          <span className='font-semibold tabular-nums'>{counts.active}</span>{' '}
          {t('Active now')}
        </span>
        <span className='flex items-center gap-1.5'>
          <Badge variant='warning'>{counts.near}</Badge> {t('Near limit')}
        </span>
        <span className='flex items-center gap-1.5'>
          <Badge variant='destructive'>{counts.exceeded}</Badge> {t('Exceeded')}
        </span>
        <span className='flex items-center gap-1.5'>
          <Badge variant='ghost'>{counts.off}</Badge> {t('Disabled')}
        </span>
        {props.nextChangeAt > 0 && (
          <span className='text-muted-foreground ml-auto text-xs'>
            {t('Next schedule change: {{time}}', {
              time: formatTimestampToDate(props.nextChangeAt),
            })}
          </span>
        )}
      </div>
      <div className='flex flex-wrap items-center gap-2'>
        <div
          className='flex flex-wrap gap-1.5'
          role='group'
          aria-label={t('Filter budgets')}
        >
          {filters.map((item) => (
            <Button
              key={item.key}
              type='button'
              size='sm'
              variant={props.filter === item.key ? 'default' : 'outline'}
              aria-pressed={props.filter === item.key}
              onClick={() => props.onFilterChange(item.key)}
            >
              {item.label}
              <span className='tabular-nums opacity-70'>{item.count}</span>
            </Button>
          ))}
        </div>
        <span className='flex-1' />
        {props.toolbarEnd}
      </div>
      {props.rows.length === 0 && (
        <p className='text-muted-foreground rounded-lg border px-4 py-8 text-center text-sm'>
          {t('No budgets configured.')}
        </p>
      )}
      {props.rows.length > 0 && visible.length === 0 && (
        <p className='text-muted-foreground rounded-lg border px-4 py-8 text-center text-sm'>
          {t('No budgets match this filter.')}
        </p>
      )}
      {visible.length > 0 && (
        <div className='overflow-x-auto rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='w-12'>{t('Enabled')}</TableHead>
                <TableHead>{t('Budget')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>
                  {t('Scope')} · {t('Window')}
                </TableHead>
                <TableHead>{t('Schedule')}</TableHead>
                <TableHead className='text-right'>{t('Limit')}</TableHead>
                <TableHead>{t('Current usage')}</TableHead>
                <TableHead>{t('When exceeded')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map(({ row, index }) => {
                const state = props.states.get(row.id)
                if (!state) return null
                const schedule = props.schedules.find(
                  (item) => item.id === row.schedule_id
                )
                const scheduleState = schedule
                  ? props.scheduleStates.get(schedule.id)
                  : undefined
                // 时段列第二行：停用 > 进行中 > 已结束 > 下次开始 > 未开始；新时段无法推算时不显示。
                let scheduleHint = ''
                if (schedule && !schedule.enabled) scheduleHint = t('Disabled')
                else if (scheduleState?.active) scheduleHint = t('In progress')
                else if (scheduleState?.ended) scheduleHint = t('Ended')
                else if (scheduleState && scheduleState.nextStartAt > 0) {
                  scheduleHint = t('Next start {{time}}', {
                    time: formatTimestampToDate(scheduleState.nextStartAt),
                  })
                } else if (scheduleState?.active === false) {
                  scheduleHint = t('Not started')
                }
                const muted =
                  state.status === 'off' || state.status === 'pending'
                const rowError = props.errorRows.includes(row.id)
                let accent: string | undefined
                if (rowError) {
                  accent =
                    'bg-destructive/5 shadow-[inset_3px_0_0_var(--destructive)]'
                } else if (state.changed || state.isNew) {
                  accent = 'shadow-[inset_3px_0_0_var(--primary)]'
                }
                return (
                  <TableRow
                    key={row.id}
                    data-state={
                      props.editingIndex === index ? 'selected' : undefined
                    }
                    data-status={state.status}
                    data-error={rowError ? 'true' : undefined}
                    className={cn(
                      accent,
                      muted ? 'text-muted-foreground' : undefined
                    )}
                  >
                    <TableCell>
                      <Switch
                        aria-label={t('Enable {{name}}', {
                          name: row.name || t('Untitled budget'),
                        })}
                        checked={row.enabled}
                        disabled={props.disabled}
                        onCheckedChange={(checked) =>
                          props.onToggle(index, checked)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <div
                        className={cn(
                          'font-medium',
                          muted ? 'text-muted-foreground' : 'text-foreground'
                        )}
                      >
                        {row.name || t('Untitled budget')}
                        {state.isNew && (
                          <Badge variant='outline' className='ml-2'>
                            {t('New')}
                          </Badge>
                        )}
                      </div>
                      <div className='mt-0.5 flex flex-wrap gap-1'>
                        {row.models.length === 0 ? (
                          <span className='text-muted-foreground text-xs'>
                            {t('All models')}
                          </span>
                        ) : (
                          row.models.map((model) => (
                            <Badge
                              key={model}
                              variant='secondary'
                              className='font-mono text-[11px]'
                            >
                              {model}
                            </Badge>
                          ))
                        )}
                      </div>
                      {rowError && (
                        <p
                          data-slot='budget-row-error'
                          className='text-destructive mt-1 text-xs'
                        >
                          {props.errorDetail}
                        </p>
                      )}
                    </TableCell>
                    <TableCell>
                      <BudgetStatusBadge status={state.status} />
                    </TableCell>
                    <TableCell className='whitespace-nowrap'>
                      {scopeLabels[row.scope]} · {windowLabels[row.window]}
                    </TableCell>
                    <TableCell>
                      {schedule ? (
                        <div className='space-y-0.5'>
                          <div>{schedule.name || t('Untitled schedule')}</div>
                          {scheduleHint && (
                            <div className='text-muted-foreground text-xs'>
                              {scheduleHint}
                            </div>
                          )}
                        </div>
                      ) : (
                        <span className='text-muted-foreground'>
                          {t('No schedule')}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {row.limit > 0 ? formatQuota(row.limit) : t('Unlimited')}
                    </TableCell>
                    <TableCell>{renderUsage(row, state)}</TableCell>
                    <TableCell className='text-sm'>
                      {props.actionSummary(row)}
                    </TableCell>
                    <TableCell className='text-right'>
                      <div className='flex justify-end gap-1'>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          onClick={() => props.onEdit(index)}
                        >
                          {t('Edit')}
                        </Button>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          disabled={state.isNew}
                          onClick={() => props.onUsage(row)}
                        >
                          {t('Usage')}
                        </Button>
                        {row.scope === 'user' && (
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            disabled={state.isNew || row.limit <= 0}
                            onClick={() => props.onOverride(row)}
                          >
                            {t('Raise limit')}
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
  )
}
