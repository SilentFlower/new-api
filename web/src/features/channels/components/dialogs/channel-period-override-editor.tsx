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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  formatQuota,
  formatTimestampForInput,
  parseTimestampFromInput,
} from '@/lib/format'

import {
  channelPeriodErrorKey,
  getChannelPeriodPolicy,
  saveChannelBudgetOverride,
} from '../../period-api'
import { MAX_PERIOD_QUOTA } from '../../period-types'
import type { ChannelUserLimitStatus } from '../../types'
import { ChannelPeriodAmount } from './channel-period-amount'
import { ChannelPeriodMetrics } from './channel-period-metrics'

/** @param props 当前用户的权威状态、权限与刷新回调。 @returns 按预算行的个人提额编辑。 */
export function ChannelPeriodOverrideEditor(props: {
  status: ChannelUserLimitStatus
  canOperate: boolean
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const channelId = props.status.channel_id
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: ['channels', channelId, 'period-policy'],
    queryFn: () => getChannelPeriodPolicy(channelId),
    retry: false,
  })
  const [budgetId, setBudgetId] = useState('')
  const [amount, setAmount] = useState<number | null>(null)
  const [expires, setExpires] = useState('')
  const [stale, setStale] = useState(false)
  const [error, setError] = useState('')
  const rows =
    query.data?.config.budgets.filter(
      (row) => row.id && row.scope === 'user' && row.limit > 0
    ) ?? []
  const mutation = useMutation({
    mutationFn: (input: { limit: number; expires_at: number } | null) =>
      saveChannelBudgetOverride(
        channelId,
        budgetId,
        props.status.user.id,
        input
      ),
  })
  const submit = (remove: boolean) => {
    if (!budgetId || stale || mutation.isPending) return
    const expiration = expires ? parseTimestampFromInput(expires) : 0
    const base = rows.find((row) => row.id === budgetId)?.limit ?? 0
    if (
      !remove &&
      (amount === null ||
        !Number.isSafeInteger(amount) ||
        amount <= base ||
        amount > MAX_PERIOD_QUOTA ||
        !Number.isSafeInteger(expiration) ||
        (expires && expiration <= Date.now() / 1000))
    ) {
      setError(
        t('The override must exceed the budget limit and expire in the future.')
      )
      return
    }
    setError('')
    mutation.mutate(
      remove ? null : { limit: amount ?? 0, expires_at: expiration },
      {
        onSuccess: () => {
          props.onChanged()
          void queryClient.invalidateQueries({ queryKey: ['channels'] })
        },
        onError: (reason) => {
          setError(t(channelPeriodErrorKey(reason)))
          setStale(true)
        },
      }
    )
  }
  return (
    <section className='border-t pt-3'>
      <FieldGroup>
        <ChannelPeriodMetrics status={props.status.period_limits} />
        <h4 className='font-medium'>{t('Personal budget override')}</h4>
        <p className='text-muted-foreground text-sm'>
          {t(
            'This override only raises the selected budget for this user. Pool and other budgets still apply.'
          )}
        </p>
        {query.isError && (
          <p role='alert'>
            {t('Period policy is unavailable. Reload and try again.')}
          </p>
        )}
        {error && (
          <p role='alert' className='text-destructive text-sm'>
            {error}
          </p>
        )}
        <FieldSet
          disabled={
            !props.canOperate || mutation.isPending || stale || query.isPending
          }
          className='space-y-3'
        >
          <Field>
            <FieldLabel className='grid gap-1 text-sm'>
              {t('Budget')}
              <NativeSelect
                value={budgetId}
                onChange={(event) => {
                  const id = event.target.value
                  setBudgetId(id)
                  const metric = props.status.period_limits?.metrics.find(
                    (item) => item.budget_id === id
                  )
                  setAmount(metric?.override_limit ?? null)
                  setExpires(
                    metric?.source.expires_at
                      ? formatTimestampForInput(metric.source.expires_at)
                      : ''
                  )
                }}
              >
                <NativeSelectOption value='' disabled>
                  {t('Select a budget')}
                </NativeSelectOption>
                {rows.map((row) => (
                  <NativeSelectOption key={row.id} value={row.id}>
                    {row.name} · {formatQuota(row.limit)}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </FieldLabel>
          </Field>
          <ChannelPeriodAmount
            label={t('Override amount')}
            value={amount}
            onChange={setAmount}
          />
          <Field>
            <FieldLabel className='grid gap-1 text-sm'>
              {t('Override expiration (local time)')}
              <Input
                type='datetime-local'
                value={expires}
                onChange={(event) => setExpires(event.target.value)}
              />
            </FieldLabel>
          </Field>
          <p className='text-muted-foreground text-xs'>
            {t('Leave expiration blank for a permanent override.')}
          </p>
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              disabled={!budgetId}
              onClick={() => submit(false)}
            >
              {t('Save budget override')}
            </Button>
            <Button
              type='button'
              variant='outline'
              disabled={!budgetId}
              onClick={() => submit(true)}
            >
              {t('Revoke selected override')}
            </Button>
          </div>
        </FieldSet>
        {(stale || query.isError) && (
          <Button
            type='button'
            variant='outline'
            onClick={async () => {
              const result = await query.refetch()
              if (result.isSuccess) {
                setStale(false)
                setError('')
                props.onChanged()
              }
            }}
          >
            {t('Reload')}
          </Button>
        )}
      </FieldGroup>
    </section>
  )
}
