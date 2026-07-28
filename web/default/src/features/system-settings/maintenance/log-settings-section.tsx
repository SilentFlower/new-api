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
import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { DateTimePicker } from '@/components/datetime-picker'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
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
import dayjs from '@/lib/dayjs'
import { formatTimestampToDate } from '@/lib/format'

import {
  getCurrentLogCleanupTask,
  getSystemTask,
  startLogCleanupTask,
} from '../api'
import {
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { LogCleanupTask } from '../types'

const logSettingsSchema = z.object({
  LogConsumeEnabled: z.boolean(),
  MessageAuditEnabled: z.boolean(),
  MessageAuditRetentionDays: z.number().int().min(1).max(30),
  MessageAuditReviewChannelID: z.number().int().min(0),
  MessageAuditReviewModel: z.string(),
  MessageAuditReviewToolCallLimit: z.number().int().min(1).max(64),
})

type LogSettingsFormValues = z.infer<typeof logSettingsSchema>

type LogSettingsSectionProps = {
  defaultEnabled: boolean
  defaultMessageAuditEnabled: boolean
  defaultMessageAuditRetentionDays: number
  defaultMessageAuditReviewConfig: string
}

type ServerLogInfo = {
  enabled: boolean
  log_dir: string
  file_count: number
  total_size: number
  oldest_time?: string
  newest_time?: string
}

const HOURS_IN_DAY = 24
const DEFAULT_MESSAGE_AUDIT_REVIEW_TOOL_CALL_LIMIT = 24

function parseMessageAuditReviewConfig(raw: string): {
  channelID: number
  model: string
  toolCallLimit: number
} {
  try {
    const value = JSON.parse(raw) as {
      channel_id?: unknown
      model?: unknown
      tool_call_limit?: unknown
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
        value.tool_call_limit >= 1 &&
        value.tool_call_limit <= 64
          ? value.tool_call_limit
          : DEFAULT_MESSAGE_AUDIT_REVIEW_TOOL_CALL_LIMIT,
    }
  } catch {
    return {
      channelID: 0,
      model: '',
      toolCallLimit: DEFAULT_MESSAGE_AUDIT_REVIEW_TOOL_CALL_LIMIT,
    }
  }
}

function formatBytes(bytes: number, decimals = 2): string {
  if (!bytes || Number.isNaN(bytes)) return '0 Bytes'
  if (bytes === 0) return '0 Bytes'
  if (bytes < 0) return `-${formatBytes(-bytes, decimals)}`
  const k = 1024
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k))
  if (i < 0 || i >= sizes.length) return `${bytes} Bytes`
  return `${Number.parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${
    sizes[i]
  }`
}

const getDateHoursAgo = (hours: number) => {
  const date = new Date()
  date.setHours(date.getHours() - hours)
  return date
}

const getDateDaysAgo = (days: number) => getDateHoursAgo(days * HOURS_IN_DAY)

const quickSelectOptions = [
  {
    label: '24 hours ago',
    getValue: () => getDateHoursAgo(24),
  },
  {
    label: '7 days ago',
    getValue: () => getDateDaysAgo(7),
  },
  {
    label: '30 days ago',
    getValue: () => getDateDaysAgo(30),
  },
]

function isActiveLogCleanupTask(task: LogCleanupTask | null) {
  return task?.status === 'pending' || task?.status === 'running'
}

export function LogSettingsSection({
  defaultEnabled,
  defaultMessageAuditEnabled,
  defaultMessageAuditRetentionDays,
  defaultMessageAuditReviewConfig,
}: LogSettingsSectionProps) {
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
      MessageAuditEnabled: defaultMessageAuditEnabled,
      MessageAuditRetentionDays: defaultMessageAuditRetentionDays,
      MessageAuditReviewChannelID: defaultReviewConfig.channelID,
      MessageAuditReviewModel: defaultReviewConfig.model,
      MessageAuditReviewToolCallLimit: defaultReviewConfig.toolCallLimit,
    },
  })

  const [purgeDate, setPurgeDate] = useState<Date | undefined>(() =>
    getDateDaysAgo(30)
  )
  const [isStartingLogCleanup, setIsStartingLogCleanup] = useState(false)
  const [logCleanupTask, setLogCleanupTask] = useState<LogCleanupTask | null>(
    null
  )
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)
  const [serverLogInfo, setServerLogInfo] = useState<ServerLogInfo | null>(null)
  const [serverLogCleanupMode, setServerLogCleanupMode] = useState('by_count')
  const [serverLogCleanupValue, setServerLogCleanupValue] = useState(10)
  const [serverLogCleanupLoading, setServerLogCleanupLoading] = useState(false)
  const [messageAuditKeyConfigured, setMessageAuditKeyConfigured] =
    useState(false)
  const reviewOptionsQuery = useQuery({
    queryKey: ['message-audit-review-options'],
    queryFn: getMessageAuditReviewOptions,
  })
  const selectedReviewChannelID = form.watch('MessageAuditReviewChannelID')
  const selectedReviewModel = form.watch('MessageAuditReviewModel')
  const selectedReviewChannel = reviewOptionsQuery.data?.channels.find(
    (channel) => channel.id === selectedReviewChannelID
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

  const fetchServerLogInfo = useCallback(async () => {
    try {
      const res = await api.get('/api/performance/logs')
      if (res.data.success) setServerLogInfo(res.data.data)
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => {
    form.reset({
      LogConsumeEnabled: defaultEnabled,
      MessageAuditEnabled: defaultMessageAuditEnabled,
      MessageAuditRetentionDays: defaultMessageAuditRetentionDays,
      MessageAuditReviewChannelID: defaultReviewConfig.channelID,
      MessageAuditReviewModel: defaultReviewConfig.model,
      MessageAuditReviewToolCallLimit: defaultReviewConfig.toolCallLimit,
    })
  }, [
    defaultEnabled,
    defaultMessageAuditEnabled,
    defaultMessageAuditRetentionDays,
    defaultReviewConfig,
    form,
  ])

  useEffect(() => {
    fetchServerLogInfo()
  }, [fetchServerLogInfo])

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

  useEffect(() => {
    let cancelled = false

    async function fetchCurrentLogCleanupTask() {
      try {
        const res = await getCurrentLogCleanupTask()
        if (!cancelled && res.success && res.data) {
          setLogCleanupTask(res.data)
        }
      } catch {
        /* ignore */
      }
    }

    fetchCurrentLogCleanupTask()

    return () => {
      cancelled = true
    }
  }, [])

  const purgeTimestamp = useMemo(() => {
    if (!purgeDate) return null
    return Math.floor(purgeDate.getTime() / 1000)
  }, [purgeDate])

  const formattedPurgeDate = useMemo(() => {
    if (!purgeDate) return ''
    return formatTimestampToDate(purgeDate.getTime(), 'milliseconds')
  }, [purgeDate])

  const logCleanupActive = isActiveLogCleanupTask(logCleanupTask)
  const logCleanupState = logCleanupTask?.state
  const logCleanupProgress = Math.min(
    100,
    Math.max(0, logCleanupState?.progress ?? 0)
  )
  const logCleanupProcessed = logCleanupState?.processed ?? 0
  const logCleanupTotal = logCleanupState?.total ?? 0
  const logCleanupTaskId = logCleanupTask?.task_id

  useEffect(() => {
    if (!logCleanupTaskId || !logCleanupActive) return

    let cancelled = false
    const interval = window.setInterval(async () => {
      try {
        const res = await getSystemTask(logCleanupTaskId)
        if (cancelled || !res.success || !res.data) return

        setLogCleanupTask(res.data)
        if (!isActiveLogCleanupTask(res.data)) {
          if (res.data.status === 'succeeded') {
            const count =
              res.data.result?.deleted_count ?? res.data.state?.processed ?? 0
            toast.success(
              count > 0
                ? t('{{count}} log entries removed.', { count })
                : t('No log entries matched the selected time.')
            )
          } else if (res.data.status === 'failed') {
            toast.error(res.data.error || t('Failed to clean logs'))
          }
        }
      } catch {
        /* keep polling */
      }
    }, 1000)

    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [logCleanupActive, logCleanupTaskId, t])

  const onSubmit = async (values: LogSettingsFormValues) => {
    if (
      values.MessageAuditReviewChannelID > 0 !==
      Boolean(values.MessageAuditReviewModel)
    ) {
      toast.error(t('Select both a review channel and model.'))
      return
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
          })
        : ''
    const currentReviewConfig =
      defaultReviewConfig.channelID > 0 && defaultReviewConfig.model
        ? JSON.stringify({
            channel_id: defaultReviewConfig.channelID,
            model: defaultReviewConfig.model,
            tool_call_limit: defaultReviewConfig.toolCallLimit,
          })
        : ''
    if (nextReviewConfig !== currentReviewConfig) {
      await updateOption.mutateAsync({
        key: 'message_audit_review.config',
        value: nextReviewConfig,
      })
    }
  }

  const handleRequestCleanLogs = () => {
    if (!purgeTimestamp) {
      toast.error(t('Select a timestamp before clearing logs.'))
      return
    }

    setShowConfirmDialog(true)
  }

  const handleCleanLogs = async () => {
    if (!purgeTimestamp) {
      toast.error(t('Select a timestamp before clearing logs.'))
      return
    }

    setIsStartingLogCleanup(true)
    try {
      const res = await startLogCleanupTask(purgeTimestamp)
      if (!res.success) {
        throw new Error(res.message || t('Failed to clean logs'))
      }
      if (!res.data) {
        throw new Error(t('Failed to clean logs'))
      }
      setLogCleanupTask(res.data)
      setShowConfirmDialog(false)
      toast.success(t('Log cleanup task started.'))
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Failed to clean logs')
      toast.error(message)
    } finally {
      setIsStartingLogCleanup(false)
    }
  }

  const cleanupServerLogFiles = async () => {
    if (
      !serverLogCleanupValue ||
      Number.isNaN(serverLogCleanupValue) ||
      serverLogCleanupValue < 1
    ) {
      toast.error(t('Please enter a valid number'))
      return
    }

    setServerLogCleanupLoading(true)
    try {
      const res = await api.delete(
        `/api/performance/logs?mode=${serverLogCleanupMode}&value=${serverLogCleanupValue}`
      )
      if (res.data.success) {
        const { deleted_count, freed_bytes } = res.data.data
        toast.success(
          t('Cleaned up {{count}} log files, freed {{size}}', {
            count: deleted_count,
            size: formatBytes(freed_bytes),
          })
        )
      } else {
        toast.error(res.data.message || t('Cleanup failed'))
      }
      fetchServerLogInfo()
    } catch {
      toast.error(t('Cleanup failed'))
    } finally {
      setServerLogCleanupLoading(false)
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
              name='MessageAuditReviewToolCallLimit'
              render={({ field }) => (
                <div className='grid max-w-xs gap-2'>
                  <FormLabel>{t('AI review Tool call limit')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={64}
                      value={field.value}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value))
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Allows 1 to 64 Tool calls per review. Context, Tool token, and timeout limits still apply.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />
          </SettingsControlGroup>

          <SettingsControlGroup className='space-y-3'>
            <div>
              <h4 className='text-sm font-medium'>{t('Clean history logs')}</h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Remove all log entries created before the selected timestamp.'
                )}
              </p>
            </div>
            <DateTimePicker value={purgeDate} onChange={setPurgeDate} />
            <div className='flex flex-wrap gap-3'>
              {quickSelectOptions.map((option) => (
                <Button
                  key={option.label}
                  type='button'
                  variant='outline'
                  onClick={() => setPurgeDate(option.getValue())}
                >
                  {t(option.label)}
                </Button>
              ))}
              <Button
                type='button'
                variant='destructive'
                onClick={handleRequestCleanLogs}
                disabled={isStartingLogCleanup || logCleanupActive}
              >
                {isStartingLogCleanup || logCleanupActive
                  ? t('Cleaning...')
                  : t('Clean logs')}
              </Button>
            </div>
            {logCleanupTask && (
              <div className='rounded-md border p-3'>
                <div className='mb-2 flex items-center justify-between gap-3 text-sm'>
                  <span className='font-medium'>
                    {t('Log cleanup progress')}
                  </span>
                  <span className='text-muted-foreground tabular-nums'>
                    {logCleanupProgress}%
                  </span>
                </div>
                <Progress value={logCleanupProgress} />
                <div className='text-muted-foreground mt-2 text-xs'>
                  {t('{{processed}} of {{total}} log entries processed.', {
                    processed: logCleanupProcessed,
                    total: logCleanupTotal,
                  })}
                </div>
                {logCleanupTask.status === 'failed' && logCleanupTask.error && (
                  <div className='text-destructive mt-2 text-xs'>
                    {logCleanupTask.error}
                  </div>
                )}
              </div>
            )}
          </SettingsControlGroup>
        </SettingsForm>
      </Form>

      <Separator />

      <div className='space-y-4'>
        <div>
          <h4 className='font-medium'>{t('Server Log Management')}</h4>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t(
              'Manage server log files. Log files accumulate over time; regular cleanup is recommended to free disk space.'
            )}
          </p>
        </div>

        {serverLogInfo !== null &&
          (serverLogInfo.enabled ? (
            <div className='space-y-4'>
              <div className='rounded-lg border p-4'>
                <div className='grid grid-cols-2 gap-2 text-sm md:grid-cols-4'>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Log Directory')}:
                    </span>{' '}
                    <span className='font-mono text-xs'>
                      {serverLogInfo.log_dir}
                    </span>
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Log File Count')}:
                    </span>{' '}
                    {serverLogInfo.file_count}
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Total Log Size')}:
                    </span>{' '}
                    {formatBytes(serverLogInfo.total_size)}
                  </div>
                  {serverLogInfo.oldest_time && serverLogInfo.newest_time && (
                    <div>
                      <span className='text-muted-foreground'>
                        {t('Date Range')}:
                      </span>{' '}
                      {dayjs(serverLogInfo.oldest_time).format('YYYY-MM-DD')} ~{' '}
                      {dayjs(serverLogInfo.newest_time).format('YYYY-MM-DD')}
                    </div>
                  )}
                </div>
              </div>

              <div className='flex flex-wrap items-end gap-3'>
                <div className='grid gap-1.5'>
                  <Label className='text-xs'>{t('Cleanup Mode')}</Label>
                  <Select
                    items={[
                      { value: 'by_count', label: t('Retain last N files') },
                      { value: 'by_days', label: t('Retain last N days') },
                    ]}
                    value={serverLogCleanupMode}
                    onValueChange={(value) =>
                      value !== null && setServerLogCleanupMode(value)
                    }
                  >
                    <SelectTrigger className='w-[160px]'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='by_count'>
                          {t('Retain last N files')}
                        </SelectItem>
                        <SelectItem value='by_days'>
                          {t('Retain last N days')}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
                <div className='grid gap-1.5'>
                  <Label className='text-xs'>
                    {serverLogCleanupMode === 'by_count'
                      ? t('Files to Retain')
                      : t('Days to Retain')}
                  </Label>
                  <Input
                    type='number'
                    min={1}
                    max={serverLogCleanupMode === 'by_count' ? 1000 : 3650}
                    value={serverLogCleanupValue}
                    onChange={(event) =>
                      setServerLogCleanupValue(Number(event.target.value))
                    }
                    className='w-[120px]'
                  />
                </div>
                <AlertDialog>
                  <AlertDialogTrigger
                    render={
                      <Button
                        type='button'
                        variant='destructive'
                        size='sm'
                        disabled={serverLogCleanupLoading}
                      />
                    }
                  >
                    {serverLogCleanupLoading
                      ? t('Cleaning...')
                      : t('Clean Up Log Files')}
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>
                        {t('Confirm log file cleanup?')}
                      </AlertDialogTitle>
                      <AlertDialogDescription>
                        {serverLogCleanupMode === 'by_count'
                          ? t(
                              'Only the last {{value}} log files will be retained; the rest will be deleted.',
                              {
                                value: serverLogCleanupValue,
                              }
                            )
                          : t(
                              'Log files older than {{value}} days will be deleted.',
                              {
                                value: serverLogCleanupValue,
                              }
                            )}
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                      <AlertDialogAction
                        variant='destructive'
                        onClick={cleanupServerLogFiles}
                      >
                        {t('Confirm Cleanup')}
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            </div>
          ) : (
            <Alert>
              <AlertDescription>
                {t(
                  'Server logging is not enabled (log directory not configured)'
                )}
              </AlertDescription>
            </Alert>
          ))}
      </div>

      <AlertDialog open={showConfirmDialog} onOpenChange={setShowConfirmDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Confirm log cleanup')}</AlertDialogTitle>
            <AlertDialogDescription>
              {formattedPurgeDate
                ? t(
                    'This will permanently remove all log entries created before {{date}}.',
                    { date: formattedPurgeDate }
                  )
                : t(
                    'This will permanently remove log entries before the selected timestamp.'
                  )}{' '}
              {t('This action cannot be undone.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isStartingLogCleanup}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={handleCleanLogs}
              disabled={isStartingLogCleanup}
            >
              {isStartingLogCleanup ? t('Cleaning...') : t('Delete logs')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
