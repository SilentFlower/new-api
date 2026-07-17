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
import { z } from 'zod'

import { MODEL_FETCHABLE_TYPES } from '../constants'
import type { ChannelFormValues } from './channel-form'

export const visionAssistEndpointModes = [
  'auto',
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_native',
] as const

export type VisionAssistEndpointMode =
  (typeof visionAssistEndpointModes)[number]

export const webSearchProviders = ['tavily', 'anysearch'] as const
export type WebSearchProvider = (typeof webSearchProviders)[number]

export const tavilySearchDepths = ['basic', 'advanced'] as const
export type TavilySearchDepth = (typeof tavilySearchDepths)[number]

export const anySearchFreshnessValues = [
  '',
  'day',
  'week',
  'month',
  'year',
] as const
export type AnySearchFreshness = (typeof anySearchFreshnessValues)[number]

export const buildChannelSettingFormSchema = {
  responses_compact_passthrough_enabled: z.boolean().optional(),
  use_upstream_model_for_billing: z.boolean().optional(),
  vision_assist_enabled: z.boolean().optional(),
  vision_assist_channel_id: z.number().min(0).optional(),
  vision_assist_model: z.string().optional(),
  vision_assist_target_models: z.string().optional(),
  vision_assist_prompt: z.string().optional(),
  vision_assist_cache_ttl_seconds: z.number().min(0).optional(),
  vision_assist_failure_policy: z.enum(['error', 'skip']).optional(),
  vision_assist_strip_image: z.boolean().optional(),
  vision_assist_endpoint_mode: z.enum(visionAssistEndpointModes).optional(),
  vision_assist_max_concurrency: z.number().min(1).max(8).optional(),
  vision_assist_retry_count: z.number().min(0).max(5).optional(),
  vision_assist_retry_backoff_ms: z.number().min(1).max(30000).optional(),
  web_search_enabled: z.boolean().optional(),
  web_search_provider: z.enum(webSearchProviders).optional(),
  web_search_api_key: z.string().optional(),
  web_search_api_key_configured: z.boolean().optional(),
  web_search_clear_api_key: z.boolean().optional(),
  web_search_max_results: z.number().min(1).max(20).optional(),
  web_search_search_depth: z.enum(tavilySearchDepths).optional(),
  web_search_freshness: z.enum(anySearchFreshnessValues).optional(),
  web_search_content_types: z.string().optional(),
} satisfies z.ZodRawShape

export const buildChannelOtherSettingFormSchema = {
  upstream_model_update_check_enabled: z.boolean().optional(),
  upstream_model_update_auto_sync_enabled: z.boolean().optional(),
  upstream_model_update_ignored_models: z.string().optional(),
} satisfies z.ZodRawShape

export const BUILD_CHANNEL_SETTING_DEFAULTS = {
  responses_compact_passthrough_enabled: false,
  use_upstream_model_for_billing: false,
  vision_assist_enabled: false,
  vision_assist_channel_id: 0,
  vision_assist_model: '',
  vision_assist_target_models: '',
  vision_assist_prompt: '',
  vision_assist_cache_ttl_seconds: 86400,
  vision_assist_failure_policy: 'error' as const,
  vision_assist_strip_image: true,
  vision_assist_endpoint_mode: 'auto' as const,
  vision_assist_max_concurrency: 2,
  vision_assist_retry_count: 1,
  vision_assist_retry_backoff_ms: 500,
  web_search_enabled: false,
  web_search_provider: 'tavily' as const,
  web_search_api_key: '',
  web_search_api_key_configured: false,
  web_search_clear_api_key: false,
  web_search_max_results: 5,
  web_search_search_depth: 'basic' as const,
  web_search_freshness: '' as const,
  web_search_content_types: '',
}

export const BUILD_CHANNEL_OTHER_SETTING_DEFAULTS = {
  upstream_model_update_check_enabled: false,
  upstream_model_update_auto_sync_enabled: false,
  upstream_model_update_ignored_models: '',
}

export const BUILD_CHANNEL_SETTING_FORM_FIELDS = [
  'responses_compact_passthrough_enabled',
  'use_upstream_model_for_billing',
  'vision_assist_enabled',
  'vision_assist_channel_id',
  'vision_assist_model',
  'vision_assist_target_models',
  'vision_assist_prompt',
  'vision_assist_cache_ttl_seconds',
  'vision_assist_failure_policy',
  'vision_assist_strip_image',
  'vision_assist_endpoint_mode',
  'vision_assist_max_concurrency',
  'vision_assist_retry_count',
  'vision_assist_retry_backoff_ms',
  'web_search_enabled',
  'web_search_provider',
  'web_search_api_key',
  'web_search_api_key_configured',
  'web_search_clear_api_key',
  'web_search_max_results',
  'web_search_search_depth',
  'web_search_freshness',
  'web_search_content_types',
] satisfies (keyof ChannelFormValues)[]

export const BUILD_CHANNEL_OTHER_SETTING_FORM_FIELDS = [
  'upstream_model_update_check_enabled',
  'upstream_model_update_auto_sync_enabled',
  'upstream_model_update_ignored_models',
] satisfies (keyof ChannelFormValues)[]

type BuildChannelSettingValues = Required<
  Pick<
    ChannelFormValues,
    (typeof BUILD_CHANNEL_SETTING_FORM_FIELDS)[number]
  >
>

type BuildChannelOtherSettingValues = Required<
  Pick<
    ChannelFormValues,
    (typeof BUILD_CHANNEL_OTHER_SETTING_FORM_FIELDS)[number]
  >
>

function isJsonObjectValue(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseCommaList(value: string | undefined): string[] {
  return String(value || '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function numberOrDefault(value: unknown, defaultValue: number): number {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : defaultValue
}

function minNumberOrDefault(
  value: unknown,
  minValue: number,
  defaultValue: number
): number {
  const numberValue = numberOrDefault(value, defaultValue)
  return numberValue >= minValue ? numberValue : defaultValue
}

export function normalizeVisionAssistEndpointMode(
  value: unknown
): VisionAssistEndpointMode {
  const endpointMode = String(value || '')
  return visionAssistEndpointModes.includes(
    endpointMode as VisionAssistEndpointMode
  )
    ? (endpointMode as VisionAssistEndpointMode)
    : 'auto'
}

export function normalizeWebSearchProvider(value: unknown): WebSearchProvider {
  const provider = String(value || '')
  return webSearchProviders.includes(provider as WebSearchProvider)
    ? (provider as WebSearchProvider)
    : 'tavily'
}

export function normalizeTavilySearchDepth(value: unknown): TavilySearchDepth {
  const depth = String(value || '')
  return tavilySearchDepths.includes(depth as TavilySearchDepth)
    ? (depth as TavilySearchDepth)
    : 'basic'
}

export function normalizeAnySearchFreshness(
  value: unknown
): AnySearchFreshness {
  const freshness = String(value || '')
  return anySearchFreshnessValues.includes(freshness as AnySearchFreshness)
    ? (freshness as AnySearchFreshness)
    : ''
}

/**
 * 从渠道 setting JSON 对象恢复 build 分支专属表单字段。
 *
 * @param settings 已解析的渠道 setting JSON。
 * @returns build 分支专属表单字段默认值。
 */
export function parseBuildChannelSettingDefaults(
  settings: unknown
): BuildChannelSettingValues {
  const parsed = isJsonObjectValue(settings) ? settings : {}
  const visionAssist = isJsonObjectValue(parsed.vision_assist)
    ? parsed.vision_assist
    : {}
  const webSearch = isJsonObjectValue(parsed.web_search)
    ? parsed.web_search
    : {}

  return {
    responses_compact_passthrough_enabled:
      parsed.responses_compact_passthrough_enabled === true,
    use_upstream_model_for_billing:
      parsed.use_upstream_model_for_billing === true,
    vision_assist_enabled: visionAssist.enabled === true,
    vision_assist_channel_id: Number(visionAssist.assist_channel_id) || 0,
    vision_assist_model: String(visionAssist.assist_model || ''),
    vision_assist_target_models: Array.isArray(visionAssist.target_models)
      ? visionAssist.target_models.join(',')
      : '',
    vision_assist_prompt: String(visionAssist.prompt || ''),
    vision_assist_cache_ttl_seconds: minNumberOrDefault(
      visionAssist.cache_ttl_seconds,
      0,
      86400
    ),
    vision_assist_failure_policy:
      visionAssist.failure_policy === 'skip' ? 'skip' : 'error',
    vision_assist_strip_image:
      visionAssist.strip_image === undefined
        ? true
        : visionAssist.strip_image !== false,
    vision_assist_endpoint_mode: normalizeVisionAssistEndpointMode(
      visionAssist.endpoint_mode
    ),
    vision_assist_max_concurrency: minNumberOrDefault(
      visionAssist.max_concurrency,
      1,
      2
    ),
    vision_assist_retry_count: minNumberOrDefault(
      visionAssist.retry_count,
      0,
      1
    ),
    vision_assist_retry_backoff_ms: minNumberOrDefault(
      visionAssist.retry_backoff_ms,
      1,
      500
    ),
    web_search_enabled: webSearch.enabled === true,
    web_search_provider: normalizeWebSearchProvider(webSearch.provider),
    web_search_api_key: '',
    web_search_api_key_configured: webSearch.api_key_configured === true,
    web_search_clear_api_key: false,
    web_search_max_results: minNumberOrDefault(webSearch.max_results, 1, 5),
    web_search_search_depth: normalizeTavilySearchDepth(
      webSearch.search_depth
    ),
    web_search_freshness: normalizeAnySearchFreshness(webSearch.freshness),
    web_search_content_types: Array.isArray(webSearch.content_types)
      ? webSearch.content_types.join(',')
      : '',
  }
}

/**
 * 从渠道 settings JSON 对象恢复 build 分支专属 other_settings 表单字段。
 *
 * @param settings 已解析的渠道 settings JSON。
 * @returns build 分支专属 other_settings 表单字段默认值。
 */
export function parseBuildChannelOtherSettingDefaults(
  settings: unknown
): BuildChannelOtherSettingValues {
  const parsed = isJsonObjectValue(settings) ? settings : {}

  return {
    upstream_model_update_check_enabled:
      parsed.upstream_model_update_check_enabled === true,
    upstream_model_update_auto_sync_enabled:
      parsed.upstream_model_update_auto_sync_enabled === true,
    upstream_model_update_ignored_models: Array.isArray(
      parsed.upstream_model_update_ignored_models
    )
      ? parsed.upstream_model_update_ignored_models.join(',')
      : '',
  }
}

/**
 * 将 build 分支专属表单字段合并回渠道 setting JSON 对象。
 *
 * @param formData 当前渠道表单值。
 * @param existingSettings 已解析的原始 setting JSON。
 * @returns 可直接展开进 setting JSON 的 build 字段。
 */
export function buildChannelSettingFields(
  formData: ChannelFormValues,
  existingSettings: Record<string, unknown>
): Record<string, unknown> {
  return {
    responses_compact_passthrough_enabled:
      formData.responses_compact_passthrough_enabled === true,
    use_upstream_model_for_billing:
      formData.use_upstream_model_for_billing === true,
    vision_assist: {
      ...((isJsonObjectValue(existingSettings.vision_assist)
        ? existingSettings.vision_assist
        : {}) as Record<string, unknown>),
      enabled: formData.vision_assist_enabled === true,
      assist_channel_id: Number(formData.vision_assist_channel_id) || 0,
      assist_model: String(formData.vision_assist_model || '').trim(),
      target_models: parseCommaList(formData.vision_assist_target_models),
      prompt: String(formData.vision_assist_prompt || '').trim(),
      cache_ttl_seconds: minNumberOrDefault(
        formData.vision_assist_cache_ttl_seconds,
        0,
        86400
      ),
      failure_policy:
        formData.vision_assist_failure_policy === 'skip' ? 'skip' : 'error',
      strip_image: formData.vision_assist_strip_image !== false,
      endpoint_mode: normalizeVisionAssistEndpointMode(
        formData.vision_assist_endpoint_mode
      ),
      max_concurrency: minNumberOrDefault(
        formData.vision_assist_max_concurrency,
        1,
        2
      ),
      retry_count: minNumberOrDefault(formData.vision_assist_retry_count, 0, 1),
      retry_backoff_ms: minNumberOrDefault(
        formData.vision_assist_retry_backoff_ms,
        1,
        500
      ),
    },
    web_search: {
      ...((isJsonObjectValue(existingSettings.web_search)
        ? existingSettings.web_search
        : {}) as Record<string, unknown>),
      enabled: formData.web_search_enabled === true,
      provider: normalizeWebSearchProvider(formData.web_search_provider),
      api_key: String(formData.web_search_api_key || '').trim() || undefined,
      clear_api_key:
        formData.web_search_clear_api_key === true &&
        !String(formData.web_search_api_key || '').trim(),
      max_results: minNumberOrDefault(formData.web_search_max_results, 1, 5),
      search_depth: normalizeTavilySearchDepth(
        formData.web_search_search_depth
      ),
      freshness: normalizeAnySearchFreshness(formData.web_search_freshness),
      content_types: parseCommaList(formData.web_search_content_types),
    },
  }
}

/**
 * 将 build 分支专属 other_settings 字段合并回渠道 settings JSON 对象。
 *
 * @param formData 当前渠道表单值。
 * @param settingsObj 已解析的原始 settings JSON，函数会原地更新该对象。
 * @returns 更新后的 settings JSON 对象。
 */
export function applyBuildChannelOtherSettingFields(
  formData: ChannelFormValues,
  settingsObj: Record<string, unknown>
): Record<string, unknown> {
  if (!MODEL_FETCHABLE_TYPES.has(formData.type)) return settingsObj

  settingsObj.upstream_model_update_check_enabled =
    formData.upstream_model_update_check_enabled === true
  settingsObj.upstream_model_update_auto_sync_enabled =
    settingsObj.upstream_model_update_check_enabled === true &&
    formData.upstream_model_update_auto_sync_enabled === true
  settingsObj.upstream_model_update_ignored_models = [
    ...new Set(
      String(formData.upstream_model_update_ignored_models || '')
        .split(',')
        .map((model) => model.trim())
        .filter(Boolean)
    ),
  ]
  if (
    !Array.isArray(settingsObj.upstream_model_update_last_detected_models) ||
    settingsObj.upstream_model_update_check_enabled !== true
  ) {
    settingsObj.upstream_model_update_last_detected_models = []
  }
  if (typeof settingsObj.upstream_model_update_last_check_time !== 'number') {
    settingsObj.upstream_model_update_last_check_time = 0
  }

  return settingsObj
}

/**
 * 校验 build 分支专属渠道设置的跨字段约束。
 *
 * @param data 当前渠道表单值。
 * @param ctx Zod 校验上下文。
 * @param addRequiredIssue 项目内统一的字段错误写入函数。
 */
export function refineBuildChannelSettings(
  data: ChannelFormValues,
  ctx: z.RefinementCtx,
  addRequiredIssue: (
    ctx: z.RefinementCtx,
    path: string,
    message: string
  ) => void
): void {
  if (data.vision_assist_enabled === true) {
    if (!data.vision_assist_channel_id || data.vision_assist_channel_id <= 0) {
      addRequiredIssue(
        ctx,
        'vision_assist_channel_id',
        'Assist channel ID is required when vision assist is enabled'
      )
    }
    if (!data.vision_assist_model?.trim()) {
      addRequiredIssue(
        ctx,
        'vision_assist_model',
        'Assist model is required when vision assist is enabled'
      )
    }
  }
  if (data.web_search_enabled === true) {
    const providerRequiresKey = data.web_search_provider === 'tavily'
    const hasNewKey = Boolean(data.web_search_api_key?.trim())
    const hasExistingKey = data.web_search_api_key_configured === true
    if (providerRequiresKey && !hasNewKey && !hasExistingKey) {
      addRequiredIssue(
        ctx,
        'web_search_api_key',
        'API Key is required when Tavily WebSearch is enabled'
      )
    }
    if (
      providerRequiresKey &&
      data.web_search_clear_api_key === true &&
      !hasNewKey
    ) {
      addRequiredIssue(
        ctx,
        'web_search_api_key',
        'Enter a new API Key before clearing the existing one'
      )
    }
  }
}

/**
 * 判断 build 分支专属高级设置是否有非默认配置。
 *
 * @param values 当前渠道表单值。
 * @returns 任一 build 专属设置非默认时返回 true。
 */
export function hasBuildChannelSettingValues(
  values: ChannelFormValues
): boolean {
  return Boolean(
    values.responses_compact_passthrough_enabled ||
    values.use_upstream_model_for_billing ||
    values.vision_assist_enabled ||
    values.web_search_enabled ||
    values.web_search_provider !== 'tavily' ||
    values.web_search_api_key?.trim() ||
    values.web_search_clear_api_key ||
    (values.web_search_max_results && values.web_search_max_results !== 5) ||
    values.web_search_search_depth !== 'basic' ||
    values.web_search_freshness ||
    values.web_search_content_types?.trim() ||
    values.upstream_model_update_check_enabled ||
    values.upstream_model_update_auto_sync_enabled ||
    values.upstream_model_update_ignored_models?.trim()
  )
}
