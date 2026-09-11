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
  saveChannelPeriodOverride,
} from '../../period-api'
import type { ChannelUserLimitStatus } from '../../types'
import { ChannelPeriodMetrics } from './channel-period-metrics'
import { ChannelPeriodAmount } from './channel-period-policy-panel'

/** @param props 当前用户的权威状态、权限与刷新回调。 @returns 整段特批编辑。 */
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
  const [ruleId, setRuleId] = useState('')
  const [amount, setAmount] = useState<number | null>(null)
  const [expires, setExpires] = useState('')
  const [stale, setStale] = useState(false)
  const [error, setError] = useState('')
  const rules =
    query.data?.config.rules.filter(
      (rule) => rule.id && (rule.user_period_quota_limit ?? 0) > 0
    ) ?? []
  const mutation = useMutation({
    mutationFn: (
      input: { user_period_quota_limit: number; expires_at: number } | null
    ) =>
      saveChannelPeriodOverride(channelId, ruleId, props.status.user.id, input),
  })
  const submit = (remove: boolean) => {
    if (!ruleId || stale || mutation.isPending) return
    const expiration = expires ? parseTimestampFromInput(expires) : 0
    const base =
      rules.find((rule) => rule.id === ruleId)?.user_period_quota_limit ?? 0
    if (
      !remove &&
      (amount === null ||
        !Number.isSafeInteger(amount) ||
        amount <= base ||
        amount > 2147483647 ||
        !Number.isSafeInteger(expiration) ||
        (expires && expiration <= Date.now() / 1000))
    ) {
      setError(
        t('The override must exceed the rule limit and expire in the future.')
      )
      return
    }
    setError('')
    mutation.mutate(
      remove
        ? null
        : { user_period_quota_limit: amount ?? 0, expires_at: expiration },
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
        <h4 className='font-medium'>{t('Whole-period personal override')}</h4>
        <p className='text-muted-foreground text-sm'>
          {t(
            'This override only changes the selected rule for this user. Pool and other limits still apply.'
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
              {t('Time rule')}
              <NativeSelect
                value={ruleId}
                onChange={(event) => {
                  const id = event.target.value
                  setRuleId(id)
                  const metric = props.status.period_limits?.metrics.find(
                    (item) =>
                      item.scope === 'user' && item.source.rule_id === id
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
                  {t('Select a rule')}
                </NativeSelectOption>
                {rules.map((rule) => (
                  <NativeSelectOption key={rule.id} value={rule.id}>
                    {rule.name} ·{' '}
                    {formatQuota(rule.user_period_quota_limit ?? 0)}
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
              disabled={!ruleId}
              onClick={() => submit(false)}
            >
              {t('Save whole-period override')}
            </Button>
            <Button
              type='button'
              variant='outline'
              disabled={!ruleId}
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
