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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { updateSystemOption } from '@/features/system-settings/api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '@/features/system-settings/components/settings-form-layout'
import { SettingsPageFormActions } from '@/features/system-settings/components/settings-page-context'
import { SettingsSection } from '@/features/system-settings/components/settings-section'
import { formatTimestampToDate } from '@/lib/format'

import { getTokenLeakScanStatus } from '../api'
import { parseTokenLeakIntervalInput } from '../lib/token-leak-scan'
import type { TokenLeakScanSettingsValues } from '../types'

type TokenLeakScanSettingsSectionProps = {
  defaultValues: TokenLeakScanSettingsValues
}

type TokenLeakScanFormValues = {
  token_leak_scan: {
    enabled: boolean
    interval_hours: number
  }
}

function CredentialStatusBadge(props: {
  configured: boolean | undefined
  loading: boolean
}) {
  const { t } = useTranslation()
  let label = t('Unavailable')
  let variant: 'success' | 'warning' | 'neutral' = 'neutral'
  if (props.loading) {
    label = t('Checking...')
  } else if (props.configured) {
    label = t('Configured')
    variant = 'success'
  } else if (props.configured === false) {
    label = t('Not configured')
    variant = 'warning'
  }

  return <StatusBadge label={label} variant={variant} copyable={false} />
}

const buildFormDefaults = (
  defaults: TokenLeakScanSettingsValues
): TokenLeakScanFormValues => ({
  token_leak_scan: {
    enabled: defaults['token_leak_scan.enabled'],
    interval_hours: defaults['token_leak_scan.interval_hours'],
  },
})

/**
 * 渲染 GitHub Key 泄露扫描的配置与外部凭据状态。
 *
 * @param props 默认持久化配置。
 * @returns 系统安全设置分区。
 */
export function TokenLeakScanSettingsSection(
  props: TokenLeakScanSettingsSectionProps
) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const formSchema = useMemo(
    () =>
      z.object({
        token_leak_scan: z.object({
          enabled: z.boolean(),
          interval_hours: z
            .number()
            .int(t('Scan interval must be a whole number.'))
            .min(1, t('Scan interval must be between 1 and 168 hours.'))
            .max(168, t('Scan interval must be between 1 and 168 hours.')),
        }),
      }),
    [t]
  )
  const form = useForm<TokenLeakScanFormValues>({
    resolver: zodResolver(formSchema),
    mode: 'onChange',
    defaultValues: buildFormDefaults(props.defaultValues),
  })
  const statusQuery = useQuery({
    queryKey: ['token-leak-scan', 'status'],
    queryFn: getTokenLeakScanStatus,
  })
  const saveMutation = useMutation({
    mutationFn: async (values: TokenLeakScanFormValues) => {
      const normalized: TokenLeakScanSettingsValues = {
        'token_leak_scan.enabled': values.token_leak_scan.enabled,
        'token_leak_scan.interval_hours': values.token_leak_scan.interval_hours,
      }
      const updateKeys = (
        Object.keys(normalized) as Array<keyof TokenLeakScanSettingsValues>
      ).filter((key) => normalized[key] !== props.defaultValues[key])

      for (const key of updateKeys) {
        const response = await updateSystemOption({
          key,
          value: normalized[key],
        })
        if (!response.success) {
          throw new Error(response.message)
        }
      }
      return normalized
    },
    onSuccess: async (values) => {
      form.reset(buildFormDefaults(values))
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['system-options'] }),
        queryClient.invalidateQueries({
          queryKey: ['token-leak-scan', 'status'],
        }),
      ])
      toast.success(t('Leak scan settings saved.'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to save leak scan settings.'))
    },
  })

  useEffect(() => {
    form.reset(buildFormDefaults(props.defaultValues))
  }, [form, props.defaultValues])

  const onSubmit = (values: TokenLeakScanFormValues) => {
    saveMutation.mutate(values)
  }
  const credentials = statusQuery.data?.credentials
  let authLabel = t('Not checked')
  let authVariant: 'success' | 'danger' | 'neutral' = 'neutral'
  if (statusQuery.isLoading) {
    authLabel = t('Checking...')
  } else if (statusQuery.data?.github_auth_status === 'ok') {
    authLabel = t('Authentication OK')
    authVariant = 'success'
  } else if (statusQuery.data?.github_auth_status === 'failed') {
    authLabel = t('Authentication failed')
    authVariant = 'danger'
  }
  const credentialRows = [
    {
      label: t('GitHub scan token'),
      configured: credentials?.github_token_configured,
    },
    {
      label: t('HMAC scan secret'),
      configured: credentials?.scan_secret_configured,
    },
    {
      label: t('DingTalk webhook'),
      configured: credentials?.dingtalk_webhook_configured,
    },
    {
      label: t('DingTalk signing secret'),
      configured: credentials?.dingtalk_signing_configured,
    },
  ]

  return (
    <SettingsSection title={t('GitHub Key Leak Scan')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={saveMutation.isPending}
            isSaveDisabled={!form.formState.isDirty}
            saveLabel='Save leak scan settings'
          />
          <FormField
            control={form.control}
            name='token_leak_scan.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable GitHub key leak scan')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Periodically scan user API keys against GitHub public code search.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <FormField
            control={form.control}
            name='token_leak_scan.interval_hours'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Scan interval (hours)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={168}
                    step={1}
                    {...field}
                    value={field.value > 0 ? field.value : ''}
                    onChange={(event) => {
                      field.onChange(
                        parseTokenLeakIntervalInput(
                          event.target.value,
                          event.target.valueAsNumber
                        )
                      )
                    }}
                  />
                </FormControl>
                <FormDescription>
                  {t('Run a complete scan every 1 to 168 hours.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div data-settings-form-span='full' className='space-y-2'>
            <h4 className='text-sm font-medium'>{t('Credential status')}</h4>
            <div className='divide-y overflow-hidden rounded-lg border'>
              {credentialRows.map((row) => (
                <div
                  key={row.label}
                  className='flex min-h-10 items-center justify-between gap-3 px-3 py-2'
                >
                  <span className='text-sm'>{row.label}</span>
                  <CredentialStatusBadge
                    configured={row.configured}
                    loading={statusQuery.isLoading}
                  />
                </div>
              ))}
              <div className='flex min-h-10 items-center justify-between gap-3 px-3 py-2'>
                <div className='min-w-0'>
                  <div className='text-sm'>{t('GitHub authentication')}</div>
                  {statusQuery.data?.github_auth_checked_at ? (
                    <div className='text-muted-foreground text-xs'>
                      {t('Last checked: {{time}}', {
                        time: formatTimestampToDate(
                          statusQuery.data.github_auth_checked_at
                        ),
                      })}
                    </div>
                  ) : null}
                </div>
                <StatusBadge
                  label={authLabel}
                  variant={authVariant}
                  copyable={false}
                />
              </div>
            </div>
          </div>

          <Alert>
            <AlertTitle>{t('Best-effort detection')}</AlertTitle>
            <AlertDescription>
              {t(
                "Only GitHub's currently searchable public default branches are covered. Unindexed files, non-default branches, history, deleted content, and split or encoded keys may not be detected."
              )}
            </AlertDescription>
          </Alert>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
