/*
Copyright (C) 2025 QuantumNous

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

import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  applyBuildChannelOtherSettingFields,
  buildChannelSettingFields,
  parseBuildChannelOtherSettings,
  parseBuildChannelSettings,
  removeBuildChannelSettingFormFields,
  validateBuildChannelSettingsBeforeSubmit,
} from './buildChannelSettings.js';

test('Classic build 设置保持未知字段和 WebSearch API Key 状态', () => {
  const existingSettings = {
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
  };
  const defaults = parseBuildChannelSettings(existingSettings);

  assert.equal(defaults.web_search_api_key, '');
  assert.equal(defaults.web_search_api_key_configured, true);
  assert.equal(defaults.web_search_clear_api_key, false);
  assert.equal(defaults.vision_assist_channel_id, 12);
  assert.equal(defaults.vision_assist_target_models, 'gpt-4o');

  const merged = {
    ...existingSettings,
    ...buildChannelSettingFields(defaults, existingSettings),
  };

  assert.equal(merged.future_flag, 'keep');
  assert.equal(merged.web_search.future_web_search_flag, 'keep');
  assert.equal(merged.web_search.api_key, undefined);
  assert.equal(merged.web_search.clear_api_key, false);
  assert.equal(merged.vision_assist.future_vision_flag, 'keep');
});

test('Classic WebSearch 支持清空旧 Key 或替换为新 Key', () => {
  const defaults = parseBuildChannelSettings({
    web_search: {
      enabled: true,
      provider: 'anysearch',
      api_key_configured: true,
    },
  });

  const clearFields = buildChannelSettingFields(
    {
      ...defaults,
      web_search_clear_api_key: true,
      web_search_api_key: '',
    },
    {},
  );
  assert.equal(clearFields.web_search.clear_api_key, true);
  assert.equal(clearFields.web_search.api_key, undefined);

  const replaceFields = buildChannelSettingFields(
    {
      ...defaults,
      web_search_clear_api_key: true,
      web_search_api_key: 'new-key',
    },
    {},
  );
  assert.equal(replaceFields.web_search.clear_api_key, false);
  assert.equal(replaceFields.web_search.api_key, 'new-key');
});

test('Classic 上游模型检测 settings 保持未知字段并规范 ignored models', () => {
  const existingSettings = {
    future_other_setting: 'keep',
    upstream_model_update_check_enabled: true,
    upstream_model_update_auto_sync_enabled: true,
    upstream_model_update_ignored_models: ['old-model'],
    upstream_model_update_last_detected_models: ['new-model'],
    upstream_model_update_last_check_time: 123,
  };
  const defaults = parseBuildChannelOtherSettings(existingSettings);
  const settings = { ...existingSettings };

  applyBuildChannelOtherSettingFields(
    {
      ...defaults,
      upstream_model_update_ignored_models: 'new-model,new-model,regex:^gpt-',
    },
    settings,
  );

  assert.equal(settings.future_other_setting, 'keep');
  assert.deepEqual(settings.upstream_model_update_ignored_models, [
    'new-model',
    'regex:^gpt-',
  ]);
  assert.deepEqual(settings.upstream_model_update_last_detected_models, [
    'new-model',
  ]);
  assert.equal(settings.upstream_model_update_last_check_time, 123);
});

test('Classic build 字段校验和临时字段清理保持兼容', () => {
  const messages = [];
  const valid = validateBuildChannelSettingsBeforeSubmit(
    {
      web_search_enabled: true,
      web_search_provider: 'tavily',
      web_search_api_key: '',
      web_search_api_key_configured: false,
    },
    (key) => key,
    (message) => messages.push(message),
  );

  assert.equal(valid, false);
  assert.equal(
    messages[0],
    '启用 Tavily WebSearch 时必须填写 WebSearch API Key',
  );

  const payload = {
    name: 'keep',
    web_search_enabled: true,
    vision_assist_enabled: true,
    upstream_model_update_check_enabled: true,
  };
  removeBuildChannelSettingFormFields(payload);

  assert.deepEqual(payload, { name: 'keep' });
});
