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
import type { FieldPath } from 'react-hook-form'

import type { ChannelFormValues } from './channel-form'

type ChannelFormErrorMap = Partial<
  Record<FieldPath<ChannelFormValues>, unknown>
>

const ADVANCED_SETTINGS_FIELDS = new Set<FieldPath<ChannelFormValues>>([
  'priority',
  'weight',
  'user_concurrency_limit',
  'test_model',
  'auto_ban',
  'tag',
  'remark',
  'param_override',
  'header_override',
  'status_code_mapping',
  'advanced_custom',
  'force_format',
  'thinking_to_content',
  'pass_through_body_enabled',
  'use_upstream_model_for_billing',
  'proxy',
  'http_protocol',
  'http2_connection_shards',
  'system_prompt',
  'system_prompt_override',
  'vision_assist_enabled',
  'vision_assist_channel_id',
  'vision_assist_model',
  'vision_assist_target_models',
  'vision_assist_prompt',
  'vision_assist_cache_ttl_seconds',
  'vision_assist_failure_policy',
  'vision_assist_strip_image',
  'vision_assist_endpoint_mode',
  'vision_assist_multi_image_mode',
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
  'allow_service_tier',
  'disable_store',
  'allow_safety_identifier',
  'allow_include_obfuscation',
  'allow_inference_geo',
  'allow_speed',
  'claude_beta_query',
  'disable_task_polling_sleep',
  'upstream_model_update_check_enabled',
  'upstream_model_update_auto_sync_enabled',
  'upstream_model_update_ignored_models',
])

export function isAdvancedSettingsField(
  fieldName: string
): fieldName is FieldPath<ChannelFormValues> {
  return ADVANCED_SETTINGS_FIELDS.has(fieldName as FieldPath<ChannelFormValues>)
}

export function hasAdvancedSettingsErrors(
  errors: ChannelFormErrorMap
): boolean {
  return Object.keys(errors).some((fieldName) =>
    isAdvancedSettingsField(fieldName)
  )
}
