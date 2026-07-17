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

import React from 'react';
import { useTranslation } from 'react-i18next';
import { Col, Form, Row, Tag, Tooltip, Typography } from '@douyinfe/semi-ui';

import ResponsesCompactPassthroughSetting from './ResponsesCompactPassthroughSetting';

const { Text } = Typography;

const formatUnixTime = (timestamp) => {
  const seconds = Number(timestamp);
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return '-';
  }
  return new Date(seconds * 1000).toLocaleString();
};

/**
 * 渲染 build 分支专属的上游模型检测设置区块。
 *
 * @param {object} props 组件参数。
 * @param {boolean} props.visible 当前渠道是否支持上游模型检测。
 * @param {object} props.inputs 当前表单输入值。
 * @param {string[]} props.upstreamDetectedModels 上次检测到的完整模型列表。
 * @param {string[]} props.upstreamDetectedModelsPreview 上次检测到的预览模型列表。
 * @param {number} props.upstreamDetectedModelsOmittedCount 预览省略数量。
 * @param {(key: string, value: unknown) => void} props.onInputChange 输入变更回调。
 * @param {(key: string, value: unknown) => void} props.onOtherSettingsChange other_settings 变更回调。
 * @returns {React.ReactElement | null} 上游模型检测设置区块。
 */
export const BuildChannelUpstreamModelSettings = (props) => {
  const { t } = useTranslation();

  if (!props.visible) return null;

  return (
    <div className='pb-3 border-b border-gray-100'>
      <Text className='text-sm font-medium text-gray-500 mb-3 block'>
        {t('上游模型管理')}
      </Text>

      <Form.Switch
        field='upstream_model_update_check_enabled'
        label={t('是否检测上游模型更新')}
        checkedText={t('开')}
        uncheckedText={t('关')}
        onChange={(value) =>
          props.onOtherSettingsChange(
            'upstream_model_update_check_enabled',
            value,
          )
        }
        extraText={t('开启后由后端定时任务检测该渠道上游模型变化')}
      />
      <Form.Switch
        field='upstream_model_update_auto_sync_enabled'
        label={t('是否自动同步上游模型更新')}
        checkedText={t('开')}
        uncheckedText={t('关')}
        disabled={!props.inputs.upstream_model_update_check_enabled}
        onChange={(value) =>
          props.onOtherSettingsChange(
            'upstream_model_update_auto_sync_enabled',
            value,
          )
        }
        extraText={t('开启后检测到新增模型会自动加入当前渠道模型列表')}
      />
      <Form.Input
        field='upstream_model_update_ignored_models'
        label={t('已忽略模型')}
        placeholder={t('例如：gpt-4.1-nano,regex:^claude-.*$,regex:^sora-.*$')}
        extraText={t('支持精确匹配；使用 regex: 开头可按正则匹配。')}
        onChange={(value) =>
          props.onInputChange('upstream_model_update_ignored_models', value)
        }
        showClear
      />
      <div className='text-xs text-gray-500 mb-2'>
        {t('上次检测时间')}:&nbsp;
        {formatUnixTime(props.inputs.upstream_model_update_last_check_time)}
      </div>
      <div className='text-xs text-gray-500 mb-3'>
        {t('上次检测到可加入模型')}:&nbsp;
        {props.upstreamDetectedModels.length === 0 ? (
          t('暂无')
        ) : (
          <>
            <Tooltip
              position='topLeft'
              content={
                <div className='max-w-[640px] break-all text-xs leading-5'>
                  {props.upstreamDetectedModels.join(', ')}
                </div>
              }
            >
              <span className='cursor-help break-all'>
                {props.upstreamDetectedModelsPreview.join(', ')}
              </span>
            </Tooltip>
            <span className='ml-1 text-gray-400'>
              {props.upstreamDetectedModelsOmittedCount > 0
                ? t('（共 {{total}} 个，省略 {{omit}} 个）', {
                    total: props.upstreamDetectedModels.length,
                    omit: props.upstreamDetectedModelsOmittedCount,
                  })
                : t('（共 {{total}} 个）', {
                    total: props.upstreamDetectedModels.length,
                  })}
            </span>
          </>
        )}
      </div>
    </div>
  );
};

/**
 * 渲染 build 分支专属的渠道额外设置字段。
 *
 * @param {object} props 组件参数。
 * @param {object} props.inputs 当前表单输入值。
 * @param {(key: string, value: unknown) => void} props.onSettingsChange setting 变更回调。
 * @returns {React.ReactElement} build 分支专属额外设置字段。
 */
export const BuildChannelExtraSettingsFields = (props) => {
  const { t } = useTranslation();

  return (
    <>
      <ResponsesCompactPassthroughSetting
        onChange={(value) =>
          props.onSettingsChange('responses_compact_passthrough_enabled', value)
        }
      />
      <Form.Switch
        field='use_upstream_model_for_billing'
        label={t('重定向后按上游模型计费')}
        checkedText={t('开')}
        uncheckedText={t('关')}
        onChange={(value) =>
          props.onSettingsChange('use_upstream_model_for_billing', value)
        }
        extraText={t(
          '开启后，model_mapping 生效时日志主模型与计费价格按最终上游模型计算',
        )}
      />

      <div className='mt-4 mb-2 text-sm font-medium text-gray-700'>
        {t('Claude Code WebSearch')}
      </div>
      <Form.Switch
        field='web_search_enabled'
        label={t('启用 WebSearch 模拟')}
        checkedText={t('开')}
        uncheckedText={t('关')}
        onChange={(value) =>
          props.onSettingsChange('web_search_enabled', value)
        }
        extraText={t('开启后在此渠道本地处理 Claude Code 纯 web_search 请求')}
      />
      <Row gutter={12}>
        <Col span={12}>
          <Form.Select
            field='web_search_provider'
            label={t('搜索供应商')}
            optionList={[
              { label: 'Tavily', value: 'tavily' },
              { label: 'AnySearch', value: 'anysearch' },
            ]}
            onChange={(value) =>
              props.onSettingsChange('web_search_provider', value)
            }
            style={{ width: '100%' }}
          />
        </Col>
        <Col span={12}>
          <Form.InputNumber
            field='web_search_max_results'
            label={t('最大搜索结果数')}
            placeholder='5'
            min={1}
            max={20}
            onNumberChange={(value) =>
              props.onSettingsChange('web_search_max_results', value)
            }
            style={{ width: '100%' }}
          />
        </Col>
      </Row>
      <Form.Input
        field='web_search_api_key'
        label={
          <span className='inline-flex items-center gap-2'>
            {t('WebSearch API Key')}
            {props.inputs.web_search_api_key_configured && (
              <Tag color='green' size='small'>
                {t('已配置')}
              </Tag>
            )}
          </span>
        }
        mode='password'
        autoComplete='new-password'
        placeholder={
          props.inputs.web_search_api_key_configured
            ? t('留空表示保留已有密钥')
            : props.inputs.web_search_provider === 'anysearch'
              ? t('供应商 API Key（可选）')
              : t('请输入供应商 API Key')
        }
        onChange={(value) =>
          props.onSettingsChange('web_search_api_key', value)
        }
        showClear
        extraText={t(
          'Tavily 必填；AnySearch 可选。填写后通过 Authorization Bearer 发送，接口响应不会返回明文密钥',
        )}
      />
      <Form.Switch
        field='web_search_clear_api_key'
        label={t('清空已保存的 WebSearch 密钥')}
        checkedText={t('开')}
        uncheckedText={t('关')}
        disabled={!props.inputs.web_search_api_key_configured}
        onChange={(value) =>
          props.onSettingsChange('web_search_clear_api_key', value)
        }
        extraText={t(
          'AnySearch 可清空后继续启用；Tavily 需要关闭 WebSearch 或填写新密钥',
        )}
      />
      {props.inputs.web_search_provider === 'tavily' ? (
        <Form.Select
          field='web_search_search_depth'
          label={t('Tavily 搜索深度')}
          optionList={[
            { label: t('基础'), value: 'basic' },
            { label: t('高级'), value: 'advanced' },
          ]}
          onChange={(value) =>
            props.onSettingsChange('web_search_search_depth', value)
          }
          style={{ width: '100%' }}
        />
      ) : (
        <Row gutter={12}>
          <Col span={12}>
            <Form.Select
              field='web_search_freshness'
              label={t('AnySearch 时效')}
              optionList={[
                { label: t('无'), value: '' },
                { label: t('天'), value: 'day' },
                { label: t('周'), value: 'week' },
                { label: t('月'), value: 'month' },
                { label: t('年'), value: 'year' },
              ]}
              onChange={(value) =>
                props.onSettingsChange('web_search_freshness', value)
              }
              style={{ width: '100%' }}
            />
          </Col>
          <Col span={12}>
            <Form.Input
              field='web_search_content_types'
              label={t('AnySearch 内容类型')}
              placeholder='web,news,doc'
              onChange={(value) =>
                props.onSettingsChange('web_search_content_types', value)
              }
              showClear
            />
          </Col>
        </Row>
      )}

      <div className='mt-4 mb-2 text-sm font-medium text-gray-700'>
        {t('视觉辅助识别')}
      </div>
      <Form.Switch
        field='vision_assist_enabled'
        label={t('启用视觉辅助识别')}
        checkedText={t('开')}
        uncheckedText={t('关')}
        onChange={(value) =>
          props.onSettingsChange('vision_assist_enabled', value)
        }
        extraText={t(
          '当目标渠道不支持图片时，先调用配置的视觉模型生成图片描述，再将请求改写为文本请求',
        )}
      />
      <Row gutter={12}>
        <Col span={12}>
          <Form.InputNumber
            field='vision_assist_channel_id'
            label={t('辅助渠道 ID')}
            placeholder={t('例如: 12')}
            min={0}
            onNumberChange={(value) =>
              props.onSettingsChange('vision_assist_channel_id', value)
            }
            style={{ width: '100%' }}
          />
        </Col>
        <Col span={12}>
          <Form.Input
            field='vision_assist_model'
            label={t('辅助模型')}
            placeholder={t('例如: gpt-4o-mini')}
            onChange={(value) =>
              props.onSettingsChange('vision_assist_model', value)
            }
            showClear
          />
        </Col>
      </Row>
      <Form.Input
        field='vision_assist_target_models'
        label={t('目标上游模型')}
        placeholder={t('留空表示全部，多个模型用英文逗号分隔')}
        onChange={(value) =>
          props.onSettingsChange('vision_assist_target_models', value)
        }
        showClear
        extraText={t(
          '按 model_mapping 重定向后的最终上游模型匹配，而不是用户请求的原始模型',
        )}
      />
      <Form.TextArea
        field='vision_assist_prompt'
        label={t('辅助提示词')}
        placeholder={t('留空使用默认图片描述提示词')}
        onChange={(value) =>
          props.onSettingsChange('vision_assist_prompt', value)
        }
        autosize
        showClear
      />
      <Row gutter={12}>
        <Col span={12}>
          <Form.Select
            field='vision_assist_endpoint_mode'
            label={t('辅助请求端点')}
            optionList={[
              { label: t('自动选择'), value: 'auto' },
              { label: t('OpenAI Chat Completions'), value: 'openai_chat' },
              { label: t('OpenAI Responses'), value: 'openai_responses' },
              { label: t('Anthropic Messages'), value: 'anthropic_messages' },
              { label: t('Gemini 原生'), value: 'gemini_native' },
            ]}
            onChange={(value) =>
              props.onSettingsChange('vision_assist_endpoint_mode', value)
            }
            style={{ width: '100%' }}
            extraText={t(
              '自动模式会让 Gemini 走原生接口、Claude 走 Messages，其他渠道走 OpenAI Chat',
            )}
          />
        </Col>
        <Col span={12}>
          <Form.InputNumber
            field='vision_assist_max_concurrency'
            label={t('辅助并发数')}
            placeholder='2'
            min={1}
            max={8}
            onNumberChange={(value) =>
              props.onSettingsChange('vision_assist_max_concurrency', value)
            }
            style={{ width: '100%' }}
          />
        </Col>
      </Row>
      <Row gutter={12}>
        <Col span={12}>
          <Form.InputNumber
            field='vision_assist_cache_ttl_seconds'
            label={t('缓存时间（秒）')}
            placeholder='86400'
            min={1}
            onNumberChange={(value) =>
              props.onSettingsChange('vision_assist_cache_ttl_seconds', value)
            }
            style={{ width: '100%' }}
          />
        </Col>
        <Col span={12}>
          <Form.Select
            field='vision_assist_failure_policy'
            label={t('失败策略')}
            optionList={[
              { label: t('报错'), value: 'error' },
              { label: t('跳过'), value: 'skip' },
            ]}
            onChange={(value) =>
              props.onSettingsChange('vision_assist_failure_policy', value)
            }
            style={{ width: '100%' }}
          />
        </Col>
      </Row>
      <Row gutter={12}>
        <Col span={12}>
          <Form.InputNumber
            field='vision_assist_retry_count'
            label={t('辅助失败重试次数')}
            placeholder='1'
            min={0}
            max={5}
            onNumberChange={(value) =>
              props.onSettingsChange('vision_assist_retry_count', value)
            }
            style={{ width: '100%' }}
          />
        </Col>
        <Col span={12}>
          <Form.InputNumber
            field='vision_assist_retry_backoff_ms'
            label={t('重试退避（毫秒）')}
            placeholder='500'
            min={1}
            max={30000}
            onNumberChange={(value) =>
              props.onSettingsChange('vision_assist_retry_backoff_ms', value)
            }
            style={{ width: '100%' }}
          />
        </Col>
      </Row>
      <Form.Switch
        field='vision_assist_strip_image'
        label={t('移除原始图片')}
        checkedText={t('开')}
        uncheckedText={t('关')}
        onChange={(value) =>
          props.onSettingsChange('vision_assist_strip_image', value)
        }
        extraText={t('推荐开启，避免非视觉模型收到无法处理的图片内容')}
      />
    </>
  );
};
