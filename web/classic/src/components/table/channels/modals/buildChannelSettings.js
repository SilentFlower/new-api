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

export const BUILD_WEB_SEARCH_FORM_DEFAULTS = {
  web_search_enabled: false,
  web_search_provider: 'tavily',
  web_search_api_key: '',
  web_search_api_key_configured: false,
  web_search_clear_api_key: false,
  web_search_max_results: 5,
  web_search_search_depth: 'basic',
  web_search_freshness: '',
  web_search_content_types: '',
};

export const BUILD_CHANNEL_SETTING_DEFAULTS = {
  responses_compact_passthrough_enabled: false,
  use_upstream_model_for_billing: false,
  vision_assist_enabled: false,
  vision_assist_channel_id: '',
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
  ...BUILD_WEB_SEARCH_FORM_DEFAULTS,
};

export const BUILD_CHANNEL_OTHER_SETTING_DEFAULTS = {
  upstream_model_update_check_enabled: false,
  upstream_model_update_auto_sync_enabled: false,
  upstream_model_update_last_check_time: 0,
  upstream_model_update_last_detected_models: [],
  upstream_model_update_ignored_models: '',
};

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
];

export const BUILD_CHANNEL_OTHER_SETTING_FORM_FIELDS = [
  'upstream_model_update_check_enabled',
  'upstream_model_update_auto_sync_enabled',
  'upstream_model_update_last_check_time',
  'upstream_model_update_last_detected_models',
  'upstream_model_update_ignored_models',
];

const WEB_SEARCH_PROVIDERS = ['tavily', 'anysearch'];
const TAVILY_SEARCH_DEPTHS = ['basic', 'advanced'];
const ANYSEARCH_FRESHNESS_VALUES = ['', 'day', 'week', 'month', 'year'];
const VISION_ASSIST_ENDPOINT_MODES = [
  'auto',
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_native',
];

const parseCommaList = (value) =>
  Array.from(
    new Set(
      String(value || '')
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  );

const joinList = (value) => (Array.isArray(value) ? value.join(',') : '');

const parseJsonObject = (value) => {
  if (!value) return {};
  if (typeof value === 'object' && !Array.isArray(value)) return value;
  if (typeof value !== 'string') return {};
  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? parsed
      : {};
  } catch (error) {
    return {};
  }
};

export const normalizeWebSearchProvider = (value) => {
  const provider = String(value || '');
  return WEB_SEARCH_PROVIDERS.includes(provider) ? provider : 'tavily';
};

export const normalizeTavilySearchDepth = (value) => {
  const depth = String(value || '');
  return TAVILY_SEARCH_DEPTHS.includes(depth) ? depth : 'basic';
};

export const normalizeAnySearchFreshness = (value) => {
  const freshness = String(value || '');
  return ANYSEARCH_FRESHNESS_VALUES.includes(freshness) ? freshness : '';
};

export const normalizeWebSearchMaxResults = (value) => {
  const maxResults = parseInt(value, 10);
  if (!Number.isFinite(maxResults)) {
    return 5;
  }
  return Math.min(Math.max(maxResults, 1), 20);
};

export const normalizeVisionAssistEndpointMode = (value) => {
  const endpointMode = String(value || '');
  return VISION_ASSIST_ENDPOINT_MODES.includes(endpointMode)
    ? endpointMode
    : 'auto';
};

/**
 * 从 setting 对象恢复 build 分支专属表单字段。
 *
 * @param {Record<string, unknown>} settings 已解析的渠道 setting JSON。
 * @returns {Record<string, unknown>} build 分支专属表单字段。
 */
export const parseBuildChannelSettings = (settings) => {
  const parsed = parseJsonObject(settings);
  const visionAssist = parseJsonObject(parsed.vision_assist);
  const webSearch = parseJsonObject(parsed.web_search);

  return {
    ...BUILD_CHANNEL_SETTING_DEFAULTS,
    responses_compact_passthrough_enabled:
      parsed.responses_compact_passthrough_enabled === true,
    use_upstream_model_for_billing:
      parsed.use_upstream_model_for_billing === true,
    vision_assist_enabled: visionAssist.enabled === true,
    vision_assist_channel_id: visionAssist.assist_channel_id || '',
    vision_assist_model: visionAssist.assist_model || '',
    vision_assist_target_models: joinList(visionAssist.target_models),
    vision_assist_prompt: visionAssist.prompt || '',
    vision_assist_cache_ttl_seconds: visionAssist.cache_ttl_seconds || 86400,
    vision_assist_failure_policy: visionAssist.failure_policy || 'error',
    vision_assist_strip_image:
      visionAssist.strip_image === undefined
        ? true
        : visionAssist.strip_image === true,
    vision_assist_endpoint_mode: normalizeVisionAssistEndpointMode(
      visionAssist.endpoint_mode,
    ),
    vision_assist_max_concurrency:
      Number(visionAssist.max_concurrency) > 0
        ? Number(visionAssist.max_concurrency)
        : 2,
    vision_assist_retry_count:
      Number(visionAssist.retry_count) >= 0
        ? Number(visionAssist.retry_count)
        : 1,
    vision_assist_retry_backoff_ms:
      Number(visionAssist.retry_backoff_ms) > 0
        ? Number(visionAssist.retry_backoff_ms)
        : 500,
    web_search_enabled: webSearch.enabled === true,
    web_search_provider: normalizeWebSearchProvider(webSearch.provider),
    web_search_api_key: '',
    web_search_api_key_configured: webSearch.api_key_configured === true,
    web_search_clear_api_key: false,
    web_search_max_results: normalizeWebSearchMaxResults(webSearch.max_results),
    web_search_search_depth: normalizeTavilySearchDepth(webSearch.search_depth),
    web_search_freshness: normalizeAnySearchFreshness(webSearch.freshness),
    web_search_content_types: joinList(webSearch.content_types),
  };
};

/**
 * 从 settings 对象恢复 build 分支专属 other_settings 表单字段。
 *
 * @param {Record<string, unknown>} settings 已解析的渠道 settings JSON。
 * @returns {Record<string, unknown>} build 分支专属 other_settings 表单字段。
 */
export const parseBuildChannelOtherSettings = (settings) => {
  const parsed = parseJsonObject(settings);

  return {
    ...BUILD_CHANNEL_OTHER_SETTING_DEFAULTS,
    upstream_model_update_check_enabled:
      parsed.upstream_model_update_check_enabled === true,
    upstream_model_update_auto_sync_enabled:
      parsed.upstream_model_update_auto_sync_enabled === true,
    upstream_model_update_last_check_time:
      Number(parsed.upstream_model_update_last_check_time) || 0,
    upstream_model_update_last_detected_models: Array.isArray(
      parsed.upstream_model_update_last_detected_models,
    )
      ? parsed.upstream_model_update_last_detected_models
      : [],
    upstream_model_update_ignored_models: Array.isArray(
      parsed.upstream_model_update_ignored_models,
    )
      ? parsed.upstream_model_update_ignored_models.join(',')
      : '',
  };
};

/**
 * 将 build 分支专属表单字段合并回 setting JSON。
 *
 * @param {Record<string, unknown>} values 当前表单值。
 * @param {Record<string, unknown>} existingSettings 已解析的原始 setting JSON。
 * @returns {Record<string, unknown>} 可展开进 setting JSON 的 build 字段。
 */
export const buildChannelSettingFields = (values, existingSettings) => {
  const visionAssistChannelId = parseInt(values.vision_assist_channel_id, 10);
  const visionAssistCacheTTL = parseInt(
    values.vision_assist_cache_ttl_seconds,
    10,
  );
  const visionAssistMaxConcurrency = parseInt(
    values.vision_assist_max_concurrency,
    10,
  );
  const visionAssistRetryCount = parseInt(values.vision_assist_retry_count, 10);
  const visionAssistRetryBackoff = parseInt(
    values.vision_assist_retry_backoff_ms,
    10,
  );

  return {
    responses_compact_passthrough_enabled:
      values.responses_compact_passthrough_enabled === true,
    use_upstream_model_for_billing:
      values.use_upstream_model_for_billing === true,
    vision_assist: {
      ...(parseJsonObject(existingSettings.vision_assist) || {}),
      enabled: values.vision_assist_enabled === true,
      assist_channel_id: Number.isFinite(visionAssistChannelId)
        ? visionAssistChannelId
        : 0,
      assist_model: String(values.vision_assist_model || '').trim(),
      target_models: parseCommaList(values.vision_assist_target_models),
      prompt: String(values.vision_assist_prompt || '').trim(),
      cache_ttl_seconds:
        Number.isFinite(visionAssistCacheTTL) && visionAssistCacheTTL > 0
          ? visionAssistCacheTTL
          : 86400,
      failure_policy:
        values.vision_assist_failure_policy === 'skip' ? 'skip' : 'error',
      strip_image: values.vision_assist_strip_image !== false,
      endpoint_mode: normalizeVisionAssistEndpointMode(
        values.vision_assist_endpoint_mode,
      ),
      max_concurrency:
        Number.isFinite(visionAssistMaxConcurrency) &&
        visionAssistMaxConcurrency > 0
          ? visionAssistMaxConcurrency
          : 2,
      retry_count:
        Number.isFinite(visionAssistRetryCount) && visionAssistRetryCount >= 0
          ? visionAssistRetryCount
          : 1,
      retry_backoff_ms:
        Number.isFinite(visionAssistRetryBackoff) &&
        visionAssistRetryBackoff > 0
          ? visionAssistRetryBackoff
          : 500,
    },
    web_search: {
      ...(parseJsonObject(existingSettings.web_search) || {}),
      enabled: values.web_search_enabled === true,
      provider: normalizeWebSearchProvider(values.web_search_provider),
      api_key: String(values.web_search_api_key || '').trim() || undefined,
      clear_api_key:
        values.web_search_clear_api_key === true &&
        !String(values.web_search_api_key || '').trim(),
      max_results: normalizeWebSearchMaxResults(values.web_search_max_results),
      search_depth: normalizeTavilySearchDepth(values.web_search_search_depth),
      freshness: normalizeAnySearchFreshness(values.web_search_freshness),
      content_types: parseCommaList(values.web_search_content_types),
    },
  };
};

/**
 * 将 build 分支专属 other_settings 字段合并回 settings JSON。
 *
 * @param {Record<string, unknown>} values 当前表单值。
 * @param {Record<string, unknown>} settings 当前 settings JSON 对象，函数会原地更新。
 * @returns {Record<string, unknown>} 更新后的 settings JSON 对象。
 */
export const applyBuildChannelOtherSettingFields = (values, settings) => {
  settings.upstream_model_update_check_enabled =
    values.upstream_model_update_check_enabled === true;
  settings.upstream_model_update_auto_sync_enabled =
    settings.upstream_model_update_check_enabled &&
    values.upstream_model_update_auto_sync_enabled === true;
  settings.upstream_model_update_ignored_models = parseCommaList(
    values.upstream_model_update_ignored_models,
  );
  if (
    !Array.isArray(settings.upstream_model_update_last_detected_models) ||
    !settings.upstream_model_update_check_enabled
  ) {
    settings.upstream_model_update_last_detected_models = [];
  }
  if (typeof settings.upstream_model_update_last_check_time !== 'number') {
    settings.upstream_model_update_last_check_time = 0;
  }

  return settings;
};

/**
 * 校验 build 分支专属 WebSearch 提交约束。
 *
 * @param {Record<string, unknown>} values 当前表单值。
 * @param {(key: string, options?: Record<string, unknown>) => string} t 翻译函数。
 * @param {(message: string) => void} showInfo 信息提示函数。
 * @returns {boolean} 校验通过时返回 true。
 */
export const validateBuildChannelSettingsBeforeSubmit = (
  values,
  t,
  showInfo,
) => {
  if (values.web_search_enabled !== true) return true;

  const webSearchProvider = normalizeWebSearchProvider(
    values.web_search_provider,
  );
  const hasNewWebSearchKey = Boolean(
    String(values.web_search_api_key || '').trim(),
  );
  const hasExistingWebSearchKey = values.web_search_api_key_configured === true;
  if (
    webSearchProvider === 'tavily' &&
    !hasNewWebSearchKey &&
    !hasExistingWebSearchKey
  ) {
    showInfo(t('启用 Tavily WebSearch 时必须填写 WebSearch API Key'));
    return false;
  }
  if (
    webSearchProvider === 'tavily' &&
    values.web_search_clear_api_key === true &&
    !hasNewWebSearchKey
  ) {
    showInfo(
      t(
        'Tavily WebSearch 清空已保存密钥前，请先填写新的 WebSearch API Key 或关闭 WebSearch',
      ),
    );
    return false;
  }

  return true;
};

/**
 * 从待提交对象中删除 build 分支专属临时字段。
 *
 * @param {Record<string, unknown>} values 待提交对象。
 * @returns {Record<string, unknown>} 删除临时字段后的同一个对象。
 */
export const removeBuildChannelSettingFormFields = (values) => {
  BUILD_CHANNEL_SETTING_FORM_FIELDS.forEach((field) => {
    delete values[field];
  });
  BUILD_CHANNEL_OTHER_SETTING_FORM_FIELDS.forEach((field) => {
    delete values[field];
  });
  return values;
};

/**
 * 生成用于 channelSettings React 状态的 build 字段快照。
 *
 * @param {Record<string, unknown>} data 当前表单数据。
 * @returns {Record<string, unknown>} build 字段状态快照。
 */
export const getBuildChannelSettingsState = (data) => ({
  responses_compact_passthrough_enabled:
    data.responses_compact_passthrough_enabled === true,
  use_upstream_model_for_billing: data.use_upstream_model_for_billing || false,
  vision_assist_enabled: data.vision_assist_enabled || false,
  vision_assist_channel_id: data.vision_assist_channel_id || '',
  vision_assist_model: data.vision_assist_model || '',
  vision_assist_target_models: data.vision_assist_target_models || '',
  vision_assist_prompt: data.vision_assist_prompt || '',
  vision_assist_cache_ttl_seconds:
    data.vision_assist_cache_ttl_seconds || 86400,
  vision_assist_failure_policy: data.vision_assist_failure_policy || 'error',
  vision_assist_strip_image: data.vision_assist_strip_image !== false,
  vision_assist_endpoint_mode: data.vision_assist_endpoint_mode || 'auto',
  vision_assist_max_concurrency: data.vision_assist_max_concurrency || 2,
  vision_assist_retry_count:
    data.vision_assist_retry_count === undefined
      ? 1
      : data.vision_assist_retry_count,
  vision_assist_retry_backoff_ms: data.vision_assist_retry_backoff_ms || 500,
  web_search_enabled: data.web_search_enabled || false,
  web_search_provider: data.web_search_provider || 'tavily',
  web_search_api_key: '',
  web_search_api_key_configured: data.web_search_api_key_configured === true,
  web_search_clear_api_key: false,
  web_search_max_results: data.web_search_max_results || 5,
  web_search_search_depth: data.web_search_search_depth || 'basic',
  web_search_freshness: data.web_search_freshness || '',
  web_search_content_types: data.web_search_content_types || '',
});

/**
 * 判断 build 分支专属高级设置是否有非默认配置。
 *
 * @param {Record<string, unknown>} data 当前表单数据。
 * @returns {boolean} 任一 build 专属设置非默认时返回 true。
 */
export const hasBuildChannelSettingValues = (data) =>
  Boolean(
    data.responses_compact_passthrough_enabled ||
    data.use_upstream_model_for_billing ||
    data.vision_assist_enabled ||
    data.vision_assist_channel_id ||
    data.vision_assist_model ||
    data.vision_assist_target_models ||
    data.web_search_enabled ||
    data.web_search_api_key_configured ||
    data.web_search_provider !== 'tavily' ||
    data.web_search_clear_api_key ||
    data.web_search_max_results !== 5 ||
    data.web_search_search_depth !== 'basic' ||
    data.web_search_freshness ||
    data.web_search_content_types,
  );
