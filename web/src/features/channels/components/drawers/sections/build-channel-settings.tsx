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
import { Eye, RefreshCw, Search } from 'lucide-react'
import { type ReactNode, useMemo } from 'react'
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import type { ChannelFormValues } from '../../../lib'
import { ResponsesCompactPassthroughField } from './responses-compact-passthrough-field'
import { VisionAssistModelFields } from './vision-assist-model-fields'

const UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT = 8

type BuildChannelUpstreamModelDetectionSectionProps = {
  id: string
  className: string
  disabled: boolean
}

function BuildCardHeading(props: {
  title: string
  icon?: ReactNode
  iconTone?: IconBadgeTone
}) {
  return (
    <div className='flex items-center gap-3'>
      {props.icon && (
        <IconBadge tone={props.iconTone} size='md'>
          {props.icon}
        </IconBadge>
      )}
      <h3 className='text-sm font-semibold tracking-tight'>{props.title}</h3>
    </div>
  )
}

function BuildSubHeading(props: {
  title: string
  icon?: ReactNode
  iconTone?: IconBadgeTone
}) {
  return (
    <div className='flex items-center gap-2'>
      {props.icon && (
        <IconBadge tone={props.iconTone} size='xs'>
          {props.icon}
        </IconBadge>
      )}
      <h4 className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
        {props.title}
      </h4>
    </div>
  )
}

function formatUnixTime(timestamp: unknown): string {
  const seconds = Number(timestamp)
  if (!Number.isFinite(seconds) || seconds <= 0) return '-'
  return new Date(seconds * 1000).toLocaleString()
}

function parseSettingsRecord(
  settings: string | undefined
): Record<string, unknown> {
  if (!settings?.trim()) return {}
  try {
    const parsed = JSON.parse(settings)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
  } catch {
    return {}
  }
  return {}
}

/**
 * 渲染 build 分支专属的渠道额外设置字段。
 *
 * @returns 与当前渠道表单绑定的 build 专属设置字段。
 */
export function BuildChannelExtraSettingsFields() {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const webSearchProvider = form.watch('web_search_provider')
  const webSearchApiKeyConfigured = form.watch('web_search_api_key_configured')
  let webSearchApiKeyPlaceholder = t('Enter provider API Key')
  if (webSearchApiKeyConfigured) {
    webSearchApiKeyPlaceholder = t('Leave empty to keep existing key')
  } else if (webSearchProvider === 'anysearch') {
    webSearchApiKeyPlaceholder = t('Optional provider API Key')
  }

  return (
    <>
      <ResponsesCompactPassthroughField />

      <FormField
        control={form.control}
        name='use_upstream_model_for_billing'
        render={({ field }) => (
          <FormItem className='flex items-center justify-between px-4 py-3'>
            <div className='space-y-0.5'>
              <FormLabel>{t('Bill Mapped Upstream Model')}</FormLabel>
              <FormDescription>
                {t(
                  'When model_mapping is applied, use the final upstream model for logs and billing'
                )}
              </FormDescription>
            </div>
            <FormControl>
              <Switch checked={field.value} onCheckedChange={field.onChange} />
            </FormControl>
          </FormItem>
        )}
      />

      <div className='border-border/60 flex flex-col gap-4 border-y py-4'>
        <BuildSubHeading
          title={t('Claude Code WebSearch')}
          icon={<Search className='h-3.5 w-3.5' />}
        />

        <FormField
          control={form.control}
          name='web_search_enabled'
          render={({ field }) => (
            <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
              <div className='space-y-0.5'>
                <FormLabel>{t('Enable WebSearch emulation')}</FormLabel>
                <FormDescription>
                  {t(
                    'Handle pure Claude Code web_search requests locally for this channel'
                  )}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />

        <div className='grid gap-4 sm:grid-cols-2'>
          <FormField
            control={form.control}
            name='web_search_provider'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Search provider')}</FormLabel>
                <Select
                  items={[
                    { value: 'tavily', label: 'Tavily' },
                    { value: 'anysearch', label: 'AnySearch' },
                  ]}
                  value={field.value || 'tavily'}
                  onValueChange={field.onChange}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='tavily'>Tavily</SelectItem>
                      <SelectItem value='anysearch'>AnySearch</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='web_search_max_results'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max search results')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={20}
                    placeholder='5'
                    value={field.value ?? 5}
                    onChange={(e) => field.onChange(Number(e.target.value))}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <FormField
          control={form.control}
          name='web_search_api_key'
          render={({ field }) => (
            <FormItem>
              <div className='flex flex-wrap items-center gap-2'>
                <FormLabel>{t('WebSearch API Key')}</FormLabel>
                {webSearchApiKeyConfigured && (
                  <Badge variant='secondary'>{t('Configured')}</Badge>
                )}
              </div>
              <FormControl>
                <Input
                  type='password'
                  autoComplete='new-password'
                  placeholder={webSearchApiKeyPlaceholder}
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Required for Tavily. Optional for AnySearch; when provided it is sent with Authorization Bearer and never returned by the API'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='web_search_clear_api_key'
          render={({ field }) => (
            <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
              <div className='space-y-0.5'>
                <FormLabel>{t('Clear saved WebSearch key')}</FormLabel>
                <FormDescription>
                  {t(
                    'Clearing is allowed for AnySearch. Tavily requires WebSearch disabled or a replacement key.'
                  )}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />

        {webSearchProvider === 'tavily' ? (
          <FormField
            control={form.control}
            name='web_search_search_depth'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Tavily search depth')}</FormLabel>
                <Select
                  items={[
                    { value: 'basic', label: t('Basic') },
                    { value: 'advanced', label: t('Advanced') },
                  ]}
                  value={field.value || 'basic'}
                  onValueChange={field.onChange}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='basic'>{t('Basic')}</SelectItem>
                      <SelectItem value='advanced'>{t('Advanced')}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
        ) : (
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='web_search_freshness'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('AnySearch freshness')}</FormLabel>
                  <Select
                    items={[
                      { value: 'none', label: t('None') },
                      { value: 'day', label: t('Day') },
                      { value: 'week', label: t('Week') },
                      { value: 'month', label: t('Month') },
                      { value: 'year', label: t('Year') },
                    ]}
                    value={field.value || 'none'}
                    onValueChange={(value) =>
                      field.onChange(value === 'none' ? '' : value)
                    }
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='none'>{t('None')}</SelectItem>
                        <SelectItem value='day'>{t('Day')}</SelectItem>
                        <SelectItem value='week'>{t('Week')}</SelectItem>
                        <SelectItem value='month'>{t('Month')}</SelectItem>
                        <SelectItem value='year'>{t('Year')}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='web_search_content_types'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('AnySearch content types')}</FormLabel>
                  <FormControl>
                    <Input placeholder='web,news,doc' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        )}
      </div>

      <div className='border-border/60 flex flex-col gap-4 border-y py-4'>
        <BuildSubHeading
          title={t('Vision Assist Settings')}
          icon={<Eye className='h-3.5 w-3.5' />}
        />

        <FormField
          control={form.control}
          name='vision_assist_enabled'
          render={({ field }) => (
            <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
              <div className='space-y-0.5'>
                <FormLabel>{t('Enable vision assist')}</FormLabel>
                <FormDescription>
                  {t(
                    'Convert images to text before forwarding requests to this channel'
                  )}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />

        <VisionAssistModelFields />

        <FormField
          control={form.control}
          name='vision_assist_target_models'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Target models')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('Leave empty to apply to all models')}
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Comma-separated upstream model names that should use vision assist'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='vision_assist_prompt'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Vision assist prompt')}</FormLabel>
              <FormControl>
                <Textarea
                  rows={3}
                  placeholder={t(
                    'Leave empty to use the built-in default prompt'
                  )}
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t('Prompt sent to the assist model for image analysis')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='vision_assist_multi_image_mode'
          render={({ field }) => {
            const currentValue = field.value || 'separate'
            return (
              <FormItem>
                <FormLabel>{t('Multi-image mode')}</FormLabel>
                <FormControl>
                  <ToggleGroup
                    value={[currentValue]}
                    onValueChange={(value) => {
                      const nextValue = value.find(
                        (item) => item !== currentValue
                      )
                      if (nextValue) field.onChange(nextValue)
                    }}
                    aria-label={t('Multi-image mode')}
                    variant='outline'
                    spacing={2}
                    className='grid w-full grid-cols-2 gap-2'
                  >
                    <ToggleGroupItem
                      value='separate'
                      className='h-auto min-h-10 w-full px-3 py-2'
                    >
                      {t('Separate images')}
                    </ToggleGroupItem>
                    <ToggleGroupItem
                      value='combined'
                      className='h-auto min-h-10 w-full px-3 py-2'
                    >
                      {t('Combine images')}
                    </ToggleGroupItem>
                  </ToggleGroup>
                </FormControl>
                <FormDescription>
                  {t(
                    'Separate sends one assist request per image; combined sends images from the same message in one request'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )
          }}
        />

        <div className='grid gap-4 sm:grid-cols-2'>
          <FormField
            control={form.control}
            name='vision_assist_endpoint_mode'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Endpoint mode')}</FormLabel>
                <Select
                  items={[
                    { value: 'auto', label: t('Auto') },
                    {
                      value: 'openai_chat',
                      label: t('OpenAI Chat Completions'),
                    },
                    {
                      value: 'openai_responses',
                      label: t('OpenAI Responses'),
                    },
                    {
                      value: 'anthropic_messages',
                      label: t('Anthropic Messages'),
                    },
                    { value: 'gemini_native', label: t('Gemini Native') },
                  ]}
                  value={field.value || 'auto'}
                  onValueChange={field.onChange}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='auto'>{t('Auto')}</SelectItem>
                      <SelectItem value='openai_chat'>
                        {t('OpenAI Chat Completions')}
                      </SelectItem>
                      <SelectItem value='openai_responses'>
                        {t('OpenAI Responses')}
                      </SelectItem>
                      <SelectItem value='anthropic_messages'>
                        {t('Anthropic Messages')}
                      </SelectItem>
                      <SelectItem value='gemini_native'>
                        {t('Gemini Native')}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t(
                    'Auto uses Gemini native for Gemini, Anthropic Messages for Claude, and OpenAI Chat otherwise'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='vision_assist_failure_policy'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Failure policy')}</FormLabel>
                <Select
                  items={[
                    { value: 'error', label: t('Error') },
                    { value: 'skip', label: t('Skip') },
                  ]}
                  value={field.value || 'error'}
                  onValueChange={field.onChange}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='error'>{t('Error')}</SelectItem>
                      <SelectItem value='skip'>{t('Skip')}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t('Error fails the request; skip ignores failed images')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <div className='grid gap-4 sm:grid-cols-4'>
          <FormField
            control={form.control}
            name='vision_assist_cache_ttl_seconds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Cache TTL seconds')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    placeholder='86400'
                    value={field.value ?? 86400}
                    onChange={(e) => field.onChange(Number(e.target.value))}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='vision_assist_max_concurrency'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max concurrency')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={8}
                    placeholder='2'
                    value={field.value ?? 2}
                    onChange={(e) => field.onChange(Number(e.target.value))}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='vision_assist_retry_count'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Retry count')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={5}
                    placeholder='1'
                    value={field.value ?? 1}
                    onChange={(e) => field.onChange(Number(e.target.value))}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='vision_assist_retry_backoff_ms'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Retry backoff ms')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={30000}
                    placeholder='500'
                    value={field.value ?? 500}
                    onChange={(e) => field.onChange(Number(e.target.value))}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <FormField
          control={form.control}
          name='vision_assist_strip_image'
          render={({ field }) => (
            <FormItem className='flex items-center justify-between gap-3 px-4 py-3'>
              <div className='space-y-0.5'>
                <FormLabel>{t('Remove original images')}</FormLabel>
                <FormDescription>
                  {t(
                    'After text is injected, remove original image parts from the forwarded request'
                  )}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value !== false}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />
      </div>
    </>
  )
}

/**
 * 渲染 build 分支专属的上游模型检测设置区块。
 *
 * @param props 组件参数。
 * @returns 与当前渠道表单绑定的上游模型检测设置区块。
 */
export function BuildChannelUpstreamModelDetectionSection(
  props: BuildChannelUpstreamModelDetectionSectionProps
) {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const upstreamModelUpdateCheckEnabled = form.watch(
    'upstream_model_update_check_enabled'
  )
  const currentSettings = form.watch('settings')
  const upstreamUpdateMeta = useMemo(() => {
    const settings = parseSettingsRecord(currentSettings)
    const detectedModels = Array.isArray(
      settings.upstream_model_update_last_detected_models
    )
      ? settings.upstream_model_update_last_detected_models
          .map((model) => String(model || '').trim())
          .filter(Boolean)
      : []

    return {
      lastCheckTime: settings.upstream_model_update_last_check_time,
      detectedModels: [...new Set(detectedModels)],
    }
  }, [currentSettings])
  const upstreamDetectedModelsPreview = upstreamUpdateMeta.detectedModels.slice(
    0,
    UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT
  )
  const upstreamDetectedModelsOmittedCount =
    upstreamUpdateMeta.detectedModels.length -
    upstreamDetectedModelsPreview.length

  return (
    <div id={props.id} className={props.className}>
      <BuildCardHeading
        title={t('Upstream Model Detection Settings')}
        icon={<RefreshCw className='h-4 w-4' />}
        iconTone='info'
      />
      <fieldset
        disabled={props.disabled}
        className='space-y-4 disabled:opacity-60'
      >
        <div className='divide-border space-y-0 divide-y border-y'>
          <FormField
            control={form.control}
            name='upstream_model_update_check_enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between px-4 py-3'>
                <div className='space-y-0.5'>
                  <FormLabel>{t('Upstream Model Update Check')}</FormLabel>
                  <FormDescription>
                    {t('Periodically check for upstream model changes')}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='upstream_model_update_auto_sync_enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between px-4 py-3'>
                <div className='space-y-0.5'>
                  <FormLabel>{t('Auto Sync Upstream Models')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Automatically sync model list when upstream changes are detected'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    disabled={!upstreamModelUpdateCheckEnabled}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
        </div>
        <FormField
          control={form.control}
          name='upstream_model_update_ignored_models'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Ignored upstream models')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t(
                    'e.g., gpt-4.1-nano,regex:^claude-.*$,regex:^sora-.*$'
                  )}
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Comma-separated exact model names. Prefix with regex: to ignore by regular expression.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className='text-muted-foreground space-y-2 border-t pt-3 text-xs'>
          <div>
            <span className='text-foreground font-medium'>
              {t('Last check time')}:
            </span>{' '}
            {formatUnixTime(upstreamUpdateMeta.lastCheckTime)}
          </div>
          <div>
            <span className='text-foreground font-medium'>
              {t('Last detected addable models')}:
            </span>{' '}
            {upstreamUpdateMeta.detectedModels.length === 0 ? (
              t('None')
            ) : (
              <>
                <span className='break-all'>
                  {upstreamDetectedModelsPreview.join(', ')}
                </span>
                {upstreamDetectedModelsOmittedCount > 0 && (
                  <span className='ml-1'>
                    {t('({{total}} total, {{omit}} omitted)', {
                      total: upstreamUpdateMeta.detectedModels.length,
                      omit: upstreamDetectedModelsOmittedCount,
                    })}
                  </span>
                )}
              </>
            )}
          </div>
        </div>
      </fieldset>
    </div>
  )
}
