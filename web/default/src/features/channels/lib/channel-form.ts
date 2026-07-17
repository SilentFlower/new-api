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

import {
  CHANNEL_STATUS,
  ERROR_MESSAGES,
  MODEL_FETCHABLE_TYPES,
} from '../constants'
import type { Channel } from '../types'
import {
  CHANNEL_TYPE_ADVANCED_CUSTOM,
  advancedCustomConfigUsesRelativeUpstreamPath,
  parseAdvancedCustomConfig,
  stringifyAdvancedCustomConfig,
  validateAdvancedCustomConfig,
} from './advanced-custom'

// ============================================================================
// Form Validation Schema
// ============================================================================

function parseOptionalJson(value: string | undefined): unknown {
  if (!value?.trim()) return undefined
  return JSON.parse(value)
}

function isJsonObjectValue(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isOptionalJsonObject(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    return parsed === undefined || isJsonObjectValue(parsed)
  } catch {
    return false
  }
}

function isOptionalModelMapping(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (!isJsonObjectValue(parsed)) return false
    return Object.values(parsed).every((item) => typeof item === 'string')
  } catch {
    return false
  }
}

function isOptionalStatusCodeMapping(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (!isJsonObjectValue(parsed)) return false
    return Object.entries(parsed).every(([from, to]) => {
      const fromCode = Number(from)
      const toCode = Number(to)
      return (
        Number.isInteger(fromCode) &&
        Number.isInteger(toCode) &&
        fromCode >= 100 &&
        fromCode <= 599 &&
        toCode >= 100 &&
        toCode <= 599
      )
    })
  } catch {
    return false
  }
}

function parseCommaList(value: string | undefined): string[] {
  return String(value || '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function isCodexCredential(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    return (
      isJsonObjectValue(parsed) &&
      typeof parsed.access_token === 'string' &&
      parsed.access_token.trim().length > 0 &&
      typeof parsed.account_id === 'string' &&
      parsed.account_id.trim().length > 0
    )
  } catch {
    return false
  }
}

function isVertexJsonKey(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (Array.isArray(parsed)) {
      return parsed.every((item) => isJsonObjectValue(item))
    }
    return isJsonObjectValue(parsed)
  } catch {
    return false
  }
}

function addRequiredIssue(
  ctx: z.RefinementCtx,
  path: string,
  message: string
): void {
  ctx.addIssue({
    code: z.ZodIssueCode.custom,
    path: [path],
    message,
  })
}

const visionAssistEndpointModes = [
  'auto',
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_native',
] as const

type VisionAssistEndpointMode = (typeof visionAssistEndpointModes)[number]

const webSearchProviders = ['tavily', 'anysearch'] as const
type WebSearchProvider = (typeof webSearchProviders)[number]

const tavilySearchDepths = ['basic', 'advanced'] as const
type TavilySearchDepth = (typeof tavilySearchDepths)[number]

const anySearchFreshnessValues = ['', 'day', 'week', 'month', 'year'] as const
type AnySearchFreshness = (typeof anySearchFreshnessValues)[number]

function normalizeVisionAssistEndpointMode(
  value: unknown
): VisionAssistEndpointMode {
  const endpointMode = String(value || '')
  return visionAssistEndpointModes.includes(
    endpointMode as VisionAssistEndpointMode
  )
    ? (endpointMode as VisionAssistEndpointMode)
    : 'auto'
}

function normalizeWebSearchProvider(value: unknown): WebSearchProvider {
  const provider = String(value || '')
  return webSearchProviders.includes(provider as WebSearchProvider)
    ? (provider as WebSearchProvider)
    : 'tavily'
}

function normalizeTavilySearchDepth(value: unknown): TavilySearchDepth {
  const depth = String(value || '')
  return tavilySearchDepths.includes(depth as TavilySearchDepth)
    ? (depth as TavilySearchDepth)
    : 'basic'
}

function normalizeAnySearchFreshness(value: unknown): AnySearchFreshness {
  const freshness = String(value || '')
  return anySearchFreshnessValues.includes(freshness as AnySearchFreshness)
    ? (freshness as AnySearchFreshness)
    : ''
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

export const channelFormSchema = z
  .object({
    name: z.string().min(1, ERROR_MESSAGES.REQUIRED_NAME),
    type: z.number().min(0, ERROR_MESSAGES.REQUIRED_TYPE),
    base_url: z.string().optional(),
    key: z.string(),
    openai_organization: z.string().optional(),
    models: z.string().min(1, ERROR_MESSAGES.REQUIRED_MODELS),
    group: z.array(z.string()).min(1, ERROR_MESSAGES.REQUIRED_GROUP),
    model_mapping: z
      .string()
      .optional()
      .refine(
        isOptionalModelMapping,
        'Model mapping must be a JSON object with string values'
      ),
    priority: z.number().optional(),
    weight: z.number().optional(),
    test_model: z.string().optional(),
    auto_ban: z.number().optional(),
    status: z.number(),
    status_code_mapping: z
      .string()
      .optional()
      .refine(
        isOptionalStatusCodeMapping,
        'Status code mapping must use valid HTTP status codes'
      ),
    tag: z.string().optional(),
    remark: z
      .string()
      .max(255, 'Remark must be less than 255 characters')
      .optional(),
    setting: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    param_override: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    header_override: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    settings: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    advanced_custom: z.string().optional(),
    other: z.string().optional(),
    // Multi-key options (not sent to backend directly)
    multi_key_mode: z.enum(['single', 'batch', 'multi_to_single']).optional(),
    multi_key_type: z.enum(['random', 'polling']).optional(),
    batch_add_set_key_prefix_2_name: z.boolean().optional(),
    key_mode: z.enum(['append', 'replace']).optional(), // For editing multi-key channels
    // Channel extra settings (stored in setting JSON, not sent directly)
    force_format: z.boolean().optional(),
    thinking_to_content: z.boolean().optional(),
    proxy: z.string().optional(),
    pass_through_body_enabled: z.boolean().optional(),
    responses_compact_passthrough_enabled: z.boolean().optional(),
    use_upstream_model_for_billing: z.boolean().optional(),
    system_prompt: z.string().optional(),
    system_prompt_override: z.boolean().optional(),
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
    // Type-specific settings (stored in settings JSON)
    is_enterprise_account: z.boolean().optional(), // OpenRouter specific
    vertex_key_type: z.enum(['json', 'api_key']).optional(), // Vertex AI specific
    aws_key_type: z.enum(['ak_sk', 'api_key']).optional(), // AWS specific
    azure_responses_version: z.string().optional(), // Azure specific
    // Field passthrough controls (stored in settings JSON)
    allow_service_tier: z.boolean().optional(), // OpenAI/Anthropic
    disable_store: z.boolean().optional(), // OpenAI only
    allow_safety_identifier: z.boolean().optional(), // OpenAI only
    allow_include_obfuscation: z.boolean().optional(), // OpenAI: include usage obfuscation
    allow_inference_geo: z.boolean().optional(), // OpenAI/Anthropic: inference geography
    allow_speed: z.boolean().optional(), // Anthropic: speed mode control
    claude_beta_query: z.boolean().optional(), // Anthropic: beta query passthrough
    disable_task_polling_sleep: z.boolean().optional(),
    // Upstream model update settings (stored in settings JSON)
    upstream_model_update_check_enabled: z.boolean().optional(),
    upstream_model_update_auto_sync_enabled: z.boolean().optional(),
    upstream_model_update_ignored_models: z.string().optional(),
  })
  .superRefine((data, ctx) => {
    if ([3, 8, 36, 45].includes(data.type) && !data.base_url?.trim()) {
      addRequiredIssue(
        ctx,
        'base_url',
        'Base URL is required for this channel type'
      )
    }

    if (data.type === CHANNEL_TYPE_ADVANCED_CUSTOM) {
      const advancedCustomConfig = parseAdvancedCustomConfig(
        data.advanced_custom
      )
      const advancedCustomError =
        validateAdvancedCustomConfig(advancedCustomConfig)
      if (advancedCustomError) {
        addRequiredIssue(ctx, 'advanced_custom', advancedCustomError.message)
      }
      if (
        advancedCustomConfigUsesRelativeUpstreamPath(advancedCustomConfig) &&
        !data.base_url?.trim()
      ) {
        addRequiredIssue(
          ctx,
          'base_url',
          'Base URL is required when an advanced route uses an upstream path'
        )
      }
    }

    if ([3, 18, 21, 39, 41, 49].includes(data.type) && !data.other?.trim()) {
      addRequiredIssue(
        ctx,
        'other',
        'This channel type requires additional configuration'
      )
    }

    if (data.type === 57) {
      if (data.multi_key_mode && data.multi_key_mode !== 'single') {
        addRequiredIssue(
          ctx,
          'multi_key_mode',
          'Codex channels do not support batch creation'
        )
      }
      if (data.key?.trim() && !isCodexCredential(data.key)) {
        addRequiredIssue(
          ctx,
          'key',
          'Codex credential must be a JSON object with access_token and account_id'
        )
      }
    }

    if (
      data.type === 41 &&
      data.vertex_key_type === 'json' &&
      data.key?.trim() &&
      !isVertexJsonKey(data.key)
    ) {
      addRequiredIssue(
        ctx,
        'key',
        'Vertex AI service account key must be valid JSON'
      )
    }

    if (
      data.type === 41 &&
      data.vertex_key_type === 'api_key' &&
      data.multi_key_mode &&
      data.multi_key_mode !== 'single'
    ) {
      addRequiredIssue(
        ctx,
        'multi_key_mode',
        'Vertex AI API Key mode does not support batch creation'
      )
    }

    if (data.vision_assist_enabled === true) {
      if (
        !data.vision_assist_channel_id ||
        data.vision_assist_channel_id <= 0
      ) {
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
  })

export type ChannelFormValues = z.infer<typeof channelFormSchema>

// ============================================================================
// Default Form Values
// ============================================================================

export const CHANNEL_FORM_DEFAULT_VALUES: ChannelFormValues = {
  name: '',
  type: 1,
  base_url: '',
  key: '',
  openai_organization: '',
  models: '',
  group: ['default'],
  model_mapping: '',
  priority: 0,
  weight: 0,
  test_model: '',
  auto_ban: 1,
  status: CHANNEL_STATUS.ENABLED,
  status_code_mapping: '',
  tag: '',
  remark: '',
  setting: '',
  param_override: '',
  header_override: '',
  settings: '{}',
  other: '',
  multi_key_mode: 'single',
  multi_key_type: 'random',
  batch_add_set_key_prefix_2_name: false,
  key_mode: 'append',
  // Channel extra settings
  force_format: false,
  thinking_to_content: false,
  proxy: '',
  pass_through_body_enabled: false,
  responses_compact_passthrough_enabled: false,
  use_upstream_model_for_billing: false,
  system_prompt: '',
  system_prompt_override: false,
  vision_assist_enabled: false,
  vision_assist_channel_id: 0,
  vision_assist_model: '',
  vision_assist_target_models: '',
  vision_assist_prompt: '',
  vision_assist_cache_ttl_seconds: 86400,
  vision_assist_failure_policy: 'error',
  vision_assist_strip_image: true,
  vision_assist_endpoint_mode: 'auto',
  vision_assist_max_concurrency: 2,
  vision_assist_retry_count: 1,
  vision_assist_retry_backoff_ms: 500,
  web_search_enabled: false,
  web_search_provider: 'tavily',
  web_search_api_key: '',
  web_search_api_key_configured: false,
  web_search_clear_api_key: false,
  web_search_max_results: 5,
  web_search_search_depth: 'basic',
  web_search_freshness: '',
  web_search_content_types: '',
  // Type-specific settings
  is_enterprise_account: false,
  vertex_key_type: 'json',
  aws_key_type: 'ak_sk',
  azure_responses_version: '',
  // Field passthrough controls
  allow_service_tier: false,
  disable_store: false,
  allow_safety_identifier: false,
  allow_include_obfuscation: false,
  allow_inference_geo: false,
  allow_speed: false,
  claude_beta_query: false,
  disable_task_polling_sleep: false,
  upstream_model_update_check_enabled: false,
  upstream_model_update_auto_sync_enabled: false,
  upstream_model_update_ignored_models: '',
  advanced_custom: '',
}

// ============================================================================
// Transform Functions
// ============================================================================

/**
 * Transform Channel from API to Form default values
 */
export function transformChannelToFormDefaults(
  channel: Channel
): ChannelFormValues {
  // Parse channel extra settings from setting field
  let extraSettings = {
    force_format: false,
    thinking_to_content: false,
    proxy: '',
    pass_through_body_enabled: false,
    responses_compact_passthrough_enabled: false,
    use_upstream_model_for_billing: false,
    system_prompt: '',
    system_prompt_override: false,
    vision_assist_enabled: false,
    vision_assist_channel_id: 0,
    vision_assist_model: '',
    vision_assist_target_models: '',
    vision_assist_prompt: '',
    vision_assist_cache_ttl_seconds: 86400,
    vision_assist_failure_policy: 'error' as 'error' | 'skip',
    vision_assist_strip_image: true,
    vision_assist_endpoint_mode: 'auto' as VisionAssistEndpointMode,
    vision_assist_max_concurrency: 2,
    vision_assist_retry_count: 1,
    vision_assist_retry_backoff_ms: 500,
    web_search_enabled: false,
    web_search_provider: 'tavily' as WebSearchProvider,
    web_search_api_key: '',
    web_search_api_key_configured: false,
    web_search_clear_api_key: false,
    web_search_max_results: 5,
    web_search_search_depth: 'basic' as TavilySearchDepth,
    web_search_freshness: '' as AnySearchFreshness,
    web_search_content_types: '',
  }

  if (channel.setting) {
    try {
      const parsed = JSON.parse(channel.setting)
      extraSettings = {
        force_format: parsed.force_format || false,
        thinking_to_content: parsed.thinking_to_content || false,
        proxy: parsed.proxy || '',
        pass_through_body_enabled: parsed.pass_through_body_enabled || false,
        responses_compact_passthrough_enabled:
          parsed.responses_compact_passthrough_enabled === true,
        use_upstream_model_for_billing:
          parsed.use_upstream_model_for_billing === true,
        system_prompt: parsed.system_prompt || '',
        system_prompt_override: parsed.system_prompt_override || false,
        vision_assist_enabled: parsed.vision_assist?.enabled === true,
        vision_assist_channel_id:
          Number(parsed.vision_assist?.assist_channel_id) || 0,
        vision_assist_model: parsed.vision_assist?.assist_model || '',
        vision_assist_target_models: Array.isArray(
          parsed.vision_assist?.target_models
        )
          ? parsed.vision_assist.target_models.join(',')
          : '',
        vision_assist_prompt: parsed.vision_assist?.prompt || '',
        vision_assist_cache_ttl_seconds: minNumberOrDefault(
          parsed.vision_assist?.cache_ttl_seconds,
          0,
          86400
        ),
        vision_assist_failure_policy:
          parsed.vision_assist?.failure_policy === 'skip' ? 'skip' : 'error',
        vision_assist_strip_image:
          parsed.vision_assist?.strip_image === undefined
            ? true
            : parsed.vision_assist.strip_image !== false,
        vision_assist_endpoint_mode: normalizeVisionAssistEndpointMode(
          parsed.vision_assist?.endpoint_mode
        ),
        vision_assist_max_concurrency: minNumberOrDefault(
          parsed.vision_assist?.max_concurrency,
          1,
          2
        ),
        vision_assist_retry_count: minNumberOrDefault(
          parsed.vision_assist?.retry_count,
          0,
          1
        ),
        vision_assist_retry_backoff_ms: minNumberOrDefault(
          parsed.vision_assist?.retry_backoff_ms,
          1,
          500
        ),
        web_search_enabled: parsed.web_search?.enabled === true,
        web_search_provider: normalizeWebSearchProvider(
          parsed.web_search?.provider
        ),
        web_search_api_key: '',
        web_search_api_key_configured:
          parsed.web_search?.api_key_configured === true,
        web_search_clear_api_key: false,
        web_search_max_results: minNumberOrDefault(
          parsed.web_search?.max_results,
          1,
          5
        ),
        web_search_search_depth: normalizeTavilySearchDepth(
          parsed.web_search?.search_depth
        ),
        web_search_freshness: normalizeAnySearchFreshness(
          parsed.web_search?.freshness
        ),
        web_search_content_types: Array.isArray(
          parsed.web_search?.content_types
        )
          ? parsed.web_search.content_types.join(',')
          : '',
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel setting:', error)
    }
  }

  // Parse type-specific settings from settings field
  let vertexKeyType: 'json' | 'api_key' = 'json'
  let azureResponsesVersion = ''
  let isEnterpriseAccount = false
  let awsKeyType: 'ak_sk' | 'api_key' = 'ak_sk'
  let allowServiceTier = false
  let disableStore = false
  let allowSafetyIdentifier = false
  let allowIncludeObfuscation = false
  let allowInferenceGeo = false
  let allowSpeed = false
  let claudeBetaQuery = false
  let disableTaskPollingSleep = false
  let upstreamModelUpdateCheckEnabled = false
  let upstreamModelUpdateAutoSyncEnabled = false
  let upstreamModelUpdateIgnoredModels = ''
  let advancedCustom = ''

  if (channel.settings) {
    try {
      const parsed = JSON.parse(channel.settings)
      vertexKeyType = parsed.vertex_key_type || 'json'
      azureResponsesVersion = parsed.azure_responses_version || ''
      isEnterpriseAccount = parsed.openrouter_enterprise === true
      awsKeyType = parsed.aws_key_type || 'ak_sk'
      allowServiceTier = parsed.allow_service_tier === true
      disableStore = parsed.disable_store === true
      allowSafetyIdentifier = parsed.allow_safety_identifier === true
      allowIncludeObfuscation = parsed.allow_include_obfuscation === true
      allowInferenceGeo = parsed.allow_inference_geo === true
      allowSpeed = parsed.allow_speed === true
      claudeBetaQuery = parsed.claude_beta_query === true
      disableTaskPollingSleep = parsed.disable_task_polling_sleep === true
      upstreamModelUpdateCheckEnabled =
        parsed.upstream_model_update_check_enabled === true
      upstreamModelUpdateAutoSyncEnabled =
        parsed.upstream_model_update_auto_sync_enabled === true
      upstreamModelUpdateIgnoredModels = Array.isArray(
        parsed.upstream_model_update_ignored_models
      )
        ? parsed.upstream_model_update_ignored_models.join(',')
        : ''
      if (parsed.advanced_custom) {
        advancedCustom = stringifyAdvancedCustomConfig(parsed.advanced_custom)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel settings:', error)
    }
  }

  return {
    name: channel.name || '',
    type: channel.type,
    base_url: channel.base_url || '',
    key: '', // Never populate key from backend for security
    openai_organization: channel.openai_organization || '',
    models: channel.models || '',
    group: parseGroups(channel.group || 'default'),
    model_mapping: channel.model_mapping || '',
    priority: channel.priority || 0,
    weight: channel.weight || 0,
    test_model: channel.test_model || '',
    auto_ban: channel.auto_ban ?? 1,
    status: channel.status,
    status_code_mapping: channel.status_code_mapping || '',
    tag: channel.tag || '',
    remark: channel.remark || '',
    setting: channel.setting || '',
    param_override: channel.param_override || '',
    header_override: channel.header_override || '',
    settings: channel.settings || '{}',
    other: channel.other || '',
    multi_key_mode: 'single',
    multi_key_type: channel.channel_info.multi_key_mode || 'random',
    batch_add_set_key_prefix_2_name: false,
    key_mode: 'append', // Default to append mode for editing multi-key channels
    // Channel extra settings
    ...extraSettings,
    // Type-specific settings
    is_enterprise_account: isEnterpriseAccount,
    vertex_key_type: vertexKeyType,
    azure_responses_version: azureResponsesVersion,
    aws_key_type: awsKeyType,
    allow_service_tier: allowServiceTier,
    disable_store: disableStore,
    allow_include_obfuscation: allowIncludeObfuscation,
    allow_inference_geo: allowInferenceGeo,
    allow_speed: allowSpeed,
    claude_beta_query: claudeBetaQuery,
    disable_task_polling_sleep: disableTaskPollingSleep,
    allow_safety_identifier: allowSafetyIdentifier,
    upstream_model_update_check_enabled: upstreamModelUpdateCheckEnabled,
    upstream_model_update_auto_sync_enabled: upstreamModelUpdateAutoSyncEnabled,
    upstream_model_update_ignored_models: upstreamModelUpdateIgnoredModels,
    advanced_custom: advancedCustom,
  }
}

/**
 * Build the setting JSON string from form extra settings
 */
function buildSettingJSON(formData: ChannelFormValues): string {
  let existingSettings: Record<string, unknown> = {}
  if (formData.setting) {
    try {
      const parsed = JSON.parse(formData.setting)
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        existingSettings = parsed
      }
    } catch {
      existingSettings = {}
    }
  }
  const settingObj = {
    ...existingSettings,
    force_format: formData.force_format || false,
    thinking_to_content: formData.thinking_to_content || false,
    proxy: formData.proxy || '',
    pass_through_body_enabled: formData.pass_through_body_enabled || false,
    responses_compact_passthrough_enabled:
      formData.responses_compact_passthrough_enabled === true,
    use_upstream_model_for_billing:
      formData.use_upstream_model_for_billing === true,
    system_prompt: formData.system_prompt || '',
    system_prompt_override: formData.system_prompt_override || false,
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
  return JSON.stringify(settingObj)
}

/**
 * Build the settings JSON string (for type-specific config like vertex_key_type)
 */
function buildSettingsJSON(formData: ChannelFormValues): string {
  let settingsObj: Record<string, unknown> = {}

  // Try to parse existing settings first
  if (formData.settings && formData.settings !== '{}') {
    try {
      settingsObj = JSON.parse(formData.settings)
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse existing settings:', error)
    }
  }

  // Add vertex_key_type for Vertex AI channels (type 41)
  if (formData.type === 41) {
    settingsObj.vertex_key_type = formData.vertex_key_type || 'json'
  } else if ('vertex_key_type' in settingsObj) {
    delete settingsObj.vertex_key_type
  }

  // Add azure_responses_version for Azure channels (type 3)
  if (formData.type === 3 && formData.azure_responses_version) {
    settingsObj.azure_responses_version = formData.azure_responses_version
  } else if ('azure_responses_version' in settingsObj) {
    delete settingsObj.azure_responses_version
  }

  // Add enterprise account setting for OpenRouter (type 20)
  if (formData.type === 20) {
    settingsObj.openrouter_enterprise = formData.is_enterprise_account === true
  } else if ('openrouter_enterprise' in settingsObj) {
    delete settingsObj.openrouter_enterprise
  }

  // Add aws_key_type for AWS channels (type 33)
  if (formData.type === 33) {
    settingsObj.aws_key_type = formData.aws_key_type || 'ak_sk'
  } else if ('aws_key_type' in settingsObj) {
    delete settingsObj.aws_key_type
  }

  // Field passthrough controls:
  // - OpenAI (type 1) and Anthropic (type 14): allow_service_tier
  // - OpenAI only: disable_store, allow_safety_identifier
  if (formData.type === 1 || formData.type === 14 || formData.type === 57) {
    settingsObj.allow_service_tier = formData.allow_service_tier === true
  } else if ('allow_service_tier' in settingsObj) {
    delete settingsObj.allow_service_tier
  }

  if (formData.type === 1 || formData.type === 57) {
    settingsObj.disable_store = formData.disable_store === true
    settingsObj.allow_safety_identifier =
      formData.allow_safety_identifier === true
    settingsObj.allow_include_obfuscation =
      formData.allow_include_obfuscation === true
    settingsObj.allow_inference_geo = formData.allow_inference_geo === true
  } else {
    if ('disable_store' in settingsObj) delete settingsObj.disable_store
    if ('allow_safety_identifier' in settingsObj)
      delete settingsObj.allow_safety_identifier
    if ('allow_include_obfuscation' in settingsObj)
      delete settingsObj.allow_include_obfuscation
    if (formData.type !== 14 && 'allow_inference_geo' in settingsObj)
      delete settingsObj.allow_inference_geo
  }

  // Anthropic (type 14): claude_beta_query, allow_inference_geo, allow_speed
  if (formData.type === 14) {
    settingsObj.allow_inference_geo = formData.allow_inference_geo === true
    settingsObj.allow_speed = formData.allow_speed === true
    settingsObj.claude_beta_query = formData.claude_beta_query === true
  } else {
    if ('allow_speed' in settingsObj) delete settingsObj.allow_speed
    if ('claude_beta_query' in settingsObj) delete settingsObj.claude_beta_query
  }

  settingsObj.disable_task_polling_sleep =
    formData.disable_task_polling_sleep === true

  // Upstream model update settings (for model-fetchable channel types)
  if (MODEL_FETCHABLE_TYPES.has(formData.type)) {
    settingsObj.upstream_model_update_check_enabled =
      formData.upstream_model_update_check_enabled === true
    settingsObj.upstream_model_update_auto_sync_enabled =
      settingsObj.upstream_model_update_check_enabled === true &&
      formData.upstream_model_update_auto_sync_enabled === true
    settingsObj.upstream_model_update_ignored_models = Array.from(
      new Set(
        String(formData.upstream_model_update_ignored_models || '')
          .split(',')
          .map((model) => model.trim())
          .filter(Boolean)
      )
    )
    if (
      !Array.isArray(settingsObj.upstream_model_update_last_detected_models) ||
      settingsObj.upstream_model_update_check_enabled !== true
    ) {
      settingsObj.upstream_model_update_last_detected_models = []
    }
    if (typeof settingsObj.upstream_model_update_last_check_time !== 'number') {
      settingsObj.upstream_model_update_last_check_time = 0
    }
  }

  if (formData.type === CHANNEL_TYPE_ADVANCED_CUSTOM) {
    const advancedCustomConfig = parseAdvancedCustomConfig(
      formData.advanced_custom
    )
    if (advancedCustomConfig) {
      settingsObj.advanced_custom = advancedCustomConfig
    }
  } else if ('advanced_custom' in settingsObj) {
    delete settingsObj.advanced_custom
  }

  return JSON.stringify(settingsObj)
}

function normalizeBaseUrl(value: string | undefined): string {
  return String(value || '')
    .trim()
    .replace(/\/+$/, '')
}

/**
 * Transform form data to API payload for creating channel
 */
export function transformFormDataToCreatePayload(formData: ChannelFormValues): {
  mode: 'single' | 'batch' | 'multi_to_single'
  multi_key_mode?: 'random' | 'polling'
  batch_add_set_key_prefix_2_name?: boolean
  channel: Partial<Channel>
} {
  const mode = formData.multi_key_mode || 'single'

  const channel: Partial<Channel> = {
    name: formData.name,
    type: formData.type,
    base_url: normalizeBaseUrl(formData.base_url) || null,
    key: formData.key,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority || null,
    weight: formData.weight || null,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status: formData.status,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Clean up empty strings to null for optional fields
  Object.keys(channel).forEach((key) => {
    if (channel[key as keyof typeof channel] === '') {
      ;(channel as Record<string, unknown>)[key] = null
    }
  })

  return {
    mode,
    multi_key_mode:
      mode === 'multi_to_single' ? formData.multi_key_type : undefined,
    batch_add_set_key_prefix_2_name:
      mode === 'batch' ? formData.batch_add_set_key_prefix_2_name : undefined,
    channel,
  }
}

/**
 * Transform form data to API payload for updating channel
 */
export function transformFormDataToUpdatePayload(
  formData: ChannelFormValues,
  channelId: number
): Partial<Channel> {
  const payload: Partial<Channel> = {
    id: channelId,
    name: formData.name,
    type: formData.type,
    base_url: normalizeBaseUrl(formData.base_url) || null,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority ?? 0,
    weight: formData.weight ?? 0,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Only include key if it was changed (not empty)
  if (formData.key && formData.key.trim()) {
    payload.key = formData.key
  }

  // Clean up empty strings to null for optional fields
  Object.keys(payload).forEach((key) => {
    if (payload[key as keyof typeof payload] === '') {
      ;(payload as Record<string, unknown>)[key] = null
    }
  })

  // Send explicit empty strings for nullable fields so GORM updates can clear them.
  payload.base_url = normalizeBaseUrl(formData.base_url) || ''
  payload.openai_organization = formData.openai_organization || ''
  payload.test_model = formData.test_model || ''
  payload.tag = formData.tag || ''
  payload.remark = formData.remark || ''
  payload.model_mapping = formData.model_mapping || ''
  payload.status_code_mapping = formData.status_code_mapping || ''
  payload.param_override = formData.param_override || ''
  payload.header_override = formData.header_override || ''

  return payload
}

// ============================================================================
// Validation Helpers
// ============================================================================

/**
 * Validate JSON string
 */
export function validateJSON(value: string): boolean {
  if (!value || value.trim() === '') return true
  try {
    JSON.parse(value)
    return true
  } catch {
    return false
  }
}

/**
 * Validate model mapping format
 */
export function validateModelMapping(value: string): boolean {
  if (!value || value.trim() === '') return true
  return validateJSON(value)
}

/**
 * Parse models string to array
 */
export function parseModels(models: string): string[] {
  if (!models) return []
  return models
    .split(',')
    .map((m) => m.trim())
    .filter((m) => m.length > 0)
}

/**
 * Parse groups string to array
 */
export function parseGroups(groups: string): string[] {
  if (!groups) return []
  return groups
    .split(',')
    .map((g) => g.trim())
    .filter((g) => g.length > 0)
}

/**
 * Format models array to string
 */
export function formatModels(models: string[]): string {
  return models.join(',')
}

/**
 * Format groups array to string
 */
export function formatGroups(groups: string[]): string {
  return groups.join(',')
}
