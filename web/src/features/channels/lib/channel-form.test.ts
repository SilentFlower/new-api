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
import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  getEditableQuotaStep,
  quotaUnitsToDollars,
  quotaUnitsToEditableAmount,
} from '@/lib/format'

import type { Channel } from '../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from './channel-form'

function createChannel(setting: string, settings = '{}'): Channel {
  return {
    id: 1,
    type: 1,
    key: '',
    openai_organization: null,
    test_model: null,
    status: 1,
    name: 'compact-test',
    weight: 0,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    base_url: '',
    other: '',
    balance: 0,
    balance_updated_time: 0,
    models: 'gpt-5.6',
    group: 'default',
    used_quota: 0,
    model_mapping: null,
    status_code_mapping: null,
    priority: 0,
    auto_ban: 1,
    user_concurrency_limit: 0,
    other_info: '',
    tag: null,
    setting,
    param_override: null,
    header_override: null,
    remark: '',
    max_input_tokens: 0,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    settings,
  }
}

test('渠道单用户并发限制保持 API 与表单往返一致', () => {
  const channel = createChannel('{}')
  channel.user_concurrency_limit = 4

  const defaults = transformChannelToFormDefaults(channel)
  assert.equal(defaults.user_concurrency_limit, 4)

  const payload = transformFormDataToUpdatePayload(defaults, channel.id)
  assert.equal(payload.user_concurrency_limit, 4)

  channel.user_concurrency_limit = null
  const historicalDefaults = transformChannelToFormDefaults(channel)
  assert.equal(historicalDefaults.user_concurrency_limit, 0)
  assert.equal(
    transformFormDataToCreatePayload(historicalDefaults).channel
      .user_concurrency_limit,
    0
  )
  assert.equal(
    transformFormDataToUpdatePayload(historicalDefaults, channel.id)
      .user_concurrency_limit,
    0
  )
})

test('渠道单用户并发限制拒绝非法边界', () => {
  for (const invalidValue of [-1, 1.5, 1001]) {
    const result = channelFormSchema.safeParse({
      ...transformChannelToFormDefaults(createChannel('{}')),
      user_concurrency_limit: invalidValue,
    })
    assert.equal(result.success, false)
  }
})

test('渠道单用户每日额度按显示金额与内部额度往返', () => {
  const channel = createChannel('{}')
  channel.user_daily_quota_limit = 500000

  const defaults = transformChannelToFormDefaults(channel)
  assert.equal(
    defaults.user_daily_quota_limit,
    quotaUnitsToEditableAmount(500000)
  )
  assert.equal(
    transformFormDataToUpdatePayload(defaults, channel.id)
      .user_daily_quota_limit,
    500000
  )

  channel.user_daily_quota_limit = null
  const historicalDefaults = transformChannelToFormDefaults(channel)
  assert.equal(historicalDefaults.user_daily_quota_limit, 0)
  assert.equal(
    transformFormDataToCreatePayload(historicalDefaults).channel
      .user_daily_quota_limit,
    0
  )
})

test('渠道单用户每日额度使用稳定编辑精度并允许空值提交为零', () => {
  const channel = createChannel('{}')
  channel.user_daily_quota_limit = 299999950
  assert.notEqual(quotaUnitsToDollars(299999950), 600)

  const defaults = transformChannelToFormDefaults(channel)
  assert.equal(defaults.user_daily_quota_limit, 600)
  assert.equal(String(getEditableQuotaStep()), '0.0001')

  const result = channelFormSchema.safeParse({
    ...defaults,
    user_daily_quota_limit: '',
  })
  assert.equal(result.success, true)
  if (result.success) {
    assert.equal(result.data.user_daily_quota_limit, 0)
  }
})

test('渠道单用户每日额度拒绝负数和超出内部上限的金额', () => {
  for (const invalidValue of [-1, quotaUnitsToDollars(2147483647 + 1000000)]) {
    const result = channelFormSchema.safeParse({
      ...transformChannelToFormDefaults(createChannel('{}')),
      user_daily_quota_limit: invalidValue,
    })
    assert.equal(result.success, false)
  }
})

test('渠道单用户每周额度按显示金额与内部额度往返并校验边界', () => {
  const channel = createChannel('{}')
  channel.user_weekly_quota_limit = 2_500_000

  const defaults = transformChannelToFormDefaults(channel)
  assert.equal(
    defaults.user_weekly_quota_limit,
    quotaUnitsToEditableAmount(2_500_000)
  )
  assert.equal(
    transformFormDataToUpdatePayload(defaults, channel.id)
      .user_weekly_quota_limit,
    2_500_000
  )

  const emptyResult = channelFormSchema.safeParse({
    ...defaults,
    user_weekly_quota_limit: '',
  })
  assert.equal(emptyResult.success, true)
  if (emptyResult.success) {
    assert.equal(emptyResult.data.user_weekly_quota_limit, 0)
  }

  for (const invalidValue of [-1, quotaUnitsToDollars(2147483647 + 1000000)]) {
    const result = channelFormSchema.safeParse({
      ...defaults,
      user_weekly_quota_limit: invalidValue,
    })
    assert.equal(result.success, false)
  }
})

test('Responses Compact 透传开关保持 setting JSON 往返兼容', () => {
  const channel = createChannel('{"future_flag":"keep"}')
  const defaults = transformChannelToFormDefaults(channel)

  assert.equal(defaults.responses_compact_passthrough_enabled, false)

  const payload = transformFormDataToUpdatePayload(
    {
      ...defaults,
      responses_compact_passthrough_enabled: true,
    },
    channel.id
  )
  const setting = JSON.parse(payload.setting || '{}') as Record<string, unknown>

  assert.equal(setting.responses_compact_passthrough_enabled, true)
  assert.equal(setting.future_flag, 'keep')
})

test('Responses Compact 透传开关能从已有渠道设置恢复', () => {
  const channel = createChannel(
    '{"responses_compact_passthrough_enabled":true}'
  )

  assert.equal(
    transformChannelToFormDefaults(channel)
      .responses_compact_passthrough_enabled,
    true
  )
})

test('Build 渠道设置保持未知字段和 WebSearch API Key 状态', () => {
  const channel = createChannel(
    JSON.stringify({
      future_flag: 'keep',
      web_search: {
        enabled: true,
        provider: 'tavily',
        api_key_configured: true,
        future_web_search_flag: 'keep',
      },
      vision_assist: {
        enabled: true,
        assist_channel_id: 12,
        assist_model: 'gpt-4o-mini',
        target_models: ['gpt-4o'],
        multi_image_mode: 'combined',
        future_vision_flag: 'keep',
      },
    })
  )
  const defaults = transformChannelToFormDefaults(channel)

  assert.equal(defaults.web_search_api_key, '')
  assert.equal(defaults.web_search_api_key_configured, true)
  assert.equal(defaults.web_search_clear_api_key, false)
  assert.equal(defaults.vision_assist_channel_id, 12)
  assert.equal(defaults.vision_assist_target_models, 'gpt-4o')
  assert.equal(defaults.vision_assist_multi_image_mode, 'combined')
  assert.equal(defaults.vision_assist_combined_max_images, 5)

  const payload = transformFormDataToUpdatePayload(defaults, channel.id)
  const setting = JSON.parse(payload.setting || '{}') as Record<string, unknown>
  const webSearch = setting.web_search as Record<string, unknown>
  const visionAssist = setting.vision_assist as Record<string, unknown>

  assert.equal(setting.future_flag, 'keep')
  assert.equal(webSearch.future_web_search_flag, 'keep')
  assert.equal(webSearch.api_key, undefined)
  assert.equal(webSearch.clear_api_key, false)
  assert.equal(visionAssist.future_vision_flag, 'keep')
  assert.equal(visionAssist.multi_image_mode, 'combined')
  assert.equal(visionAssist.combined_max_images, 5)
})

test('新建渠道默认使用合并识别', () => {
  assert.equal(
    CHANNEL_FORM_DEFAULT_VALUES.vision_assist_multi_image_mode,
    'combined'
  )
})

test('视觉辅助多图模式缺失或非法时保持逐张识别', () => {
  const empty = transformChannelToFormDefaults(createChannel(''))
  const historical = transformChannelToFormDefaults(
    createChannel('{"vision_assist":{"enabled":true}}')
  )
  const invalid = transformChannelToFormDefaults(
    createChannel(
      '{"vision_assist":{"enabled":true,"multi_image_mode":"invalid"}}'
    )
  )

  assert.equal(empty.vision_assist_multi_image_mode, 'separate')
  assert.equal(historical.vision_assist_multi_image_mode, 'separate')
  assert.equal(invalid.vision_assist_multi_image_mode, 'separate')
})

test('视觉辅助合并单批图片数默认值、边界和往返保持一致', () => {
  const empty = transformChannelToFormDefaults(createChannel(''))
  const invalid = transformChannelToFormDefaults(
    createChannel(
      '{"vision_assist":{"multi_image_mode":"combined","combined_max_images":65}}'
    )
  )
  const fractional = transformChannelToFormDefaults(
    createChannel(
      '{"vision_assist":{"multi_image_mode":"combined","combined_max_images":2.5}}'
    )
  )
  const configured = transformChannelToFormDefaults(
    createChannel(
      '{"vision_assist":{"multi_image_mode":"combined","combined_max_images":12}}'
    )
  )

  assert.equal(empty.vision_assist_combined_max_images, 5)
  assert.equal(invalid.vision_assist_combined_max_images, 5)
  assert.equal(fractional.vision_assist_combined_max_images, 5)
  assert.equal(configured.vision_assist_combined_max_images, 12)

  const payload = transformFormDataToUpdatePayload(configured, 1)
  const setting = JSON.parse(payload.setting || '{}') as Record<string, unknown>
  const visionAssist = setting.vision_assist as Record<string, unknown>
  assert.equal(visionAssist.combined_max_images, 12)

  const invalidSchemaResult = channelFormSchema.safeParse({
    ...configured,
    vision_assist_combined_max_images: 2.5,
  })
  assert.equal(invalidSchemaResult.success, false)
})

test('Build WebSearch 支持清空旧 Key 或替换为新 Key', () => {
  const channel = createChannel(
    '{"web_search":{"enabled":true,"provider":"anysearch","api_key_configured":true}}'
  )
  const defaults = transformChannelToFormDefaults(channel)

  const clearPayload = transformFormDataToUpdatePayload(
    {
      ...defaults,
      web_search_clear_api_key: true,
      web_search_api_key: '',
    },
    channel.id
  )
  const clearSetting = JSON.parse(clearPayload.setting || '{}') as Record<
    string,
    unknown
  >
  const clearWebSearch = clearSetting.web_search as Record<string, unknown>
  assert.equal(clearWebSearch.clear_api_key, true)
  assert.equal(clearWebSearch.api_key, undefined)

  const replacePayload = transformFormDataToUpdatePayload(
    {
      ...defaults,
      web_search_clear_api_key: true,
      web_search_api_key: 'new-key',
    },
    channel.id
  )
  const replaceSetting = JSON.parse(replacePayload.setting || '{}') as Record<
    string,
    unknown
  >
  const replaceWebSearch = replaceSetting.web_search as Record<string, unknown>
  assert.equal(replaceWebSearch.clear_api_key, false)
  assert.equal(replaceWebSearch.api_key, 'new-key')
})

test('Build 上游模型检测 settings 保持未知字段并规范 ignored models', () => {
  const channel = createChannel(
    '{}',
    JSON.stringify({
      future_other_setting: 'keep',
      upstream_model_update_check_enabled: true,
      upstream_model_update_auto_sync_enabled: true,
      upstream_model_update_ignored_models: ['old-model'],
      upstream_model_update_last_detected_models: ['new-model'],
      upstream_model_update_last_check_time: 123,
    })
  )
  const defaults = transformChannelToFormDefaults(channel)

  assert.equal(defaults.upstream_model_update_check_enabled, true)
  assert.equal(defaults.upstream_model_update_auto_sync_enabled, true)
  assert.equal(defaults.upstream_model_update_ignored_models, 'old-model')

  const payload = transformFormDataToUpdatePayload(
    {
      ...defaults,
      upstream_model_update_ignored_models: 'new-model,new-model,regex:^gpt-',
    },
    channel.id
  )
  const settings = JSON.parse(payload.settings || '{}') as Record<
    string,
    unknown
  >

  assert.equal(settings.future_other_setting, 'keep')
  assert.deepEqual(settings.upstream_model_update_ignored_models, [
    'new-model',
    'regex:^gpt-',
  ])
  assert.deepEqual(settings.upstream_model_update_last_detected_models, [
    'new-model',
  ])
  assert.equal(settings.upstream_model_update_last_check_time, 123)
})
