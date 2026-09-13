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
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { formatQuota } from '@/lib/format'

import {
  channelPolicyErrorDetail,
  matchChannelPolicyErrorRows,
} from '../../lib/channel-policy-error-rows'
import { resolveChannelScheduleStates } from '../../lib/channel-schedule-state'
import {
  channelPeriodErrorKey,
  channelPeriodErrorMessage,
  getChannelBudgetUsageSummary,
  getChannelPeriodPolicy,
  getChannelPeriodTargets,
  previewChannelPeriodPolicy,
  saveChannelPeriodPolicy,
} from '../../period-api'
import {
  channelPeriodConfigSchema,
  type ChannelBudgetAction,
  type ChannelBudgetPreview,
  type ChannelBudgetPreviewRow,
  type ChannelBudgetRow,
  type ChannelBudgetUsageSummaryView,
  type ChannelPeriodConfig,
  type ChannelPeriodTarget,
  type ChannelPeriodView,
} from '../../period-types'
import { BudgetEditorSheet } from './budget/budget-editor-sheet'
import { BudgetOverrideSheet } from './budget/budget-override-sheet'
import {
  computeBudgetRowStates,
  NEW_ID_PREFIX,
  type BudgetFilter,
} from './budget/budget-row-state'
import { BudgetTable } from './budget/budget-table'
import { BudgetUsageSheet } from './budget/budget-usage-sheet'
import { diffChannelPeriodConfig } from './budget/policy-diff'
import { PolicyPreviewDialog } from './budget/policy-preview-dialog'
import { PolicySaveBar } from './budget/policy-save-bar'
import { ScheduleList } from './budget/schedule-list'

/** 面板当前展示的区块：预算表或时段列表；两者共用同一份草稿与保存栏。 */
export type ChannelPeriodPolicySection = 'budgets' | 'schedules'

/** @param props 渠道 ID、展示区块、操作权限与脏草稿回调。 @returns 独立查询的预算策略面板。 */
export function ChannelPeriodPolicyPanel(props: {
  channelId: number
  section?: ChannelPeriodPolicySection
  canOperate: boolean
  onDirtyChange?: (dirty: boolean) => void
}) {
  const { t } = useTranslation()
  const writing =
    useIsMutating({
      mutationKey: ['channels', props.channelId, 'period-policy'],
    }) > 0
  const [reload, setReload] = useState(0)
  const [dirty, setDirty] = useState(false)
  const [confirmReload, setConfirmReload] = useState(false)
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
  // 全部行的用量一次拉取，不随行数增长；预览权威配置只为取得时段是否活跃与下一切换点。
  const summary = useQuery({
    queryKey: ['channels', props.channelId, 'budget-usage-summary'],
    queryFn: () => getChannelBudgetUsageSummary(props.channelId),
    refetchOnWindowFocus: false,
    retry: false,
  })
  const revision = query.data?.revision
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
        query.data?.revision ?? 0,
        query.data?.config as ChannelPeriodConfig
      ),
    enabled: !!query.data,
    refetchOnWindowFocus: false,
    retry: false,
  })
  let summaryState: 'loading' | 'error' | 'ready' = 'ready'
  if (summary.isPending) summaryState = 'loading'
  else if (summary.isError) summaryState = 'error'
  const refetchAll = async () => {
    const result = await query.refetch()
    await Promise.all([targets.refetch(), summary.refetch()])
    if (result.isSuccess) setReload((value) => value + 1)
  }
  const onDirtyChange = props.onDirtyChange
  useEffect(() => {
    onDirtyChange?.(dirty)
  }, [dirty, onDirtyChange])
  return (
    <div className='space-y-4'>
      <div className='flex items-center justify-between gap-3'>
        <div>
          <h3 className='font-medium'>{t('Period policy')}</h3>
          <p className='text-muted-foreground text-xs'>
            {query.data &&
              `${t('Server timezone: {{timezone}}', { timezone: query.data.timezone })} · ${t('Version')} ${query.data.revision}`}
          </p>
        </div>
        <Button
          variant='outline'
          disabled={writing || query.isFetching || targets.isFetching}
          onClick={() => {
            if (dirty) setConfirmReload(true)
            else void refetchAll()
          }}
        >
          {t('Reload')}
        </Button>
      </div>
      <ConfirmDialog
        open={confirmReload}
        onOpenChange={setConfirmReload}
        title={t('Discard unsaved changes?')}
        desc={t('Reloading drops your draft and shows the saved policy.')}
        confirmText={t('Discard')}
        destructive
        handleConfirm={() => {
          setConfirmReload(false)
          void refetchAll()
        }}
      />
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
          section={props.section ?? 'budgets'}
          view={query.data}
          targets={Array.isArray(targets.data) ? targets.data : []}
          summary={summary.data}
          summaryState={summaryState}
          onRetrySummary={() => void summary.refetch()}
          previewRows={effective.data?.rows ?? []}
          nextChangeAt={effective.data?.next_change_at ?? 0}
          canOperate={props.canOperate}
          onDirtyChange={setDirty}
        />
      )}
    </div>
  )
}

/** @param props 权威配置、展示区块、降级候选、聚合用量与权限。 @returns 预算表或时段列表、常驻保存栏与各类抽屉。 */
function ChannelPeriodPolicyForm(props: {
  channelId: number
  section: ChannelPeriodPolicySection
  view: ChannelPeriodView
  targets: ChannelPeriodTarget[]
  summary?: ChannelBudgetUsageSummaryView
  summaryState: 'loading' | 'error' | 'ready'
  onRetrySummary: () => void
  previewRows: ChannelBudgetPreviewRow[]
  nextChangeAt: number
  canOperate: boolean
  onDirtyChange: (dirty: boolean) => void
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
  const [usageRow, setUsageRow] = useState<ChannelBudgetRow | null>(null)
  const [overrideRow, setOverrideRow] = useState<ChannelBudgetRow | null>(null)
  const [filter, setFilter] = useState<BudgetFilter>('all')
  const [preview, setPreview] = useState<ChannelBudgetPreview | null>(null)
  const [previewSnapshot, setPreviewSnapshot] = useState('')
  const [previewOpen, setPreviewOpen] = useState(false)
  const [confirmDiscard, setConfirmDiscard] = useState(false)
  const [stale, setStale] = useState(false)
  const [error, setError] = useState('')
  const [errorRows, setErrorRows] = useState<string[]>([])
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
  const busy = previewMutation.isPending || saveMutation.isPending
  const disabled = !props.canOperate || stale || busy
  /** @param action 超限动作。 @returns 用于表格与 diff 的动作摘要。 */
  const actionText = (action: ChannelBudgetAction) => {
    if (action.mode !== 'fallback') return modeLabels[action.mode]
    const target =
      action.channel_id === 0 || action.channel_id === selfTarget?.id
        ? t('This channel')
        : (props.targets.find((item) => item.id === action.channel_id)?.name ??
          `#${action.channel_id}`)
    return `${modeLabels.fallback} → ${target} · ${action.model}`
  }
  const changes = useMemo(
    () =>
      diffChannelPeriodConfig(props.view.config, config, {
        scope: (value) => scopeLabels[value],
        window: (value) => windowLabels[value],
        schedule: (id) =>
          config.schedules.find((item) => item.id === id)?.name ||
          props.view.config.schedules.find((item) => item.id === id)?.name ||
          (id ? id : t('No schedule')),
        action: actionText,
        amount: (value) => (value > 0 ? formatQuota(value) : t('Unlimited')),
        models: (value) => (value.length ? value.join(', ') : t('All models')),
        enabled: (value) => (value ? t('Enabled') : t('Disabled')),
        kind: (value) =>
          value === 'date_range' ? t('Specific dates') : t('Weekly schedule'),
        field: (name) =>
          ({
            default_on_exceed: t('Default action when exceeded'),
            model_usage_tracking_enabled: t('Continuously track model usage'),
            schedule: t('Schedule'),
            budget: t('Budget'),
            name: t('Name'),
            enabled: t('Enabled'),
            kind: t('Schedule type'),
            range: t('Time range'),
            scope: t('Scope'),
            window: t('Window'),
            schedule_id: t('Schedule'),
            models: t('Models'),
            limit: t('Limit'),
            on_exceed: t('When exceeded'),
          })[name] ?? name,
        added: t('Added'),
        removed: t('Removed'),
        policy: t('Policy'),
        untitled: t('Untitled budget'),
      }),
    // 草稿对象每次 watch 都是新引用；按序列化内容缓存即可。
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [JSON.stringify(config), props.view.config, props.targets]
  )
  const dirty = changes.length > 0
  const onDirtyChange = props.onDirtyChange
  useEffect(() => {
    onDirtyChange(dirty)
  }, [dirty, onDirtyChange])
  const summaries = useMemo(
    () =>
      new Map(
        (props.summary?.items ?? []).map((item) => [item.budget_id, item])
      ),
    [props.summary]
  )
  // 时段数组每次 watch 都是新引用，按序列化内容缓存。
  const schedulesKey = JSON.stringify(config.schedules)
  const scheduleStates = useMemo(
    () =>
      resolveChannelScheduleStates(
        config.schedules,
        props.previewRows,
        props.view.now,
        props.view.timezone
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [schedulesKey, props.previewRows, props.view.now, props.view.timezone]
  )
  const states = computeBudgetRowStates(
    config,
    props.view.config,
    summaries,
    scheduleStates
  )
  const errorDetail = channelPolicyErrorDetail(error)
  /** @param reason 请求错误。 @param lock 是否锁定草稿。 @param rows 提交时的行，用于按后端点名的行名定位出错行。 */
  const fail = (reason: unknown, lock: boolean, rows: ChannelBudgetRow[]) => {
    const key = channelPeriodErrorKey(reason)
    const message = channelPeriodErrorMessage(reason)
    setError(message ? `${t(key)} ${message}` : t(key))
    setErrorRows(matchChannelPolicyErrorRows(message, rows))
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
      setErrorRows([])
      previewMutation.mutate(value, {
        onSuccess: (result) => {
          setPreview(result)
          setPreviewSnapshot(JSON.stringify(value))
          setPreviewOpen(true)
        },
        onError: (reason) => fail(reason, false, value.budgets),
      })
    },
    () => setError(t('Check schedules, overlapping budgets, and quota values.'))
  )
  const runSave = form.handleSubmit((value) => {
    // 只允许保存与预览完全一致的草稿，避免预览后继续修改却按旧预览确认。
    if (disabled || JSON.stringify(value) !== previewSnapshot) return
    setError('')
    setErrorRows([])
    saveMutation.mutate(value, {
      onSuccess: (result) => {
        toast.success(t('Period policy saved'))
        setPreviewOpen(false)
        queryClient.setQueryData(mutationKey, result)
        void queryClient.invalidateQueries({ queryKey: ['channels'] })
      },
      onError: (reason) => {
        setPreviewOpen(false)
        fail(reason, true, value.budgets)
      },
    })
  })
  const previewMatchesDraft =
    previewSnapshot ===
    JSON.stringify(channelPeriodConfigSchema.safeParse(config).data)
  const disablesRows = config.budgets.some(
    (row) =>
      !row.enabled &&
      props.view.config.budgets.some(
        (item) => item.id === row.id && item.enabled
      )
  )
  const targetModels = (channelId: number) =>
    props.targets.find(
      (item) => item.id === channelId || (channelId === 0 && item.self)
    )?.models ?? []
  const addBudget = () => {
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
    setEditing(budgets.fields.length)
  }
  return (
    <form id={formId} onSubmit={runPreview} className='space-y-5'>
      {error && (
        <p role='alert' className='text-destructive text-sm'>
          {error}
        </p>
      )}
      <fieldset disabled={disabled} className='space-y-5'>
        {/* 两个区块常驻挂载、按页签切换显隐，草稿、筛选与抽屉状态在切换页签时不丢失。 */}
        <div hidden={props.section !== 'budgets'} className='space-y-5'>
          <div className='flex items-start justify-between gap-3 rounded-lg border px-4 py-3'>
            <div>
              <label
                htmlFor={`${formId}-model-tracking`}
                className='text-sm font-medium'
              >
                {t('Continuously track model usage')}
              </label>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Track daily and weekly usage for each user and the pool, even without model budgets. Uses additional storage. Disabling keeps existing budgets counting; untracked history is not backfilled.'
                )}
              </p>
            </div>
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
          </div>
          <BudgetTable
            rows={config.budgets}
            states={states}
            schedules={config.schedules}
            scheduleStates={scheduleStates}
            errorRows={errorRows}
            errorDetail={errorDetail}
            filter={filter}
            onFilterChange={setFilter}
            nextChangeAt={props.nextChangeAt}
            editingIndex={editing}
            disabled={disabled}
            summaryState={props.summaryState}
            onRetrySummary={props.onRetrySummary}
            actionSummary={(row) => actionText(row.on_exceed)}
            onToggle={(index, checked) =>
              form.setValue(`budgets.${index}.enabled`, checked, {
                shouldDirty: true,
              })
            }
            onEdit={setEditing}
            onUsage={setUsageRow}
            onOverride={setOverrideRow}
            toolbarEnd={
              <>
                <div
                  className='flex flex-wrap items-center gap-2'
                  aria-label={t('Default action when exceeded')}
                >
                  <span className='text-muted-foreground text-xs'>
                    {t('Default action when exceeded')}
                  </span>
                  <NativeSelect
                    aria-label={t('Default action when exceeded')}
                    className='h-8'
                    {...form.register('default_on_exceed.mode')}
                  >
                    <NativeSelectOption value='reject'>
                      {modeLabels.reject}
                    </NativeSelectOption>
                    <NativeSelectOption value='fallback'>
                      {modeLabels.fallback}
                    </NativeSelectOption>
                  </NativeSelect>
                  {config.default_on_exceed.mode === 'fallback' && (
                    <>
                      <NativeSelect
                        aria-label={t('Target channel')}
                        className='h-8'
                        value={config.default_on_exceed.channel_id}
                        onChange={(event) => {
                          form.setValue(
                            'default_on_exceed.channel_id',
                            Number(event.target.value),
                            {
                              shouldDirty: true,
                            }
                          )
                          form.setValue('default_on_exceed.model', '', {
                            shouldDirty: true,
                          })
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
                      <NativeSelect
                        aria-label={t('Target model')}
                        className='h-8'
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
                    </>
                  )}
                </div>
                <Button
                  type='button'
                  size='sm'
                  disabled={disabled || budgets.fields.length >= 128}
                  onClick={addBudget}
                >
                  {t('Add budget')}
                </Button>
              </>
            }
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Usage is counted by the client model name. Daily and weekly counters ignore schedules, so a scheduled daily or weekly budget shares its counter with the unscheduled budget of the same scope and models.'
            )}
          </p>
        </div>
        <div hidden={props.section !== 'schedules'}>
          <ScheduleList
            form={form}
            fields={schedules.fields}
            schedules={config.schedules}
            budgets={config.budgets}
            states={scheduleStates}
            now={props.view.now}
            disabled={disabled}
            onAdd={() =>
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
            onRemove={(index) => schedules.remove(index)}
          />
        </div>
      </fieldset>
      <PolicySaveBar
        changes={changes}
        disabled={disabled}
        busy={busy}
        onDiscard={() => setConfirmDiscard(true)}
        onPreview={() => void runPreview()}
      />
      <ConfirmDialog
        open={confirmDiscard}
        onOpenChange={setConfirmDiscard}
        title={t('Discard unsaved changes?')}
        desc={t(
          '{{count}} changes will be lost and the saved policy restored.',
          {
            count: changes.length,
          }
        )}
        confirmText={t('Discard')}
        destructive
        handleConfirm={() => {
          setConfirmDiscard(false)
          setEditing(null)
          form.reset(props.view.config)
        }}
      />
      <BudgetEditorSheet
        form={form}
        formId={formId}
        index={editing}
        config={config}
        targets={props.targets}
        selfTarget={selfTarget}
        otherTargets={otherTargets}
        disabled={disabled}
        fieldKey={
          editing === null ? '' : (budgets.fields[editing]?.formKey ?? '')
        }
        rowError={
          editing !== null &&
          config.budgets[editing] &&
          errorRows.includes(config.budgets[editing].id)
            ? errorDetail
            : ''
        }
        onClose={() => setEditing(null)}
        onRemove={(index) => {
          setEditing(null)
          budgets.remove(index)
        }}
        onDuplicate={(index) => {
          const source = config.budgets[index]
          budgets.append({
            ...source,
            id: `${NEW_ID_PREFIX}budget-${++nextId.current}`,
            name: source.name ? `${source.name} (copy)` : '',
            created_at: 0,
          })
          setEditing(budgets.fields.length)
        }}
      />
      <BudgetUsageSheet
        channelId={props.channelId}
        row={usageRow}
        canOperate={props.canOperate}
        onClose={() => setUsageRow(null)}
      />
      <BudgetOverrideSheet
        channelId={props.channelId}
        open={overrideRow !== null}
        budget={overrideRow}
        canOperate={props.canOperate}
        onClose={() => setOverrideRow(null)}
        onChanged={() => {
          props.onRetrySummary()
          void queryClient.invalidateQueries({
            queryKey: ['channels', props.channelId],
          })
        }}
      />
      <PolicyPreviewDialog
        open={previewOpen && preview !== null && previewMatchesDraft}
        preview={preview}
        changes={changes}
        disablesRows={disablesRows}
        busy={saveMutation.isPending}
        onClose={() => setPreviewOpen(false)}
        onConfirm={() => void runSave()}
      />
    </form>
  )
}
