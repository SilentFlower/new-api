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

import React, {
  useState,
  useEffect,
  useCallback,
  useMemo,
  useRef,
} from 'react';
import { useTranslation } from 'react-i18next';
import { initVChartSemiTheme } from '@visactor/vchart-semi-theme';

import {
  renderQuota,
  renderNumber,
  modelColorMap,
  modelToColor,
  getQuotaWithUnit,
  timestamp2string,
  getLogOther,
  copy,
  showSuccess,
  renderLogContent,
  renderClaudeLogContent,
  renderClaudeModelPrice,
  renderModelPrice,
  renderAudioModelPrice,
} from '../../helpers';
import {
  aggregateDataByTimeAndModel,
  generateChartTimePoints,
  updateChartSpec,
  getDefaultTime,
} from '../../helpers/dashboard';
import { ITEMS_PER_PAGE } from '../../constants';
import { getLogsColumns } from '../../components/table/usage-logs/UsageLogsColumnDefs';
import {
  createTokenAPI,
} from './api';
import {
  TokenLogAuthPanel,
  TokenLogChartsPanel,
  TokenLogHeader,
  TokenLogStatsCards,
  TokenLogTablePanel,
  TokenLogTimeRangeControls,
} from './components';
import {
  getGlobalTokenLogTimeRange,
} from './utils';
import ParamOverrideEntry from '../../components/table/usage-logs/components/ParamOverrideEntry';
import ParamOverrideModal from '../../components/table/usage-logs/modals/ParamOverrideModal';

/**
 * LogViewer - 公共 API Key 使用日志查看页面
 * 无需登录，用户输入 API Key 后可查看该 Key 的使用统计和日志
 */
const LogViewer = () => {
  const { t } = useTranslation();

  // ========== API Key 状态 ==========
  const [apiKey, setApiKey] = useState('');
  const [authenticated, setAuthenticated] = useState(false);
  const [authLoading, setAuthLoading] = useState(false);
  const [authError, setAuthError] = useState('');
  const tokenAPI = useRef(null);

  // ========== 统计数据状态 ==========
  const [statLoading, setStatLoading] = useState(false);
  const [stat, setStat] = useState(null);

  // ========== 图表数据状态 ==========
  const [chartLoading, setChartLoading] = useState(false);
  const [activeChartTab, setActiveChartTab] = useState('1');

  // ========== 日志表格状态 ==========
  const [logs, setLogs] = useState([]);
  const [expandData, setExpandData] = useState({});
  const [logLoading, setLogLoading] = useState(false);
  const [activePage, setActivePage] = useState(1);
  const [logCount, setLogCount] = useState(0);
  const [pageSize, setPageSize] = useState(ITEMS_PER_PAGE);
  const [formApi, setFormApi] = useState(null);

  // ========== 参数覆盖弹窗状态 ==========
  const [showParamOverrideModal, setShowParamOverrideModal] = useState(false);
  const [paramOverrideTarget, setParamOverrideTarget] = useState(null);

  // ========== 全局时间范围状态 ==========
  const [timePreset, setTimePreset] = useState('today');
  const [customDateRange, setCustomDateRange] = useState(null);

  // ========== 图表规格状态 ==========
  const [specPie, setSpecPie] = useState({
    type: 'pie',
    data: [{ id: 'id0', values: [{ type: 'null', value: '0' }] }],
    outerRadius: 0.8,
    innerRadius: 0.5,
    padAngle: 0.6,
    valueField: 'value',
    categoryField: 'type',
    pie: {
      style: { cornerRadius: 10 },
      state: {
        hover: { outerRadius: 0.85, stroke: '#000', lineWidth: 1 },
        selected: { outerRadius: 0.85, stroke: '#000', lineWidth: 1 },
      },
    },
    title: {
      visible: true,
      text: t('模型调用次数占比'),
      subtext: `${t('总计')}：${renderNumber(0)}`,
    },
    legends: { visible: true, orient: 'left' },
    label: { visible: true },
    tooltip: {
      mark: {
        content: [
          {
            key: (datum) => datum['type'],
            value: (datum) => renderNumber(datum['value']),
          },
        ],
      },
    },
    color: { specified: modelColorMap },
  });

  const [specLine, setSpecLine] = useState({
    type: 'bar',
    data: [{ id: 'barData', values: [] }],
    xField: 'Time',
    yField: 'Usage',
    seriesField: 'Model',
    stack: true,
    legends: { visible: true, selectMode: 'single' },
    title: {
      visible: true,
      text: t('模型消耗分布'),
      subtext: `${t('总计')}：${renderQuota(0, 2)}`,
    },
    bar: {
      state: { hover: { stroke: '#000', lineWidth: 1 } },
    },
    tooltip: {
      mark: {
        content: [
          {
            key: (datum) => datum['Model'],
            value: (datum) => renderQuota(datum['rawQuota'] || 0, 4),
          },
        ],
      },
    },
    color: { specified: modelColorMap },
  });

  // ========== 列定义（公共模式：隐藏敏感列）==========
  const COLUMN_KEYS = useMemo(
    () => ({
      TIME: 'time',
      CHANNEL: 'channel',
      USERNAME: 'username',
      TOKEN: 'token',
      GROUP: 'group',
      TYPE: 'type',
      MODEL: 'model',
      USE_TIME: 'use_time',
      PROMPT: 'prompt',
      COMPLETION: 'completion',
      COST: 'cost',
      RETRY: 'retry',
      IP: 'ip',
      DETAILS: 'details',
    }),
    [],
  );

  // 公共模式下的可见列（隐藏渠道、用户名、IP、重试）
  const visibleColumns = useMemo(
    () => ({
      [COLUMN_KEYS.TIME]: true,
      [COLUMN_KEYS.CHANNEL]: false,
      [COLUMN_KEYS.USERNAME]: false,
      [COLUMN_KEYS.TOKEN]: true,
      [COLUMN_KEYS.GROUP]: true,
      [COLUMN_KEYS.TYPE]: true,
      [COLUMN_KEYS.MODEL]: true,
      [COLUMN_KEYS.USE_TIME]: true,
      [COLUMN_KEYS.PROMPT]: true,
      [COLUMN_KEYS.COMPLETION]: true,
      [COLUMN_KEYS.COST]: true,
      [COLUMN_KEYS.RETRY]: false,
      [COLUMN_KEYS.IP]: false,
      [COLUMN_KEYS.DETAILS]: true,
    }),
    [COLUMN_KEYS],
  );

  // ========== 全局时间范围计算 ==========
  const getGlobalTimeRange = useCallback(() => {
    return getGlobalTokenLogTimeRange(timePreset, customDateRange);
  }, [timePreset, customDateRange]);

  // ========== 初始化图表主题 ==========
  useEffect(() => {
    initVChartSemiTheme({ isWatchingThemeSwitch: true });
  }, []);

  // ========== 复制文本辅助函数 ==========
  const copyText = useCallback(
    async (e, text) => {
      e.stopPropagation();
      if (await copy(text)) {
        showSuccess(t('已复制') + '：' + text);
      }
    },
    [t],
  );

  // ========== 参数覆盖弹窗 ==========
  const openParamOverrideModal = useCallback((log, other) => {
    const lines = Array.isArray(other?.po) ? other.po.filter(Boolean) : [];
    if (lines.length === 0) return;
    setParamOverrideTarget({
      lines,
      modelName: log?.model_name || '',
      requestId: log?.request_id || '',
      requestPath: other?.request_path || '',
    });
    setShowParamOverrideModal(true);
  }, []);

  // ========== 格式化日志数据 ==========
  const formatLogs = useCallback(
    (rawLogs) => {
      const expandDatesLocal = {};
      for (let i = 0; i < rawLogs.length; i++) {
        rawLogs[i].timestamp2string = timestamp2string(rawLogs[i].created_at);
        rawLogs[i].key = rawLogs[i].id;
        const other = getLogOther(rawLogs[i].other);
        const expandDataLocal = [];

        if (rawLogs[i].request_id) {
          expandDataLocal.push({
            key: t('Request ID'),
            value: rawLogs[i].request_id,
          });
        }
        if (other?.ws || other?.audio) {
          expandDataLocal.push({
            key: t('语音输入'),
            value: other.audio_input,
          });
          expandDataLocal.push({
            key: t('语音输出'),
            value: other.audio_output,
          });
          expandDataLocal.push({
            key: t('文字输入'),
            value: other.text_input,
          });
          expandDataLocal.push({
            key: t('文字输出'),
            value: other.text_output,
          });
        }
        if (other?.cache_tokens > 0) {
          expandDataLocal.push({
            key: t('缓存 Tokens'),
            value: other.cache_tokens,
          });
        }
        if (other?.cache_creation_tokens > 0) {
          expandDataLocal.push({
            key: t('缓存创建 Tokens'),
            value: other.cache_creation_tokens,
          });
        }
        if (rawLogs[i].type === 2) {
          expandDataLocal.push({
            key: t('日志详情'),
            value: other?.claude
              ? renderClaudeLogContent(
                  other?.model_ratio,
                  other.completion_ratio,
                  other.model_price,
                  other.group_ratio,
                  other?.user_group_ratio,
                  other.cache_ratio || 1.0,
                  other.cache_creation_ratio || 1.0,
                  other.cache_creation_tokens_5m || 0,
                  other.cache_creation_ratio_5m ||
                    other.cache_creation_ratio ||
                    1.0,
                  other.cache_creation_tokens_1h || 0,
                  other.cache_creation_ratio_1h ||
                    other.cache_creation_ratio ||
                    1.0,
                  'price',
                )
              : renderLogContent(
                  other?.model_ratio,
                  other.completion_ratio,
                  other.model_price,
                  other.group_ratio,
                  other?.user_group_ratio,
                  other.cache_ratio || 1.0,
                  false,
                  1.0,
                  other.web_search || false,
                  other.web_search_call_count || 0,
                  other.file_search || false,
                  other.file_search_call_count || 0,
                  'price',
                ),
          });
          if (rawLogs[i]?.content) {
            expandDataLocal.push({
              key: t('其他详情'),
              value: rawLogs[i].content,
            });
          }
        }
        if (rawLogs[i].type === 2) {
          const modelMapped =
            other?.is_model_mapped &&
            other?.upstream_model_name &&
            other?.upstream_model_name !== '';
          if (modelMapped) {
            expandDataLocal.push({
              key: t('请求并计费模型'),
              value: rawLogs[i].model_name,
            });
            expandDataLocal.push({
              key: t('实际模型'),
              value: other.upstream_model_name,
            });
          }

          let content = '';
          if (other?.ws || other?.audio) {
            content = renderAudioModelPrice(
              other?.text_input,
              other?.text_output,
              other?.model_ratio,
              other?.model_price,
              other?.completion_ratio,
              other?.audio_input,
              other?.audio_output,
              other?.audio_ratio,
              other?.audio_completion_ratio,
              other?.group_ratio,
              other?.user_group_ratio,
              other?.cache_tokens || 0,
              other?.cache_ratio || 1.0,
              'price',
            );
          } else if (other?.claude) {
            content = renderClaudeModelPrice(
              rawLogs[i].prompt_tokens,
              rawLogs[i].completion_tokens,
              other.model_ratio,
              other.model_price,
              other.completion_ratio,
              other.group_ratio,
              other?.user_group_ratio,
              other.cache_tokens || 0,
              other.cache_ratio || 1.0,
              other.cache_creation_tokens || 0,
              other.cache_creation_ratio || 1.0,
              other.cache_creation_tokens_5m || 0,
              other.cache_creation_ratio_5m ||
                other.cache_creation_ratio ||
                1.0,
              other.cache_creation_tokens_1h || 0,
              other.cache_creation_ratio_1h ||
                other.cache_creation_ratio ||
                1.0,
              'price',
            );
          } else {
            content = renderModelPrice(
              rawLogs[i].prompt_tokens,
              rawLogs[i].completion_tokens,
              other?.model_ratio,
              other?.model_price,
              other?.completion_ratio,
              other?.group_ratio,
              other?.user_group_ratio,
              other?.cache_tokens || 0,
              other?.cache_ratio || 1.0,
              other?.image || false,
              other?.image_ratio || 0,
              other?.image_output || 0,
              other?.web_search || false,
              other?.web_search_call_count || 0,
              other?.web_search_price || 0,
              other?.file_search || false,
              other?.file_search_call_count || 0,
              other?.file_search_price || 0,
              other?.audio_input_seperate_price || false,
              other?.audio_input_token_count || 0,
              other?.audio_input_price || 0,
              other?.image_generation_call || false,
              other?.image_generation_call_price || 0,
              'price',
            );
          }
          expandDataLocal.push({
            key: t('计费过程'),
            value: content,
          });
        }
        if (rawLogs[i].type === 6) {
          if (other?.task_id) {
            expandDataLocal.push({
              key: t('任务ID'),
              value: other.task_id,
            });
          }
          if (other?.reason) {
            expandDataLocal.push({
              key: t('失败原因'),
              value: (
                <div
                  style={{
                    maxWidth: 600,
                    whiteSpace: 'normal',
                    wordBreak: 'break-word',
                    lineHeight: 1.6,
                  }}
                >
                  {other.reason}
                </div>
              ),
            });
          }
        }
        if (other?.request_path) {
          expandDataLocal.push({
            key: t('请求路径'),
            value: other.request_path,
          });
        }
        if (Array.isArray(other?.po) && other.po.length > 0) {
          expandDataLocal.push({
            key: t('参数覆盖'),
            value: (
              <ParamOverrideEntry
                count={other.po.length}
                t={t}
                onOpen={(event) => {
                  event.stopPropagation();
                  openParamOverrideModal(rawLogs[i], other);
                }}
              />
            ),
          });
        }
        if (other?.billing_source === 'subscription') {
          const planId = other?.subscription_plan_id;
          const planTitle = other?.subscription_plan_title || '';
          const subscriptionId = other?.subscription_id;
          const unit = t('额度');
          const pre = other?.subscription_pre_consumed ?? 0;
          const postDelta = other?.subscription_post_delta ?? 0;
          const finalConsumed = other?.subscription_consumed ?? pre + postDelta;
          const remain = other?.subscription_remain;
          const total = other?.subscription_total;
          if (planId) {
            expandDataLocal.push({
              key: t('订阅套餐'),
              value: `#${planId} ${planTitle}`.trim(),
            });
          }
          if (subscriptionId) {
            expandDataLocal.push({
              key: t('订阅实例'),
              value: `#${subscriptionId}`,
            });
          }
          const settlementLines = [
            `${t('预扣')}：${pre} ${unit}`,
            `${t('结算差额')}：${postDelta > 0 ? '+' : ''}${postDelta} ${unit}`,
            `${t('最终抵扣')}：${finalConsumed} ${unit}`,
          ]
            .filter(Boolean)
            .join('\n');
          expandDataLocal.push({
            key: t('订阅结算'),
            value: (
              <div style={{ whiteSpace: 'pre-line' }}>{settlementLines}</div>
            ),
          });
          if (remain !== undefined && total !== undefined) {
            expandDataLocal.push({
              key: t('订阅剩余'),
              value: `${remain}/${total} ${unit}`,
            });
          }
        }

        expandDatesLocal[rawLogs[i].key] = expandDataLocal;
      }

      setExpandData(expandDatesLocal);
      setLogs(rawLogs);
    },
    [t, openParamOverrideModal],
  );

  // ========== 获取表单过滤值 ==========
  const getFormValues = useCallback(() => {
    const formValues = formApi ? formApi.getValues() : {};
    const { startTs, endTs } = getGlobalTimeRange();

    return {
      model_name: formValues.model_name || '',
      request_id: formValues.request_id || '',
      startTs,
      endTs,
      logType: formValues.logType ? parseInt(formValues.logType) : 0,
    };
  }, [formApi, getGlobalTimeRange]);

  // ========== API Key 验证 ==========
  const handleAuth = useCallback(async () => {
    if (!apiKey.trim()) {
      setAuthError(t('请输入 API Key'));
      return;
    }
    setAuthLoading(true);
    setAuthError('');
    try {
      const api = createTokenAPI(apiKey.trim());
      // 使用 stat 端点验证 Key 有效性
      const res = await api.get('/api/log/token/stat');
      if (res.data.success) {
        tokenAPI.current = api;
        setAuthenticated(true);
        setStat(res.data.data);
      } else {
        setAuthError(res.data.message || t('API Key 无效'));
      }
    } catch (err) {
      if (err.response?.status === 401) {
        setAuthError(t('API Key 无效'));
      } else if (err.response?.status === 403) {
        setAuthError(t('用户已被封禁'));
      } else if (err.response?.status === 429) {
        setAuthError(t('请求过于频繁，请稍后再试'));
      } else {
        setAuthError(t('验证失败，请检查网络连接'));
      }
    } finally {
      setAuthLoading(false);
    }
  }, [apiKey, t]);

  // ========== 加载统计数据 ==========
  const loadStat = useCallback(async (startTs, endTs) => {
    if (!tokenAPI.current) return;
    setStatLoading(true);
    try {
      let url = '/api/log/token/stat?';
      if (startTs) url += `start_timestamp=${startTs}&`;
      if (endTs) url += `end_timestamp=${endTs}&`;
      const res = await tokenAPI.current.get(url);
      if (res.data.success) {
        setStat(res.data.data);
      }
    } catch (err) {
      console.error('Failed to load stat:', err);
    } finally {
      setStatLoading(false);
    }
  }, []);

  // ========== 加载图表数据 ==========
  const loadChartData = useCallback(
    async (startTs, endTs) => {
      if (!tokenAPI.current) return;
      setChartLoading(true);
      try {
        const res = await tokenAPI.current.get(
          `/api/log/token/data?start_timestamp=${startTs}&end_timestamp=${endTs}`,
        );
        if (res.data.success) {
          const data = res.data.data;

          // 更新饼图数据
          const pieData = (data.model_stats || []).map((item) => ({
            type: item.model_name,
            value: item.count,
          }));
          const totalCount = pieData.reduce((sum, item) => sum + item.value, 0);

          // 生成模型颜色映射
          const newModelColors = {};
          pieData.forEach((item) => {
            newModelColors[item.type] =
              modelColorMap[item.type] || modelToColor(item.type);
          });

          updateChartSpec(
            setSpecPie,
            pieData.length > 0 ? pieData : [{ type: t('无数据'), value: 0 }],
            `${t('总计')}：${renderNumber(totalCount)}`,
            newModelColors,
            'id0',
          );

          // 处理折线图/柱状图数据
          const rawQuotaData = data.quota_data || [];
          if (rawQuotaData.length > 0) {
            const dataExportDefaultTime = getDefaultTime();
            const uniqueModels = new Set();
            rawQuotaData.forEach((item) => uniqueModels.add(item.model_name));

            const aggregatedData = aggregateDataByTimeAndModel(
              rawQuotaData,
              dataExportDefaultTime,
            );
            const chartTimePoints = generateChartTimePoints(
              aggregatedData,
              rawQuotaData,
              dataExportDefaultTime,
            );

            let newLineData = [];
            let totalQuota = 0;
            chartTimePoints.forEach((time) => {
              const timeData = Array.from(uniqueModels).map((model) => {
                const key = `${time}-${model}`;
                const aggregated = aggregatedData.get(key);
                if (aggregated) totalQuota += aggregated.quota;
                return {
                  Time: time,
                  Model: model,
                  rawQuota: aggregated?.quota || 0,
                  Usage: aggregated?.quota
                    ? getQuotaWithUnit(aggregated.quota, 4)
                    : 0,
                };
              });
              newLineData.push(...timeData);
            });
            newLineData.sort((a, b) => a.Time.localeCompare(b.Time));

            updateChartSpec(
              setSpecLine,
              newLineData,
              `${t('总计')}：${renderQuota(totalQuota, 2)}`,
              newModelColors,
              'barData',
            );
          } else {
            updateChartSpec(
              setSpecLine,
              [],
              `${t('总计')}：${renderQuota(0, 2)}`,
              {},
              'barData',
            );
          }
        }
      } catch (err) {
        console.error('Failed to load chart data:', err);
      } finally {
        setChartLoading(false);
      }
    },
    [t],
  );

  // ========== 加载日志列表 ==========
  const loadLogs = useCallback(
    async (page, size, customLogType = null) => {
      if (!tokenAPI.current) return;
      setLogLoading(true);
      try {
        const {
          model_name,
          request_id,
          startTs,
          endTs,
          logType: formLogType,
        } = getFormValues();
        const currentLogType =
          customLogType !== null ? customLogType : formLogType;

        const url = encodeURI(
          `/api/log/token?p=${page}&page_size=${size}&type=${currentLogType}&model_name=${model_name}&start_timestamp=${startTs}&end_timestamp=${endTs}&request_id=${request_id}`,
        );
        const res = await tokenAPI.current.get(url);
        if (res.data.success) {
          const data = res.data.data;
          setActivePage(data.page);
          setPageSize(data.page_size);
          setLogCount(data.total);
          formatLogs(data.items || []);
        }
      } catch (err) {
        console.error('Failed to load logs:', err);
      } finally {
        setLogLoading(false);
      }
    },
    [getFormValues, formatLogs],
  );

  // ========== 全量刷新（统计 + 图表 + 日志）==========
  const refreshAll = useCallback(async () => {
    const { startTs, endTs } = getGlobalTimeRange();
    setActivePage(1);
    await Promise.all([
      loadStat(startTs, endTs),
      loadChartData(startTs, endTs),
      loadLogs(1, pageSize),
    ]);
  }, [getGlobalTimeRange, loadStat, loadChartData, loadLogs, pageSize]);

  // ========== 初始化数据加载（认证成功后）==========
  useEffect(() => {
    if (authenticated) {
      refreshAll();
    }
  }, [authenticated]);

  // ========== 时间范围变更时刷新 ==========
  useEffect(() => {
    if (authenticated) {
      refreshAll();
    }
  }, [timePreset, customDateRange]);

  // ========== 分页 ==========
  const handlePageChange = useCallback(
    (page) => {
      setActivePage(page);
      loadLogs(page, pageSize);
    },
    [loadLogs, pageSize],
  );

  const handlePageSizeChange = useCallback(
    (size) => {
      setPageSize(size);
      setActivePage(1);
      loadLogs(1, size);
    },
    [loadLogs],
  );

  // ========== 表格列配置 ==========
  const allColumns = useMemo(() => {
    return getLogsColumns({
      t,
      COLUMN_KEYS,
      copyText,
      showUserInfoFunc: () => {},
      openChannelAffinityUsageCacheModal: () => {},
      isAdminUser: false,
      billingDisplayMode: 'price',
    });
  }, [t, COLUMN_KEYS, copyText]);

  const tableColumns = useMemo(() => {
    return allColumns.filter((col) => visibleColumns[col.key]);
  }, [allColumns, visibleColumns]);

  const hasExpandableRows = useCallback(() => {
    return logs.some(
      (log) => expandData[log.key] && expandData[log.key].length > 0,
    );
  }, [logs, expandData]);

  // ========== 未认证界面 ==========
  if (!authenticated) {
    return (
      <TokenLogAuthPanel
        t={t}
        apiKey={apiKey}
        setApiKey={setApiKey}
        authError={authError}
        authLoading={authLoading}
        onAuth={handleAuth}
      />
    );
  }

  // ========== 已认证界面 ==========
  return (
    <div className='px-4 py-4'>
      {/* 参数覆盖弹窗 */}
      <ParamOverrideModal
        showParamOverrideModal={showParamOverrideModal}
        setShowParamOverrideModal={setShowParamOverrideModal}
        paramOverrideTarget={paramOverrideTarget}
        t={t}
      />

      {/* 顶部：标题 + 全局时间选择器 + 操作 */}
      <div className='mb-4 flex flex-col gap-3'>
        <TokenLogHeader
          t={t}
          onSwitchKey={() => {
              setAuthenticated(false);
              setApiKey('');
              setStat(null);
              setLogs([]);
              tokenAPI.current = null;
            }}
        />
        <TokenLogTimeRangeControls
          t={t}
          timePreset={timePreset}
          setTimePreset={setTimePreset}
          customDateRange={customDateRange}
          setCustomDateRange={setCustomDateRange}
          onRefresh={refreshAll}
          loading={statLoading || chartLoading || logLoading}
        />
      </div>

      <TokenLogStatsCards t={t} stat={stat} loading={statLoading} />

      <TokenLogChartsPanel
        t={t}
        activeChartTab={activeChartTab}
        setActiveChartTab={setActiveChartTab}
        specPie={specPie}
        specLine={specLine}
        loading={chartLoading}
      />

      <TokenLogTablePanel
        t={t}
        formApi={formApi}
        setFormApi={setFormApi}
        tableColumns={tableColumns}
        hasExpandableRows={hasExpandableRows}
        expandData={expandData}
        logs={logs}
        activePage={activePage}
        pageSize={pageSize}
        logCount={logCount}
        loading={logLoading}
        onRefresh={refreshAll}
        onPageSizeChange={handlePageSizeChange}
        onPageChange={handlePageChange}
      />
    </div>
  );
};

export default LogViewer;
