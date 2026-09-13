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
import { useId, useRef, useState } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'
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

import {
  channelPeriodErrorKey,
  getChannelPeriodPolicy,
  getChannelPeriodTargets,
  previewChannelPeriodPolicy,
  saveChannelPeriodPolicy,
} from '../../period-api'
import {
  channelPeriodConfigSchema,
  type ChannelBudgetPreview,
  type ChannelBudgetRow,
  type ChannelPeriodConfig,
  type ChannelPeriodTarget,
  type ChannelPeriodView,
} from '../../period-types'
import { ChannelBudgetProgress } from './channel-budget-progress'
import { ChannelBudgetUsagePanel } from './channel-budget-usage'
import { ChannelPeriodAmount } from './channel-period-amount'
import { ChannelPeriodSourceLabel } from './channel-period-metrics'

/** 新时段与新预算行的临时 id 前缀；服务端保存时替换为稳定 id，同一次保存内的行可按临时 id 引用新时段。 */
const NEW_ID_PREFIX = 'new-'

/** @param id 时段或预算行 id。 @returns 是否为尚未保存的临时身份。 */
function isNewId(id: string): boolean {
  return id === '' || id.startsWith(NEW_ID_PREFIX)
}

/** @param props 渠道 ID 和操作权限。 @returns 独立查询的预算策略面板。 */
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

/** @param props 权威配置、降级候选与权限。 @returns 版本化的预算表编辑与确认预览。 */
function ChannelPeriodPolicyForm(props: {
  channelId: number
  view: ChannelPeriodView
  targets: ChannelPeriodTarget[]
  canOperate: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const formId = useId()
  const form = useForm<ChannelPeriodConfig>({
    defaultValues: props.view.config,
    resolver: zodResolver(channelPeriodConfigSchema),
  })
  const schedules = useFieldArray({
    control: form.control,
    name: 'schedules',
    keyName: 'formKey',
  })
  const budgets = useFieldArray({
    control: form.control,
    name: 'budgets',
    keyName: 'formKey',
  })
  const config = form.watch()
  const nextId = useRef(0)
  const [editing, setEditing] = useState<number | null>(null)
  const [editingModels, setEditingModels] = useState('')
  const [usageRow, setUsageRow] = useState<ChannelBudgetRow | null>(null)
  const [preview, setPreview] = useState<ChannelBudgetPreview | null>(null)
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
  const scopeLabels = { user: t('Per user'), pool: t('Pool') }
  const windowLabels = {
    daily: t('Daily'),
    weekly: t('Weekly'),
    occurrence: t('Whole period'),
  }
  const modeLabels = {
    inherit: t('Use policy default'),
    reject: t('Reject'),
    fallback: t('Fallback'),
  }
  const selfTarget = props.targets.find((item) => item.self)
  const otherTargets = props.targets.filter((item) => !item.self)
  const mutationKey = ['channels', props.channelId, 'period-policy']
  const previewMutation = useMutation({
    mutationKey,
    mutationFn: (input: ChannelPeriodConfig) =>
      previewChannelPeriodPolicy(props.channelId, props.view.revision, input),
  })
  const saveMutation = useMutation({
    mutationKey,
    mutationFn: (input: ChannelPeriodConfig) =>
      saveChannelPeriodPolicy(props.channelId, props.view.revision, input),
  })
  const disabled =
    !props.canOperate ||
    stale ||
    previewMutation.isPending ||
    saveMutation.isPending
  const fail = (reason: unknown, lock: boolean) => {
    const key = channelPeriodErrorKey(reason)
    setError(t(key))
    if (
      lock ||
      key === 'The policy has changed. Reload before editing again.'
    ) {
      setStale(true)
    }
  }
  const runPreview = form.handleSubmit(
    (value) => {
      if (disabled) return
      setError('')
      previewMutation.mutate(value, {
        onSuccess: (result) => {
          setPreview(result)
          setPreviewSnapshot(JSON.stringify(value))
        },
        onError: (reason) => fail(reason, false),
      })
    },
    () => setError(t('Check schedules, overlapping budgets, and quota values.'))
  )
  const runSave = form.handleSubmit((value) => {
    // 只允许保存与预览完全一致的草稿，避免预览后继续修改却按旧预览确认。
    if (disabled || JSON.stringify(value) !== previewSnapshot) return
    setError('')
    saveMutation.mutate(value, {
      onSuccess: (result) => {
        toast.success(t('Period policy saved'))
        queryClient.setQueryData(mutationKey, result)
        void queryClient.invalidateQueries({ queryKey: ['channels'] })
      },
      onError: (reason) => fail(reason, true),
    })
  })
  /** @param channelId 行级降级目标；0 表示同渠道换模型。 @returns 目标渠道的可选模型。 */
  const targetModels = (channelId: number) =>
    props.targets.find(
      (item) => item.id === channelId || (channelId === 0 && item.self)
    )?.models ?? []
  /** @param row 预算行。 @returns 表格中的超限动作摘要。 */
  const actionSummary = (row: ChannelBudgetRow) => {
    if (row.on_exceed.mode !== 'fallback') return modeLabels[row.on_exceed.mode]
    const target =
      row.on_exceed.channel_id === 0
        ? t('This channel')
        : `#${row.on_exceed.channel_id}`
    return `${modeLabels.fallback} · ${target} · ${row.on_exceed.model}`
  }
  // Zod 会调整可选字段的键顺序；比较同样归一化的草稿，避免新开关使有效预览消失。
  const previewMatchesDraft =
    previewSnapshot ===
    JSON.stringify(channelPeriodConfigSchema.safeParse(config).data)
  const editingRow = editing === null ? null : config.budgets[editing]
  return (
    <form id={formId} onSubmit={runPreview}>
      <FieldGroup>
        <p className='text-sm'>
          {t('Server timezone: {{timezone}}', {
            timezone: props.view.timezone,
          })}
        </p>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Each budget limits one scope, window, and model set. Personal overrides, specific dates, weekly schedules, then default rows apply within a group; other groups still apply.'
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
          <Field orientation='horizontal' data-disabled={disabled}>
            <FieldContent>
              <FieldLabel htmlFor={`${formId}-model-tracking`}>
                {t('Continuously track model usage')}
              </FieldLabel>
              <FieldDescription>
                {t(
                  'Track daily and weekly usage for each user and the pool, even without model budgets. Uses additional storage. Disabling keeps existing budgets counting; untracked history is not backfilled.'
                )}
              </FieldDescription>
            </FieldContent>
            <Switch
              id={`${formId}-model-tracking`}
              aria-label={t('Continuously track model usage')}
              checked={config.model_usage_tracking_enabled ?? false}
              disabled={disabled}
              onCheckedChange={(checked) =>
                form.setValue('model_usage_tracking_enabled', checked, {
                  shouldDirty: true,
                })
              }
            />
          </Field>

          <section className='space-y-3' aria-label={t('Schedules')}>
            <h4 className='font-medium'>{t('Schedules')}</h4>
            {schedules.fields.map((field, index) => {
              const schedule = config.schedules[index]
              return (
                <section
                  key={field.formKey}
                  className='space-y-3 rounded-lg border p-4'
                  aria-label={t('Schedule {{index}}', { index: index + 1 })}
                >
                  <div className='flex items-center justify-between gap-2'>
                    <Field>
                      <FieldLabel className='flex items-center gap-2 text-sm'>
                        <Switch
                          checked={schedule.enabled}
                          onCheckedChange={(checked) =>
                            form.setValue(`schedules.${index}.enabled`, checked)
                          }
                        />
                        {t('Enabled')}
                      </FieldLabel>
                    </Field>
                    {isNewId(schedule.id) && (
                      <Button
                        type='button'
                        variant='ghost'
                        onClick={() => schedules.remove(index)}
                      >
                        {t('Remove')}
                      </Button>
                    )}
                  </div>
                  <div className='grid gap-3 sm:grid-cols-2'>
                    <Field>
                      <FieldLabel className='grid gap-1 text-sm'>
                        {t('Schedule name')}
                        <Input
                          required
                          maxLength={80}
                          {...form.register(`schedules.${index}.name`)}
                        />
                      </FieldLabel>
                    </Field>
                    <Field>
                      <FieldLabel className='grid gap-1 text-sm'>
                        {t('Schedule type')}
                        <NativeSelect
                          disabled={!isNewId(schedule.id)}
                          {...form.register(`schedules.${index}.kind`)}
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
                    {schedule.kind === 'date_range' ? (
                      <>
                        <Field>
                          <FieldLabel className='grid gap-1 text-sm'>
                            {t('Start time')}
                            <Input
                              type='datetime-local'
                              required
                              {...form.register(
                                `schedules.${index}.start_local`
                              )}
                            />
                          </FieldLabel>
                        </Field>
                        <Field>
                          <FieldLabel className='grid gap-1 text-sm'>
                            {t('End time')}
                            <Input
                              type='datetime-local'
                              required
                              {...form.register(`schedules.${index}.end_local`)}
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
                              {...form.register(
                                `schedules.${index}.start_weekday`,
                                { valueAsNumber: true }
                              )}
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
                              {...form.register(
                                `schedules.${index}.start_time`
                              )}
                            />
                          </FieldLabel>
                        </Field>
                        <Field>
                          <FieldLabel className='grid gap-1 text-sm'>
                            {t('End weekday')}
                            <NativeSelect
                              {...form.register(
                                `schedules.${index}.end_weekday`,
                                { valueAsNumber: true }
                              )}
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
                              {...form.register(`schedules.${index}.end_time`)}
                            />
                          </FieldLabel>
                        </Field>
                      </>
                    )}
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Started schedules keep their time boundaries. Disabling a schedule preserves its usage.'
                    )}
                  </p>
                </section>
              )
            })}
            <Button
              type='button'
              variant='outline'
              disabled={schedules.fields.length >= 64}
              onClick={() =>
                schedules.append({
                  id: `${NEW_ID_PREFIX}schedule-${++nextId.current}`,
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
                })
              }
            >
              {t('Add schedule')}
            </Button>
          </section>
          <section className='space-y-3' aria-label={t('Budgets')}>
            <h4 className='font-medium'>{t('Budgets')}</h4>
            {budgets.fields.length === 0 ? (
              <p className='text-muted-foreground text-sm'>
                {t('No budgets configured.')}
              </p>
            ) : (
              <div className='overflow-x-auto'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Enabled')}</TableHead>
                      <TableHead>{t('Name')}</TableHead>
                      <TableHead>{t('Scope')}</TableHead>
                      <TableHead>{t('Window')}</TableHead>
                      <TableHead>{t('Schedule')}</TableHead>
                      <TableHead>{t('Models')}</TableHead>
                      <TableHead>{t('Limit')}</TableHead>
                      <TableHead>
                        {t('Used')} / {t('Remaining')}
                      </TableHead>
                      <TableHead>{t('When exceeded')}</TableHead>
                      <TableHead className='text-right'>
                        {t('Actions')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {budgets.fields.map((field, index) => {
                      const row = config.budgets[index]
                      const schedule = config.schedules.find(
                        (item) => item.id === row.schedule_id
                      )
                      return (
                        <TableRow
                          key={field.formKey}
                          data-state={
                            editing === index ? 'selected' : undefined
                          }
                        >
                          <TableCell>
                            <Switch
                              aria-label={t('Enabled')}
                              checked={row.enabled}
                              onCheckedChange={(checked) =>
                                form.setValue(
                                  `budgets.${index}.enabled`,
                                  checked
                                )
                              }
                            />
                          </TableCell>
                          <TableCell className='font-medium'>
                            {row.name || t('Untitled budget')}
                          </TableCell>
                          <TableCell>{scopeLabels[row.scope]}</TableCell>
                          <TableCell>{windowLabels[row.window]}</TableCell>
                          <TableCell>
                            {schedule?.name || t('No schedule')}
                          </TableCell>
                          <TableCell>
                            {row.models.length > 0
                              ? row.models.join(', ')
                              : t('All models')}
                          </TableCell>
                          <TableCell>
                            {row.limit > 0
                              ? formatQuota(row.limit)
                              : t('Unlimited')}
                          </TableCell>
                          <TableCell>
                            {isNewId(row.id) ? (
                              t('Save to view usage')
                            ) : (
                              <ChannelBudgetProgress
                                channelId={props.channelId}
                                row={row}
                              />
                            )}
                          </TableCell>
                          <TableCell>{actionSummary(row)}</TableCell>
                          <TableCell className='text-right'>
                            <div className='flex justify-end gap-2'>
                              <Button
                                type='button'
                                variant='outline'
                                size='sm'
                                onClick={() => {
                                  setEditingModels(row.models.join(', '))
                                  setEditing(editing === index ? null : index)
                                }}
                              >
                                {editing === index ? t('Done') : t('Edit')}
                              </Button>
                              {!isNewId(row.id) && (
                                <Button
                                  type='button'
                                  variant='outline'
                                  size='sm'
                                  onClick={() => setUsageRow(row)}
                                >
                                  {t('Usage')}
                                </Button>
                              )}
                              {isNewId(row.id) && (
                                <Button
                                  type='button'
                                  variant='ghost'
                                  size='sm'
                                  onClick={() => {
                                    setEditing(null)
                                    budgets.remove(index)
                                  }}
                                >
                                  {t('Remove')}
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
            {editing !== null && editingRow && (
              <Sheet
                open
                onOpenChange={(open) => {
                  if (!open) setEditing(null)
                }}
              >
                <SheetContent
                  className='w-full sm:max-w-2xl'
                  showCloseButton={false}
                  aria-label={t('Budget editor')}
                >
                  <SheetHeader>
                    <SheetTitle>{t('Budget editor')}</SheetTitle>
                    <SheetDescription>
                      {t(
                        'Close to keep your draft, then preview and save the policy.'
                      )}
                    </SheetDescription>
                  </SheetHeader>
                  <FieldSet
                    key={budgets.fields[editing]?.formKey}
                    disabled={disabled}
                    className='visible-scrollbar min-h-0 flex-1 space-y-3 overflow-y-auto px-4 pb-4'
                  >
                    <div className='grid gap-3 sm:grid-cols-2'>
                      <Field>
                        <FieldLabel className='grid gap-1 text-sm'>
                          {t('Budget name')}
                          <Input
                            form={formId}
                            required
                            maxLength={80}
                            {...form.register(`budgets.${editing}.name`)}
                          />
                        </FieldLabel>
                      </Field>
                      <Field>
                        <FieldLabel className='grid gap-1 text-sm'>
                          {t('Scope')}
                          <NativeSelect
                            form={formId}
                            {...form.register(`budgets.${editing}.scope`)}
                          >
                            <NativeSelectOption value='user'>
                              {scopeLabels.user}
                            </NativeSelectOption>
                            <NativeSelectOption value='pool'>
                              {scopeLabels.pool}
                            </NativeSelectOption>
                          </NativeSelect>
                        </FieldLabel>
                      </Field>
                      <Field>
                        <FieldLabel className='grid gap-1 text-sm'>
                          {t('Window')}
                          <NativeSelect
                            form={formId}
                            {...form.register(`budgets.${editing}.window`)}
                          >
                            <NativeSelectOption value='daily'>
                              {windowLabels.daily}
                            </NativeSelectOption>
                            <NativeSelectOption value='weekly'>
                              {windowLabels.weekly}
                            </NativeSelectOption>
                            <NativeSelectOption value='occurrence'>
                              {windowLabels.occurrence}
                            </NativeSelectOption>
                          </NativeSelect>
                        </FieldLabel>
                      </Field>
                      <Field>
                        <FieldLabel className='grid gap-1 text-sm'>
                          {t('Schedule')}
                          <NativeSelect
                            form={formId}
                            required={editingRow.window === 'occurrence'}
                            {...form.register(`budgets.${editing}.schedule_id`)}
                          >
                            <NativeSelectOption
                              value=''
                              disabled={editingRow.window === 'occurrence'}
                            >
                              {editingRow.window === 'occurrence'
                                ? t('Select a schedule')
                                : t('No schedule')}
                            </NativeSelectOption>
                            {config.schedules.map((item) => (
                              <NativeSelectOption key={item.id} value={item.id}>
                                {item.name || t('Untitled schedule')}
                              </NativeSelectOption>
                            ))}
                          </NativeSelect>
                        </FieldLabel>
                      </Field>
                      <Field>
                        <FieldLabel className='grid gap-1 text-sm'>
                          {t('Models')}
                          <Input
                            form={formId}
                            value={editingModels}
                            placeholder={t('Blank applies to all models')}
                            onChange={(event) => {
                              setEditingModels(event.target.value)
                              form.setValue(
                                `budgets.${editing}.models`,
                                event.target.value
                                  .split(/[,\n]/)
                                  .map((item) => item.trim())
                                  .filter(Boolean)
                              )
                            }}
                          />
                        </FieldLabel>
                      </Field>
                      <ChannelPeriodAmount
                        label={t('Limit')}
                        value={editingRow.limit}
                        onChange={(value) =>
                          form.setValue(`budgets.${editing}.limit`, value ?? 0)
                        }
                      />
                      <Field>
                        <FieldLabel className='grid gap-1 text-sm'>
                          {t('When exceeded')}
                          <NativeSelect
                            form={formId}
                            {...form.register(
                              `budgets.${editing}.on_exceed.mode`
                            )}
                          >
                            <NativeSelectOption value='inherit'>
                              {modeLabels.inherit}
                            </NativeSelectOption>
                            <NativeSelectOption value='reject'>
                              {modeLabels.reject}
                            </NativeSelectOption>
                            <NativeSelectOption value='fallback'>
                              {modeLabels.fallback}
                            </NativeSelectOption>
                          </NativeSelect>
                        </FieldLabel>
                      </Field>
                      {editingRow.on_exceed.mode === 'fallback' && (
                        <>
                          <Field>
                            <FieldLabel className='grid gap-1 text-sm'>
                              {t('Target channel')}
                              <NativeSelect
                                form={formId}
                                value={
                                  editingRow.on_exceed.channel_id === 0
                                    ? (selfTarget?.id ?? 0)
                                    : editingRow.on_exceed.channel_id
                                }
                                onChange={(event) => {
                                  form.setValue(
                                    `budgets.${editing}.on_exceed.channel_id`,
                                    Number(event.target.value)
                                  )
                                  form.setValue(
                                    `budgets.${editing}.on_exceed.model`,
                                    ''
                                  )
                                }}
                              >
                                <NativeSelectOption value={0} disabled>
                                  {t('Select channel')}
                                </NativeSelectOption>
                                {selfTarget && editingRow.models.length > 0 && (
                                  <NativeSelectOption value={selfTarget.id}>
                                    #{selfTarget.id} · {selfTarget.name} ·{' '}
                                    {t('This channel')}
                                  </NativeSelectOption>
                                )}
                                {editingRow.on_exceed.channel_id > 0 &&
                                  !props.targets.some(
                                    (item) =>
                                      item.id ===
                                      editingRow.on_exceed.channel_id
                                  ) && (
                                    <NativeSelectOption
                                      value={editingRow.on_exceed.channel_id}
                                    >
                                      #{editingRow.on_exceed.channel_id} ·{' '}
                                      {t('Unavailable')}
                                    </NativeSelectOption>
                                  )}
                                {otherTargets.map((item) => (
                                  <NativeSelectOption
                                    key={item.id}
                                    value={item.id}
                                  >
                                    #{item.id} · {item.name}
                                  </NativeSelectOption>
                                ))}
                              </NativeSelect>
                            </FieldLabel>
                          </Field>
                          <Field>
                            <FieldLabel className='grid gap-1 text-sm'>
                              {t('Target model')}
                              <NativeSelect
                                form={formId}
                                required
                                {...form.register(
                                  `budgets.${editing}.on_exceed.model`
                                )}
                              >
                                <NativeSelectOption value='' disabled>
                                  {t('Select model')}
                                </NativeSelectOption>
                                {!!editingRow.on_exceed.model &&
                                  !targetModels(
                                    editingRow.on_exceed.channel_id
                                  ).includes(editingRow.on_exceed.model) && (
                                    <NativeSelectOption
                                      value={editingRow.on_exceed.model}
                                    >
                                      {editingRow.on_exceed.model} ·{' '}
                                      {t('Unavailable')}
                                    </NativeSelectOption>
                                  )}
                                {targetModels(editingRow.on_exceed.channel_id)
                                  .filter(
                                    (model) =>
                                      editingRow.on_exceed.channel_id !== 0 ||
                                      !editingRow.models.includes(model)
                                  )
                                  .map((model) => (
                                    <NativeSelectOption
                                      key={model}
                                      value={model}
                                    >
                                      {model}
                                    </NativeSelectOption>
                                  ))}
                              </NativeSelect>
                            </FieldLabel>
                          </Field>
                        </>
                      )}
                    </div>
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'Zero means unlimited. Changing models uses the selected model history when available; renaming or changing the limit keeps usage.'
                      )}
                    </p>
                    <Button
                      type='button'
                      variant='outline'
                      onClick={() => setEditing(null)}
                    >
                      {t('Done')}
                    </Button>
                  </FieldSet>
                </SheetContent>
              </Sheet>
            )}
            <Button
              type='button'
              variant='outline'
              disabled={budgets.fields.length >= 128}
              onClick={() => {
                budgets.append({
                  id: `${NEW_ID_PREFIX}budget-${++nextId.current}`,
                  name: '',
                  enabled: true,
                  scope: 'pool',
                  window: 'daily',
                  schedule_id: '',
                  models: [],
                  limit: 0,
                  on_exceed: { mode: 'inherit', channel_id: 0, model: '' },
                  created_at: 0,
                })
                setEditingModels('')
                setEditing(budgets.fields.length)
              }}
            >
              {t('Add budget')}
            </Button>
          </section>
          <section
            className='space-y-3 rounded-lg border p-4'
            aria-label={t('Default action when exceeded')}
          >
            <h4 className='font-medium'>{t('Default action when exceeded')}</h4>
            <div className='grid gap-3 sm:grid-cols-3'>
              <Field>
                <FieldLabel className='grid gap-1 text-sm'>
                  {t('Action')}
                  <NativeSelect {...form.register('default_on_exceed.mode')}>
                    <NativeSelectOption value='reject'>
                      {modeLabels.reject}
                    </NativeSelectOption>
                    <NativeSelectOption value='fallback'>
                      {modeLabels.fallback}
                    </NativeSelectOption>
                  </NativeSelect>
                </FieldLabel>
              </Field>
              {config.default_on_exceed.mode === 'fallback' && (
                <>
                  <Field>
                    <FieldLabel className='grid gap-1 text-sm'>
                      {t('Target channel')}
                      <NativeSelect
                        value={config.default_on_exceed.channel_id}
                        onChange={(event) => {
                          form.setValue(
                            'default_on_exceed.channel_id',
                            Number(event.target.value)
                          )
                          form.setValue('default_on_exceed.model', '')
                        }}
                      >
                        <NativeSelectOption value={0} disabled>
                          {t('Select channel')}
                        </NativeSelectOption>
                        {config.default_on_exceed.channel_id > 0 &&
                          !otherTargets.some(
                            (item) =>
                              item.id === config.default_on_exceed.channel_id
                          ) && (
                            <NativeSelectOption
                              value={config.default_on_exceed.channel_id}
                            >
                              #{config.default_on_exceed.channel_id} ·{' '}
                              {t('Unavailable')}
                            </NativeSelectOption>
                          )}
                        {otherTargets.map((item) => (
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
                      <NativeSelect
                        required
                        {...form.register('default_on_exceed.model')}
                      >
                        <NativeSelectOption value='' disabled>
                          {t('Select model')}
                        </NativeSelectOption>
                        {!!config.default_on_exceed.model &&
                          !targetModels(
                            config.default_on_exceed.channel_id
                          ).includes(config.default_on_exceed.model) && (
                            <NativeSelectOption
                              value={config.default_on_exceed.model}
                            >
                              {config.default_on_exceed.model} ·{' '}
                              {t('Unavailable')}
                            </NativeSelectOption>
                          )}
                        {targetModels(config.default_on_exceed.channel_id).map(
                          (model) => (
                            <NativeSelectOption key={model} value={model}>
                              {model}
                            </NativeSelectOption>
                          )
                        )}
                      </NativeSelect>
                    </FieldLabel>
                  </Field>
                </>
              )}
            </div>
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
        {usageRow && (
          <ChannelBudgetUsagePanel
            key={usageRow.id}
            channelId={props.channelId}
            row={usageRow}
            canOperate={props.canOperate}
            onClose={() => setUsageRow(null)}
          />
        )}
        {preview && previewMatchesDraft && (
          <section
            className='space-y-2 rounded-lg border p-4'
            aria-label={t('Policy preview')}
          >
            <h4 className='font-medium'>
              {t('Policy preview')} · {t('Version')} {preview.revision}
            </h4>
            {preview.rows.map((row) => {
              const budget = preview.config.budgets.find(
                (item) => item.id === row.budget_id
              )
              return (
                <p key={row.budget_id} className='text-sm'>
                  {budget?.name ?? row.budget_id}:{' '}
                  {budget && budget.limit > 0
                    ? formatQuota(budget.limit)
                    : t('Unlimited')}{' '}
                  · {row.active ? t('Active now') : t('Inactive now')} ·{' '}
                  {row.enforced
                    ? t('Enforced')
                    : t('Overridden by a higher-priority schedule')}{' '}
                  · <ChannelPeriodSourceLabel source={row.source} />
                </p>
              )
            })}
            {!!preview.next_change_at && (
              <p className='text-sm'>
                {t('Next schedule change: {{time}}', {
                  time: formatTimestampToDate(preview.next_change_at),
                })}
              </p>
            )}
            <Button
              type='button'
              disabled={disabled}
              onClick={() => void runSave()}
            >
              {t('Confirm and save policy')}
            </Button>
          </section>
        )}
      </FieldGroup>
    </form>
  )
}
