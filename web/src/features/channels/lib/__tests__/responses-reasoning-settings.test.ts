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
  hasBuildChannelSettingValues,
  parseBuildChannelSettingDefaults,
} from '../build-channel-settings'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'

test('旧渠道默认关闭，已有渠道能恢复历史推理计数开关', () => {
  assert.equal(
    parseBuildChannelSettingDefaults({}).responses_reasoning_included_enabled,
    false
  )
  assert.equal(
    parseBuildChannelSettingDefaults({
      responses_reasoning_included_enabled: true,
    }).responses_reasoning_included_enabled,
    true
  )
})

test('历史推理计数开关可创建、保存和关闭，保留未知配置', () => {
  for (const enabled of [true, false]) {
    const values = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      setting: '{"future_setting":"keep"}',
      responses_reasoning_included_enabled: enabled,
    }
    const created = transformFormDataToCreatePayload(values)
    const updated = transformFormDataToUpdatePayload(values, 1)
    for (const setting of [created.channel.setting, updated.setting]) {
      const saved = JSON.parse(setting || '{}')
      assert.equal(saved.responses_reasoning_included_enabled, enabled)
      assert.equal(saved.future_setting, 'keep')
      assert.equal(
        parseBuildChannelSettingDefaults(saved)
          .responses_reasoning_included_enabled,
        enabled
      )
    }
    assert.equal(hasBuildChannelSettingValues(values), enabled)
  }
})
