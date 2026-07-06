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

import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { API, isAdmin, showError, timestamp2string } from '../../helpers';
import { getDefaultTime, getInitialTimestamp } from '../../helpers/dashboard';
import {
  appendDashboardFilterParams,
  buildDashboardTokenOptionValue,
  extractDashboardTokenName,
  filterDashboardTokenOptionsByGroups,
  filterDashboardTokenValuesByGroups,
  normalizeSelectValues,
} from '../../helpers/dashboardFilters';
import { TIME_OPTIONS } from '../../constants/dashboard.constants';
import { useIsMobile } from '../common/useIsMobile';
import { useMinimumLoadingTime } from '../common/useMinimumLoadingTime';

export const useDashboardData = (userState, userDispatch, statusState) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const initialized = useRef(false);

  // ========== 基础状态 ==========
  const [loading, setLoading] = useState(false);
  const [exportLoading, setExportLoading] = useState(false);
  const [exportModalVisible, setExportModalVisible] = useState(false);
  const [greetingVisible, setGreetingVisible] = useState(false);
  const [searchModalVisible, setSearchModalVisible] = useState(false);
  const showLoading = useMinimumLoadingTime(loading);

  // ========== 输入状态 ==========
  const [inputs, setInputs] = useState({
    username: '',
    token_name: '',
    token_names: [],
    groups: [],
    model_name: '',
    start_timestamp: getInitialTimestamp(),
    end_timestamp: timestamp2string(new Date().getTime() / 1000 + 3600),
    channel: '',
    data_export_default_time: '',
  });

  const [dataExportDefaultTime, setDataExportDefaultTime] =
    useState(getDefaultTime());

  // ========== 筛选选项 ==========
  const [allTokenOptions, setAllTokenOptions] = useState([]);
  const [groupOptions, setGroupOptions] = useState([]);

  // ========== 数据状态 ==========
  const [quotaData, setQuotaData] = useState([]);
  const [consumeQuota, setConsumeQuota] = useState(0);
  const [consumeTokens, setConsumeTokens] = useState(0);
  const [times, setTimes] = useState(0);
  const [pieData, setPieData] = useState([{ type: 'null', value: '0' }]);
  const [lineData, setLineData] = useState([]);
  const [modelColors, setModelColors] = useState({});

  // ========== 图表状态 ==========
  const [activeChartTab, setActiveChartTab] = useState('1');

  // ========== 趋势数据 ==========
  const [trendData, setTrendData] = useState({
    balance: [],
    usedQuota: [],
    requestCount: [],
    times: [],
    consumeQuota: [],
    tokens: [],
    rpm: [],
    tpm: [],
  });

  // ========== Uptime 数据 ==========
  const [uptimeData, setUptimeData] = useState([]);
  const [uptimeLoading, setUptimeLoading] = useState(false);
  const [activeUptimeTab, setActiveUptimeTab] = useState('');

  // ========== 系统级统计（管理员专用） ==========
  const [systemStats, setSystemStats] = useState(null);

  // ========== 常量 ==========
  const now = new Date();
  const isAdminUser = isAdmin();

  // ========== Panel enable flags ==========
  const apiInfoEnabled = statusState?.status?.api_info_enabled ?? true;
  const announcementsEnabled =
    statusState?.status?.announcements_enabled ?? true;
  const faqEnabled = statusState?.status?.faq_enabled ?? true;
  const uptimeEnabled = statusState?.status?.uptime_kuma_enabled ?? true;

  const hasApiInfoPanel = apiInfoEnabled;
  const hasInfoPanels = announcementsEnabled || faqEnabled || uptimeEnabled;

  // ========== Memoized Values ==========
  const timeOptions = useMemo(
    () =>
      TIME_OPTIONS.map((option) => ({
        ...option,
        label: t(option.label),
      })),
    [t],
  );

  const tokenOptions = useMemo(
    () => filterDashboardTokenOptionsByGroups(allTokenOptions, inputs.groups),
    [allTokenOptions, inputs.groups],
  );

  const performanceMetrics = useMemo(() => {
    const { start_timestamp, end_timestamp } = inputs;
    const timeDiff =
      (Date.parse(end_timestamp) - Date.parse(start_timestamp)) / 60000;
    const avgRPM = isNaN(times / timeDiff)
      ? '0'
      : (times / timeDiff).toFixed(3);
    const avgTPM = isNaN(consumeTokens / timeDiff)
      ? '0'
      : (consumeTokens / timeDiff).toFixed(3);

    return { avgRPM, avgTPM, timeDiff };
  }, [times, consumeTokens, inputs.start_timestamp, inputs.end_timestamp]);

  const getGreeting = useMemo(() => {
    const hours = new Date().getHours();
    let greeting = '';

    if (hours >= 5 && hours < 12) {
      greeting = t('早上好');
    } else if (hours >= 12 && hours < 14) {
      greeting = t('中午好');
    } else if (hours >= 14 && hours < 18) {
      greeting = t('下午好');
    } else {
      greeting = t('晚上好');
    }

    const username = userState?.user?.username || '';
    return `👋${greeting}，${username}`;
  }, [t, userState?.user?.username]);

  // ========== 回调函数 ==========
  const handleInputChange = useCallback(
    (value, name) => {
      if (name === 'data_export_default_time') {
        setDataExportDefaultTime(value);
        localStorage.setItem('data_export_default_time', value);
        return;
      }
      if (name === 'groups') {
        const groups = normalizeSelectValues(value);
        setInputs((inputs) => {
          const tokenNames = filterDashboardTokenValuesByGroups(
            inputs.token_names,
            groups,
            allTokenOptions,
          );
          return {
            ...inputs,
            groups,
            token_names: tokenNames,
            token_name:
              tokenNames.length === 1
                ? extractDashboardTokenName(tokenNames[0])
                : '',
          };
        });
        return;
      }
      if (name === 'token_names') {
        const tokenNames = normalizeSelectValues(value);
        setInputs((inputs) => ({
          ...inputs,
          token_names: tokenNames,
          token_name:
            tokenNames.length === 1
              ? extractDashboardTokenName(tokenNames[0])
              : '',
        }));
        return;
      }
      setInputs((inputs) => ({ ...inputs, [name]: value }));
    },
    [allTokenOptions],
  );

  const loadGroupOptions = useCallback(async () => {
    if (!isAdminUser) {
      setGroupOptions([]);
      return;
    }
    try {
      const res = await API.get('/api/group/');
      const { success, data } = res.data;
      if (success && Array.isArray(data)) {
        setGroupOptions(data.map((group) => ({ value: group, label: group })));
      }
    } catch (err) {
      console.error('Failed to load group options:', err);
    }
  }, [isAdminUser]);

  // 加载令牌列表用于下拉选择
  const loadTokenOptions = useCallback(async () => {
    try {
      let tokens = [];
      if (isAdminUser) {
        // 管理员：获取所有用户的令牌名称，附带用户名以区分同名令牌
        const res = await API.get('/api/data/token-names');
        const { success, data } = res.data;
        if (success && data) {
          tokens = data.map((item) => ({
            value: buildDashboardTokenOptionValue(
              item.name,
              item.username,
              item.group,
            ),
            label: item.username
              ? `${item.name} (${item.username}${item.group ? ` / ${item.group}` : ''})`
              : item.name,
            group: item.group || '',
          }));
        }
      } else {
        // 普通用户：只获取自己的令牌
        const res = await API.get('/api/token/?p=1&size=100');
        const { success, data } = res.data;
        if (success && data && data.items) {
          tokens = data.items.map((token) => ({
            value: token.name,
            label: token.name,
          }));
        }
      }
      setAllTokenOptions(tokens);
    } catch (err) {
      console.error('Failed to load token options:', err);
    }
  }, [isAdminUser]);

  const handleTokenSelect = useCallback(
    (value) => {
      handleInputChange(value, 'token_names');
    },
    [handleInputChange],
  );

  const loadFilterOptions = useCallback(() => {
    loadGroupOptions();
    loadTokenOptions();
  }, [loadGroupOptions, loadTokenOptions]);

  const showSearchModal = useCallback(() => {
    loadFilterOptions();
    setSearchModalVisible(true);
  }, [loadFilterOptions]);

  const handleCloseModal = useCallback(() => {
    setSearchModalVisible(false);
  }, []);

  // 加载系统级统计数据（管理员专用）
  const loadSystemStats = useCallback(async () => {
    if (!isAdminUser) return;
    try {
      const res = await API.get('/api/data/system-stats');
      const { success, data } = res.data;
      if (success && data) {
        setSystemStats(data);
      }
    } catch (err) {
      console.error('Failed to load system stats:', err);
    }
  }, [isAdminUser]);

  // ========== API 调用函数 ==========
  const loadQuotaData = useCallback(async () => {
    setLoading(true);
    try {
      const { start_timestamp, end_timestamp, username, token_names, groups } =
        inputs;
      let localStartTimestamp = Date.parse(start_timestamp) / 1000;
      let localEndTimestamp = Date.parse(end_timestamp) / 1000;
      const params = new URLSearchParams();
      params.append('start_timestamp', localStartTimestamp);
      params.append('end_timestamp', localEndTimestamp);
      params.append('default_time', dataExportDefaultTime);
      if (isAdminUser && username) {
        params.append('username', username);
      }
      appendDashboardFilterParams(
        params,
        'token_names',
        token_names,
        extractDashboardTokenName,
      );
      if (isAdminUser) {
        appendDashboardFilterParams(params, 'groups', groups);
      }
      const url = isAdminUser
        ? `/api/data/?${params.toString()}`
        : `/api/data/self/?${params.toString()}`;

      const res = await API.get(url);
      const { success, message, data } = res.data;
      if (success) {
        setQuotaData(data);
        if (data.length === 0) {
          data.push({
            count: 0,
            model_name: '无数据',
            quota: 0,
            created_at: now.getTime() / 1000,
          });
        }
        data.sort((a, b) => a.created_at - b.created_at);
        return data;
      } else {
        showError(message);
        return [];
      }
    } finally {
      setLoading(false);
    }
  }, [inputs, dataExportDefaultTime, isAdminUser, now]);

  const loadUptimeData = useCallback(async () => {
    setUptimeLoading(true);
    try {
      const res = await API.get('/api/uptime/status');
      const { success, message, data } = res.data;
      if (success) {
        setUptimeData(data || []);
        if (data && data.length > 0 && !activeUptimeTab) {
          setActiveUptimeTab(data[0].categoryName);
        }
      } else {
        showError(message);
      }
    } catch (err) {
      console.error(err);
    } finally {
      setUptimeLoading(false);
    }
  }, [activeUptimeTab]);

  const loadUserQuotaData = useCallback(async () => {
    if (!isAdminUser) return [];
    try {
      const { start_timestamp, end_timestamp, username, token_names, groups } =
        inputs;
      const localStartTimestamp = Date.parse(start_timestamp) / 1000;
      const localEndTimestamp = Date.parse(end_timestamp) / 1000;
      const params = new URLSearchParams();
      params.append('start_timestamp', localStartTimestamp);
      params.append('end_timestamp', localEndTimestamp);
      if (username) {
        params.append('username', username);
      }
      appendDashboardFilterParams(
        params,
        'token_names',
        token_names,
        extractDashboardTokenName,
      );
      appendDashboardFilterParams(params, 'groups', groups);
      const url = `/api/data/users?${params.toString()}`;
      const res = await API.get(url);
      const { success, message, data } = res.data;
      if (success) {
        return data || [];
      } else {
        showError(message);
        return [];
      }
    } catch (err) {
      console.error(err);
      return [];
    }
  }, [inputs, isAdminUser]);

  const getUserData = useCallback(async () => {
    let res = await API.get(`/api/user/self`);
    const { success, message, data } = res.data;
    if (success) {
      userDispatch({ type: 'login', payload: data });
    } else {
      showError(message);
    }
  }, [userDispatch]);

  const refresh = useCallback(async () => {
    const data = await loadQuotaData();
    await loadUptimeData();
    loadSystemStats();
    return data;
  }, [loadQuotaData, loadUptimeData, loadSystemStats]);

  const handleSearchConfirm = useCallback(
    async (updateChartDataCallback) => {
      const data = await refresh();
      if (data && data.length > 0 && updateChartDataCallback) {
        updateChartDataCallback(data);
      }
      setSearchModalVisible(false);
    },
    [refresh],
  );

  // ========== 导出 Excel ==========
  const showExportModal = useCallback(() => {
    loadFilterOptions();
    setExportModalVisible(true);
  }, [loadFilterOptions]);

  const closeExportModal = useCallback(() => {
    setExportModalVisible(false);
  }, []);

  const exportExcel = useCallback(
    async (
      startTime,
      endTime,
      selectedTokenValues = [],
      selectedGroups = [],
    ) => {
      setExportLoading(true);
      try {
        let localStartTimestamp = Date.parse(startTime) / 1000;
        let localEndTimestamp = Date.parse(endTime) / 1000;
        const params = new URLSearchParams();
        params.append('start_timestamp', localStartTimestamp);
        params.append('end_timestamp', localEndTimestamp);

        appendDashboardFilterParams(
          params,
          'token_names',
          selectedTokenValues,
          extractDashboardTokenName,
        );
        appendDashboardFilterParams(params, 'groups', selectedGroups);

        const res = await API.get(`/api/data/export?${params.toString()}`, {
          responseType: 'blob',
          disableDuplicate: true,
        });

        // 检查响应是否为 JSON 错误（Content-Type 为 application/json 说明返回了错误）
        const contentType = res.headers['content-type'] || '';
        if (contentType.includes('application/json')) {
          const text = await res.data.text();
          const errorData = JSON.parse(text);
          showError(errorData.message || t('导出失败'));
          return;
        }

        // 从 Content-Disposition 中提取文件名，或使用默认文件名
        const disposition = res.headers['content-disposition'] || '';
        let fileName = `数据报表.xlsx`;
        const filenameMatch = disposition.match(
          /filename\*?=(?:UTF-8'')?(.+)/i,
        );
        if (filenameMatch) {
          fileName = decodeURIComponent(filenameMatch[1]);
        }

        // 创建 Blob URL 并触发下载
        const blob = new Blob([res.data], {
          type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
        });
        const url = window.URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = fileName;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        window.URL.revokeObjectURL(url);
      } catch (err) {
        console.error('Export failed:', err);
        showError(t('导出失败'));
      } finally {
        setExportLoading(false);
        setExportModalVisible(false);
      }
    },
    [t],
  );

  // ========== Effects ==========
  useEffect(() => {
    const timer = setTimeout(() => {
      setGreetingVisible(true);
    }, 100);
    return () => clearTimeout(timer);
  }, []);

  useEffect(() => {
    if (!initialized.current) {
      getUserData();
      loadSystemStats();
      initialized.current = true;
    }
  }, [getUserData, loadSystemStats]);

  return {
    // 基础状态
    loading: showLoading,
    exportLoading,
    exportModalVisible,
    greetingVisible,
    searchModalVisible,

    // 输入状态
    inputs,
    dataExportDefaultTime,
    groupOptions,
    tokenOptions,
    allTokenOptions,

    // 数据状态
    quotaData,
    consumeQuota,
    setConsumeQuota,
    consumeTokens,
    setConsumeTokens,
    times,
    setTimes,
    pieData,
    setPieData,
    lineData,
    setLineData,
    modelColors,
    setModelColors,

    // 图表状态
    activeChartTab,
    setActiveChartTab,

    // 趋势数据
    trendData,
    setTrendData,

    // Uptime 数据
    uptimeData,
    uptimeLoading,
    activeUptimeTab,
    setActiveUptimeTab,

    // 系统级统计（管理员）
    systemStats,

    // 计算值
    timeOptions,
    performanceMetrics,
    getGreeting,
    isAdminUser,
    hasApiInfoPanel,
    hasInfoPanels,
    apiInfoEnabled,
    announcementsEnabled,
    faqEnabled,
    uptimeEnabled,

    // 函数
    handleInputChange,
    handleTokenSelect,
    showSearchModal,
    handleCloseModal,
    showExportModal,
    closeExportModal,
    exportExcel,
    loadQuotaData,
    loadUserQuotaData,
    loadUptimeData,
    getUserData,
    refresh,
    handleSearchConfirm,

    // 导航和翻译
    navigate,
    t,
    isMobile,
  };
};
