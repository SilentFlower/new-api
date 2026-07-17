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

import type { Channel } from '../types'
import {
  transformChannelToFormDefaults,
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

  const payload = transformFormDataToUpdatePayload(defaults, channel.id)
  const setting = JSON.parse(payload.setting || '{}') as Record<string, unknown>
  const webSearch = setting.web_search as Record<string, unknown>
  const visionAssist = setting.vision_assist as Record<string, unknown>

  assert.equal(setting.future_flag, 'keep')
  assert.equal(webSearch.future_web_search_flag, 'keep')
  assert.equal(webSearch.api_key, undefined)
  assert.equal(webSearch.clear_api_key, false)
  assert.equal(visionAssist.future_vision_flag, 'keep')
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
