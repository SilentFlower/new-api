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
import { zodResolver } from '@hookform/resolvers/zod'
import {
  useIsMutating,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { useState } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { getCurrencyLabel } from '@/lib/currency'
import {
  formatQuota,
  formatTimestampToDate,
  parseQuotaFromDollars,
  quotaUnitsToDollars,
} from '@/lib/format'

import {
  channelPeriodErrorKey,
  getChannelPeriodPolicy,
  getChannelPeriodTargets,
  saveChannelPeriodPolicy,
} from '../../period-api'
import {
  channelPeriodConfigSchema,
  periodQuotaKeys,
  type ChannelPeriodConfig,
  type ChannelPeriodTarget,
  type ChannelPeriodView,
} from '../../period-types'
import { ChannelPeriodSourceLabel } from './channel-period-metrics'

/** @param props 金额输入契约，null 为继承。 @returns 按当前币种转换的输入框。 */
export function ChannelPeriodAmount(props: {
  label: string
  value: number | null
  nullable?: boolean
  onChange: (value: number | null) => void
}) {
  const { t } = useTranslation()
  return (
    <Field>
      <FieldLabel className='grid gap-1 text-sm'>
        {props.label} ({getCurrencyLabel()})
        <Input
          type='number'
          min='0'
          step='any'
          required={!props.nullable}
          value={
            props.value === null || Number.isNaN(props.value)
              ? ''
              : quotaUnitsToDollars(props.value)
          }
          placeholder={
            props.nullable
              ? t('Blank inherits; zero means unlimited')
              : undefined
          }
          onChange={(event) => {
            const text = event.target.value
            if (text === '') {
              props.onChange(props.nullable ? null : Number.NaN)
              return
            }
            const amount = Number(text)
            props.onChange(amount < 0 ? -1 : parseQuotaFromDollars(amount))
          }}
        />
      </FieldLabel>
    </Field>
  )
}

/** @param props 渠道 ID 和操作权限。 @returns 独立查询的周期管理面板。 */
export function ChannelPeriodPolicyPanel(props: {
  channelId: number
  canOperate: boolean
}) {
  const { t } = useTranslation()
  const writing =
    useIsMutating({
      mutationKey: ['channels', props.channelId, 'period-policy'],
    }) > 0
  const [reload, setReload] = useState(0)
  const query = useQuery({
    queryKey: ['channels', props.channelId, 'period-policy'],
    queryFn: () => getChannelPeriodPolicy(props.channelId),
    refetchOnWindowFocus: false,
    retry: false,
  })
  const targets = useQuery({
    queryKey: ['channels', props.channelId, 'period-targets'],
    queryFn: () => getChannelPeriodTargets(props.channelId),
    refetchOnWindowFocus: false,
    retry: false,
  })
  return (
    <div className='space-y-4'>
      <div className='flex items-center justify-between gap-3'>
        <h3 className='font-medium'>{t('Period policy')}</h3>
        <Button
          variant='outline'
          disabled={writing || query.isFetching || targets.isFetching}
          onClick={async () => {
            const result = await query.refetch()
            await targets.refetch()
            if (result.isSuccess) setReload((value) => value + 1)
          }}
        >
          {t('Reload')}
        </Button>
      </div>
      {(query.isPending || targets.isPending) && (
        <p role='status'>{t('Loading...')}</p>
      )}
      {(query.isError || targets.isError) && (
        <p role='alert' className='text-destructive'>
          {t('Period policy is unavailable. Reload and try again.')}
        </p>
      )}
      {query.data && targets.data && !query.isError && !targets.isError && (
        <ChannelPeriodPolicyForm
          key={`${props.channelId}:${query.data.revision}:${reload}`}
          channelId={props.channelId}
          view={query.data}
          targets={targets.data}
          canOperate={props.canOperate}
        />
      )}
    </div>
  )
}

/** @param props 权威配置和渠道快照。 @returns 版本化策略编辑与确认预览。 */
function ChannelPeriodPolicyForm(props: {
  channelId: number
  view: ChannelPeriodView
  targets: ChannelPeriodTarget[]
  canOperate: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<ChannelPeriodConfig>({
    defaultValues: props.view.config,
    resolver: zodResolver(channelPeriodConfigSchema),
  })
  const rules = useFieldArray({
    control: form.control,
    name: 'rules',
    keyName: 'formKey',
  })
  const config = form.watch()
  const [preview, setPreview] = useState<ChannelPeriodView | null>(null)
  const [previewSnapshot, setPreviewSnapshot] = useState('')
  const [stale, setStale] = useState(false)
  const [error, setError] = useState('')
  const weekdays = [
    t('Monday'),
    t('Tuesday'),
    t('Wednesday'),
    t('Thursday'),
    t('Friday'),
    t('Saturday'),
    t('Sunday'),
  ]
  const labels = [
    t('User daily amount'),
    t('Pool daily amount'),
    t('User whole-period amount'),
    t('Pool whole-period amount'),
  ]
  const metrics: Record<string, string> = {
    user_daily: labels[0],
    pool_daily: labels[1],
    pool_weekly: t('Pool weekly amount'),
    user_custom: labels[2],
    pool_custom: labels[3],
  }
  const models =
    props.targets.find((item) => item.id === config.fallback.channel_id)
      ?.models ?? []
  const mutation = useMutation({
    mutationKey: ['channels', props.channelId, 'period-policy'],
    mutationFn: (input: { config: ChannelPeriodConfig; preview: boolean }) =>
      saveChannelPeriodPolicy(
        props.channelId,
        props.view.revision,
        input.config,
        input.preview
      ),
  })
  const disabled = !props.canOperate || stale || mutation.isPending
  const submit = (previewOnly: boolean) =>
    form.handleSubmit(
      (value) => {
        if (disabled) return
        const snapshot = JSON.stringify(value)
        if (!previewOnly && snapshot !== previewSnapshot) return
        setError('')
        mutation.mutate(
          { config: value, preview: previewOnly },
          {
            onSuccess: (result) => {
              if (previewOnly) {
                setPreview(result)
                setPreviewSnapshot(snapshot)
                return
              }
              toast.success(t('Period policy saved'))
              queryClient.setQueryData(
                ['channels', props.channelId, 'period-policy'],
                result
              )
              void queryClient.invalidateQueries({ queryKey: ['channels'] })
            },
            onError: (reason) => {
              setError(t(channelPeriodErrorKey(reason)))
              if (
                !previewOnly ||
                channelPeriodErrorKey(reason) ===
                  'The policy has changed. Reload before editing again.'
              ) {
                setStale(true)
              }
            },
          }
        )
      },
      () =>
        setError(t('Check rule times, overlapping limits, and quota values.'))
    )
  return (
    <form onSubmit={submit(true)}>
      <FieldGroup>
        <p className='text-sm'>
          {t('Server timezone: {{timezone}}', {
            timezone: props.view.timezone,
          })}
        </p>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Personal overrides, specific dates, weekly schedules, then defaults apply per limit. Other limits still apply.'
          )}
        </p>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Daily resets do not reset whole-period usage. In-flight requests may exceed these soft limits.'
          )}
        </p>
        {error && (
          <p role='alert' className='text-destructive text-sm'>
            {error}
          </p>
        )}
        <FieldSet disabled={disabled} className='space-y-4'>
          <div className='grid gap-3 sm:grid-cols-2'>
            <ChannelPeriodAmount
              label={t('Pool daily amount')}
              value={config.pool_daily_quota_limit}
              onChange={(value) =>
                form.setValue('pool_daily_quota_limit', value ?? 0)
              }
            />
            <ChannelPeriodAmount
              label={t('Pool weekly amount')}
              value={config.pool_weekly_quota_limit}
              onChange={(value) =>
                form.setValue('pool_weekly_quota_limit', value ?? 0)
              }
            />
          </div>
          <p className='text-muted-foreground text-xs'>
            {t('Zero means unlimited. New counters exclude earlier usage.')}
          </p>
          {rules.fields.map((field, index) => {
            const rule = config.rules[index]
            return (
              <section
                key={field.formKey}
                className='space-y-3 rounded-lg border p-4'
                aria-label={t('Time rule {{index}}', { index: index + 1 })}
              >
                <div className='flex items-center justify-between gap-2'>
                  <Field>
                    <FieldLabel className='flex items-center gap-2 text-sm'>
                      <Switch
                        checked={rule.enabled}
                        onCheckedChange={(checked) =>
                          form.setValue(`rules.${index}.enabled`, checked)
                        }
                      />
                      {t('Enabled')}
                    </FieldLabel>
                  </Field>
                  {!rule.id && (
                    <Button
                      type='button'
                      variant='ghost'
                      onClick={() => rules.remove(index)}
                    >
                      {t('Remove')}
                    </Button>
                  )}
                </div>
                <div className='grid gap-3 sm:grid-cols-2'>
                  <Field>
                    <FieldLabel className='grid gap-1 text-sm'>
                      {t('Rule name')}
                      <Input
                        required
                        maxLength={80}
                        {...form.register(`rules.${index}.name`)}
                      />
                    </FieldLabel>
                  </Field>
                  <Field>
                    <FieldLabel className='grid gap-1 text-sm'>
                      {t('Schedule type')}
                      <NativeSelect
                        disabled={!!rule.id}
                        {...form.register(`rules.${index}.kind`)}
                      >
                        <NativeSelectOption value='date_range'>
                          {t('Specific dates')}
                        </NativeSelectOption>
                        <NativeSelectOption value='weekly'>
                          {t('Weekly schedule')}
                        </NativeSelectOption>
                      </NativeSelect>
                    </FieldLabel>
                  </Field>
                  {rule.kind === 'date_range' ? (
                    <>
                      <Field>
                        <FieldLabel className='grid gap-1 text-sm'>
                          {t('Start time')}
                          <Input
                            type='datetime-local'
                            required
                            {...form.register(`rules.${index}.start_local`)}
                          />
                        </FieldLabel>
                      </Field>
                      <Field>
                        <FieldLabel className='grid gap-1 text-sm'>
                          {t('End time')}
                          <Input
                            type='datetime-local'
                            required
                            {...form.register(`rules.${index}.end_local`)}
                          />
                        </FieldLabel>
                      </Field>
                    </>
                  ) : (
                    <>
                      <Field>
                        <FieldLabel className='grid gap-1 text-sm'>
                          {t('Start weekday')}
                          <NativeSelect
                            {...form.register(`rules.${index}.start_weekday`, {
                              valueAsNumber: true,
                            })}
                          >
                            {weekdays.map((day, i) => (
                              <NativeSelectOption key={day} value={i}>
                                {day}
                              </NativeSelectOption>
                            ))}
                          </NativeSelect>
                          <Input
                            aria-label={t('Start time')}
                            type='time'
                            required
                            {...form.register(`rules.${index}.start_time`)}
                          />
                        </FieldLabel>
                      </Field>
                      <Field>
                        <FieldLabel className='grid gap-1 text-sm'>
                          {t('End weekday')}
                          <NativeSelect
                            {...form.register(`rules.${index}.end_weekday`, {
                              valueAsNumber: true,
                            })}
                          >
                            {weekdays.map((day, i) => (
                              <NativeSelectOption key={day} value={i}>
                                {day}
                              </NativeSelectOption>
                            ))}
                          </NativeSelect>
                          <Input
                            aria-label={t('End time')}
                            type='time'
                            required
                            {...form.register(`rules.${index}.end_time`)}
                          />
                        </FieldLabel>
                      </Field>
                    </>
                  )}
                  {periodQuotaKeys.map((key, i) => (
                    <ChannelPeriodAmount
                      key={key}
                      label={labels[i]}
                      value={rule[key]}
                      nullable
                      onChange={(value) =>
                        form.setValue(`rules.${index}.${key}`, value)
                      }
                    />
                  ))}
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Started rules keep their time boundaries. Disabling a rule preserves its usage.'
                  )}
                </p>
              </section>
            )
          })}
          <Button
            type='button'
            variant='outline'
            disabled={rules.fields.length >= 64}
            onClick={() =>
              rules.append({
                id: '',
                name: '',
                enabled: true,
                kind: 'date_range',
                start_local: '',
                end_local: '',
                start_at: 0,
                end_at: 0,
                start_weekday: 0,
                end_weekday: 1,
                start_time: '00:00',
                end_time: '00:00',
                created_at: 0,
                user_daily_quota_limit: null,
                pool_daily_quota_limit: null,
                user_period_quota_limit: null,
                pool_period_quota_limit: null,
              })
            }
          >
            {t('Add time rule')}
          </Button>
          <section className='space-y-3 rounded-lg border p-4'>
            <Field>
              <FieldLabel className='flex items-center gap-2 text-sm'>
                <Switch
                  checked={config.fallback.enabled}
                  onCheckedChange={(checked) =>
                    form.setValue('fallback.enabled', checked)
                  }
                />
                {t('Try one fallback when a quota is exhausted')}
              </FieldLabel>
            </Field>
            {config.fallback.enabled && (
              <div className='grid gap-3 sm:grid-cols-2'>
                <Field>
                  <FieldLabel className='grid gap-1 text-sm'>
                    {t('Target channel')}
                    <NativeSelect
                      value={config.fallback.channel_id}
                      onChange={(event) => {
                        form.setValue(
                          'fallback.channel_id',
                          Number(event.target.value)
                        )
                        form.setValue('fallback.model', '')
                      }}
                    >
                      <NativeSelectOption value={0} disabled>
                        {t('Select channel')}
                      </NativeSelectOption>
                      {config.fallback.channel_id > 0 &&
                        !props.targets.some(
                          (item) => item.id === config.fallback.channel_id
                        ) && (
                          <NativeSelectOption
                            value={config.fallback.channel_id}
                          >
                            #{config.fallback.channel_id} · {t('Unavailable')}
                          </NativeSelectOption>
                        )}
                      {props.targets.map((item) => (
                        <NativeSelectOption key={item.id} value={item.id}>
                          #{item.id} · {item.name}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </FieldLabel>
                </Field>
                <Field>
                  <FieldLabel className='grid gap-1 text-sm'>
                    {t('Target model')}
                    <NativeSelect required {...form.register('fallback.model')}>
                      <NativeSelectOption value='' disabled>
                        {t('Select model')}
                      </NativeSelectOption>
                      {!!config.fallback.model &&
                        !models.includes(config.fallback.model) && (
                          <NativeSelectOption value={config.fallback.model}>
                            {config.fallback.model} · {t('Unavailable')}
                          </NativeSelectOption>
                        )}
                      {models.map((model) => (
                        <NativeSelectOption key={model} value={model}>
                          {model}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </FieldLabel>
                </Field>
              </div>
            )}
            <p className='text-muted-foreground text-xs'>
              {t(
                'Fallback requires a compatible HTTP Chat, Messages, or Responses request, model access, and target quota. New requests return to the source after recovery.'
              )}
            </p>
          </section>
          <Button type='submit' variant='outline'>
            {t('Preview effective limits')}
          </Button>
        </FieldSet>
        {preview && previewSnapshot === JSON.stringify(config) && (
          <section
            className='space-y-2 rounded-lg border p-4'
            aria-label={t('Policy preview')}
          >
            <h4 className='font-medium'>
              {t('Policy preview')} · {t('Version')} {preview.revision}
            </h4>
            {Object.entries(preview.sources ?? {}).map(([key, source]) => (
              <p key={key} className='text-sm'>
                {metrics[key]}:{' '}
                {preview.limits?.[key]
                  ? formatQuota(preview.limits[key])
                  : t('Unlimited')}{' '}
                · <ChannelPeriodSourceLabel source={source} />
              </p>
            ))}
            {!!preview.next_change_at && (
              <p className='text-sm'>
                {t('Next rule change: {{time}}', {
                  time: formatTimestampToDate(preview.next_change_at),
                })}
              </p>
            )}
            <Button
              type='button'
              disabled={disabled}
              onClick={() => void submit(false)()}
            >
              {t('Confirm and save policy')}
            </Button>
          </section>
        )}
      </FieldGroup>
    </form>
  )
}
