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

function createChannel(setting: string): Channel {
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
    settings: '{}',
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
