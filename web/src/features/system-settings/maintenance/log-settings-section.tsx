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
import { safeNumberFieldProps } from '../utils/numeric-field'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useId, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { getMessageAuditReviewOptions } from '@/features/message-audits/api'
import { api } from '@/lib/api'
import {
  SettingsControlGroup,
  SettingsForm,
  SettingsFormGrid,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

import { HistoryLogCleanup } from './history-log-cleanup'
import { ServerLogFiles } from './server-log-files'

const logSettingsSchema = z.object({
  LogConsumeEnabled: z.boolean(),
  log_setting: z.object({
    user_export_enabled: z.boolean(),
    max_range_days: z.number().int().min(1),
  }),
  system_task_setting: z.object({
    retention_days: z.number().int().min(1),
  }),
  MessageAuditEnabled: z.boolean(),
  MessageAuditRetentionDays: z.number().int().min(1).max(30),
  MessageAuditReviewChannelID: z.number().int().min(0),
  MessageAuditReviewModel: z.string(),
  MessageAuditReviewToolCallLimit: z.number().int().positive(),
  MessageAuditReviewContextMode: z.enum(['merged', 'tool']),
})

type LogSettingsFormValues = z.infer<typeof logSettingsSchema>

type LogSettingsSectionProps = {
  defaultValues: {
    LogConsumeEnabled: boolean
    'log_setting.user_export_enabled': boolean
    'log_setting.max_range_days': number
    'system_task_setting.retention_days': number
  }

  defaultMessageAuditEnabled: boolean
  defaultMessageAuditRetentionDays: number
  defaultMessageAuditReviewConfig: string
}

const DEFAULT_MESSAGE_AUDIT_REVIEW_TOOL_CALL_LIMIT = 24
const DEFAULT_MESSAGE_AUDIT_REVIEW_CONTEXT_MODE = 'merged'

function parseMessageAuditReviewConfig(raw: string): {
  channelID: number
  model: string
  toolCallLimit: number
  contextMode: 'merged' | 'tool'
} {
  try {
    const value = JSON.parse(raw) as {
      channel_id?: unknown
      model?: unknown
      tool_call_limit?: unknown
      context_mode?: unknown
    }
    return {
      channelID:
        typeof value.channel_id === 'number' && value.channel_id > 0
          ? value.channel_id
          : 0,
      model: typeof value.model === 'string' ? value.model : '',
      toolCallLimit:
        typeof value.tool_call_limit === 'number' &&
        Number.isInteger(value.tool_call_limit) &&
        value.tool_call_limit >= 1
          ? value.tool_call_limit
          : DEFAULT_MESSAGE_AUDIT_REVIEW_TOOL_CALL_LIMIT,
      contextMode:
        value.context_mode === 'tool'
          ? 'tool'
          : DEFAULT_MESSAGE_AUDIT_REVIEW_CONTEXT_MODE,
    }
  } catch {
    return {
      channelID: 0,
      model: '',
      toolCallLimit: DEFAULT_MESSAGE_AUDIT_REVIEW_TOOL_CALL_LIMIT,
      contextMode: DEFAULT_MESSAGE_AUDIT_REVIEW_CONTEXT_MODE,
    }
  }
}

export function LogSettingsSection({
  defaultValues,
  defaultMessageAuditEnabled,
  defaultMessageAuditRetentionDays,
  defaultMessageAuditReviewConfig,
}: LogSettingsSectionProps) {
  const defaultEnabled = defaultValues.LogConsumeEnabled
  const recordingTitleId = useId()
  const searchTitleId = useId()
  const taskHistoryTitleId = useId()
  const extraDefaults = useMemo(() => ({
    log_setting: { user_export_enabled: defaultValues['log_setting.user_export_enabled'], max_range_days: defaultValues['log_setting.max_range_days'] },
    system_task_setting: { retention_days: defaultValues['system_task_setting.retention_days'] },
  }), [defaultValues])

  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const defaultReviewConfig = useMemo(
    () => parseMessageAuditReviewConfig(defaultMessageAuditReviewConfig),
    [defaultMessageAuditReviewConfig]
  )
  const form = useForm<LogSettingsFormValues>({
    resolver: zodResolver(logSettingsSchema),
    defaultValues: {
      LogConsumeEnabled: defaultEnabled,
      ...extraDefaults,
      MessageAuditEnabled: defaultMessageAuditEnabled,
      MessageAuditRetentionDays: defaultMessageAuditRetentionDays,
      MessageAuditReviewChannelID: defaultReviewConfig.channelID,
      MessageAuditReviewModel: defaultReviewConfig.model,
      MessageAuditReviewToolCallLimit: defaultReviewConfig.toolCallLimit,
      MessageAuditReviewContextMode: defaultReviewConfig.contextMode,
    },
  })

  const [messageAuditKeyConfigured, setMessageAuditKeyConfigured] =
    useState(false)
  const reviewOptionsQuery = useQuery({
    queryKey: ['message-audit-review-options'],
    queryFn: getMessageAuditReviewOptions,
  })
  const selectedReviewChannelID = form.watch('MessageAuditReviewChannelID')
  const selectedReviewModel = form.watch('MessageAuditReviewModel')
  const selectedReviewContextMode = form.watch('MessageAuditReviewContextMode')
  const selectedReviewChannel = reviewOptionsQuery.data?.channels.find(
    (channel) => channel.id === selectedReviewChannelID
  )
  const reviewContextModeItems = useMemo(
    () => [
      { value: 'merged', label: t('Merged context') },
      { value: 'tool', label: t('Model Tool reading') },
    ],
    [t]
  )
  const reviewChannelItems = useMemo(
    () =>
      (reviewOptionsQuery.data?.channels ?? []).map((channel) => ({
        value: String(channel.id),
        label: channel.name,
      })),
    [reviewOptionsQuery.data?.channels]
  )
  const reviewModelItems = useMemo(
    () =>
      (selectedReviewChannel?.models ?? []).map((model) => ({
        value: model,
        label: model,
      })),
    [selectedReviewChannel?.models]
  )
  const reviewConfigAvailable =
    selectedReviewChannel !== undefined &&
    selectedReviewChannel.models.includes(selectedReviewModel)

  useEffect(() => {
    form.reset({
      LogConsumeEnabled: defaultEnabled,
      ...extraDefaults,
      MessageAuditEnabled: defaultMessageAuditEnabled,
      MessageAuditRetentionDays: defaultMessageAuditRetentionDays,
      MessageAuditReviewChannelID: defaultReviewConfig.channelID,
      MessageAuditReviewModel: defaultReviewConfig.model,
      MessageAuditReviewToolCallLimit: defaultReviewConfig.toolCallLimit,
      MessageAuditReviewContextMode: defaultReviewConfig.contextMode,
    })
  }, [
    defaultEnabled,
    extraDefaults,
    defaultMessageAuditEnabled,
    defaultMessageAuditRetentionDays,
    defaultReviewConfig,
    form,
  ])

  useEffect(() => {
    let cancelled = false
    async function fetchMessageAuditStatus() {
      try {
        const res = await api.get('/api/message-audit/status')
        if (!cancelled && res.data.success) {
          setMessageAuditKeyConfigured(Boolean(res.data.data?.key_configured))
        }
      } catch {
        /* 状态提示不阻断其他日志设置。 */
      }
    }
    void fetchMessageAuditStatus()
    return () => {
      cancelled = true
    }
  }, [])

  const onSubmit = async (values: LogSettingsFormValues) => {
    if (
      values.MessageAuditReviewChannelID > 0 !==
      Boolean(values.MessageAuditReviewModel)
    ) {
      toast.error(t('Select both a review channel and model.'))
      return
    }
    const changes = [
      [
        'log_setting.user_export_enabled',
        values.log_setting.user_export_enabled,
      ],
      ['log_setting.max_range_days', values.log_setting.max_range_days],
      [
        'system_task_setting.retention_days',
        values.system_task_setting.retention_days,
      ],
    ] as const
    for (const [key, value] of changes) {
      if (value === defaultValues[key]) continue
      await updateOption.mutateAsync({ key, value })
    }
    if (values.LogConsumeEnabled !== defaultEnabled) {
      await updateOption.mutateAsync({
        key: 'LogConsumeEnabled',
        value: values.LogConsumeEnabled,
      })
    }
    if (values.MessageAuditEnabled !== defaultMessageAuditEnabled) {
      await updateOption.mutateAsync({
        key: 'MessageAuditEnabled',
        value: values.MessageAuditEnabled,
      })
    }
    if (values.MessageAuditRetentionDays !== defaultMessageAuditRetentionDays) {
      await updateOption.mutateAsync({
        key: 'MessageAuditRetentionDays',
        value: values.MessageAuditRetentionDays,
      })
    }
    const nextReviewConfig =
      values.MessageAuditReviewChannelID > 0 && values.MessageAuditReviewModel
        ? JSON.stringify({
            channel_id: values.MessageAuditReviewChannelID,
            model: values.MessageAuditReviewModel,
            tool_call_limit: values.MessageAuditReviewToolCallLimit,
            context_mode: values.MessageAuditReviewContextMode,
          })
        : ''
    const currentReviewConfig =
      defaultReviewConfig.channelID > 0 && defaultReviewConfig.model
        ? JSON.stringify({
            channel_id: defaultReviewConfig.channelID,
            model: defaultReviewConfig.model,
            tool_call_limit: defaultReviewConfig.toolCallLimit,
            context_mode: defaultReviewConfig.contextMode,
          })
        : ''
    if (nextReviewConfig !== currentReviewConfig) {
      await updateOption.mutateAsync({
        key: 'message_audit_review.config',
        value: nextReviewConfig,
      })
    }
  }

  return (
    <SettingsSection title={t('Log Maintenance')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save log settings'
          />
          <div
            role='group'
            aria-labelledby={recordingTitleId}
            className='flex min-w-0 flex-col gap-4'
          >
            <h4 id={recordingTitleId} className='font-medium'>
              {t('Log recording')}
            </h4>
            <FormField
              control={form.control}
              name='LogConsumeEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Record quota usage')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Track per-request consumption to power usage analytics. Keeping this on increases database writes.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                  <FormMessage />
                </SettingsSwitchItem>
              )}
            />
          </div>

          <Separator />

          <div
            role='group'
            aria-labelledby={searchTitleId}
            className='flex min-w-0 flex-col gap-4'
          >
            <h4 id={searchTitleId} className='font-medium'>
              {t('Log search and export')}
            </h4>
            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='log_setting.user_export_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Allow users to export logs')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Users can download their own logs from the usage log page. Administrators can always export.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                    <FormMessage />
                  </SettingsSwitchItem>
                )}
              />
              <FormField
                control={form.control}
                name='log_setting.max_range_days'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Longest log time range (days)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'The longest time range a user can view at once in the logs and on the dashboard, and the longest range any export can cover.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGrid>
          </div>

          <Separator />

          <div
            role='group'
            aria-labelledby={taskHistoryTitleId}
            className='flex min-w-0 flex-col gap-4'
          >
            <h4 id={taskHistoryTitleId} className='font-medium'>
              {t('System task records')}
            </h4>
            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='system_task_setting.retention_days'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Keep finished tasks for (days)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        placeholder='30'
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Finished system tasks older than this are deleted automatically every day.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGrid>
          </div>

          <SettingsControlGroup className='space-y-4'>
            <div>
              <h4 className='text-sm font-medium'>{t('Message auditing')}</h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Persist encrypted inbound AI messages for root security review. Relay requests remain fail-open when audit storage is unavailable.'
                )}
              </p>
            </div>
            <FormField
              control={form.control}
              name='MessageAuditEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Capture inbound messages')}</FormLabel>
                    <FormDescription>
                      {messageAuditKeyConfigured
                        ? t('Encryption key is configured on this node.')
                        : t(
                            'MESSAGE_AUDIT_SECRET is missing or invalid. Configure it on every node before enabling auditing.'
                          )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={!messageAuditKeyConfigured && !field.value}
                    />
                  </FormControl>
                  <FormMessage />
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='MessageAuditRetentionDays'
              render={({ field }) => (
                <div className='grid max-w-xs gap-2'>
                  <FormLabel>{t('Audit retention days')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={30}
                      value={field.value}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value))
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Encrypted message audits are retained for 1 to 30 days.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />
            <Separator />
            <div className='space-y-1'>
              <h5 className='text-sm font-medium'>{t('AI review model')}</h5>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Used for administrator-triggered session reviews. Review calls are not billed and are not added to message audits.'
                )}
              </p>
            </div>
            {reviewOptionsQuery.isError && (
              <Alert variant='destructive'>
                <AlertDescription>
                  {t('Failed to load review channels and models.')}
                </AlertDescription>
              </Alert>
            )}
            {!reviewOptionsQuery.isLoading &&
              selectedReviewChannelID > 0 &&
              !reviewConfigAvailable && (
                <Alert variant='destructive'>
                  <AlertDescription>
                    {t(
                      'The saved review channel or model is unavailable. Select a new pair before starting a review.'
                    )}
                  </AlertDescription>
                </Alert>
              )}
            <div className='grid max-w-2xl gap-4 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='MessageAuditReviewChannelID'
                render={({ field }) => (
                  <div className='grid gap-2'>
                    <FormLabel>{t('Review channel')}</FormLabel>
                    <Select
                      items={reviewChannelItems}
                      value={
                        reviewChannelItems.some(
                          (item) => item.value === String(field.value)
                        )
                          ? String(field.value)
                          : null
                      }
                      onValueChange={(value) => {
                        field.onChange(value ? Number(value) : 0)
                        form.setValue('MessageAuditReviewModel', '', {
                          shouldDirty: true,
                        })
                      }}
                      disabled={
                        reviewOptionsQuery.isLoading ||
                        reviewChannelItems.length === 0
                      }
                    >
                      <FormControl>
                        <SelectTrigger className='w-full'>
                          <SelectValue placeholder={t('Select a channel')} />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectGroup>
                          {reviewChannelItems.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </div>
                )}
              />
              <FormField
                control={form.control}
                name='MessageAuditReviewModel'
                render={({ field }) => (
                  <div className='grid gap-2'>
                    <FormLabel>{t('Review model')}</FormLabel>
                    <Select
                      items={reviewModelItems}
                      value={
                        reviewModelItems.some(
                          (item) => item.value === field.value
                        )
                          ? field.value
                          : null
                      }
                      onValueChange={(value) => field.onChange(value ?? '')}
                      disabled={
                        !selectedReviewChannel || reviewModelItems.length === 0
                      }
                    >
                      <FormControl>
                        <SelectTrigger className='w-full'>
                          <SelectValue placeholder={t('Select a model')} />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectGroup>
                          {reviewModelItems.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </div>
                )}
              />
            </div>
            <FormField
              control={form.control}
              name='MessageAuditReviewContextMode'
              render={({ field }) => (
                <div className='grid max-w-xs gap-2'>
                  <FormLabel>{t('AI review context mode')}</FormLabel>
                  <Select
                    items={reviewContextModeItems}
                    value={field.value}
                    onValueChange={(value) =>
                      field.onChange(value === 'tool' ? 'tool' : 'merged')
                    }
                  >
                    <FormControl>
                      <SelectTrigger className='w-full'>
                        <SelectValue placeholder={t('Select a context mode')} />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectGroup>
                        {reviewContextModeItems.map((item) => (
                          <SelectItem key={item.value} value={item.value}>
                            {item.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {t(
                      'Merged context sends saved audit material once. Model Tool reading lets the model read virtual files itself.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />
            <FormField
              control={form.control}
              name='MessageAuditReviewToolCallLimit'
              render={({ field }) => (
                <div className='grid max-w-xs gap-2'>
                  <FormLabel>{t('AI review Tool call limit')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      value={field.value}
                      disabled={selectedReviewContextMode !== 'tool'}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value))
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Only applies when context mode is Model Tool reading. Enter any positive integer; the task timeout still prevents stuck runs.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />
          </SettingsControlGroup>

        </SettingsForm>
      </Form>

      <Separator />

      <HistoryLogCleanup />

      <Separator />

      <ServerLogFiles />
    </SettingsSection>
  )
}
