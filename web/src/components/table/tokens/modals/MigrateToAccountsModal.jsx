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

import React, { useEffect, useMemo, useState } from 'react';
import { Modal, Banner, Table, Tag, Button, Space } from '@douyinfe/semi-ui';
import { migrateTokensToAccounts } from '../../../../helpers/token';
import { showError } from '../../../../helpers';

/**
 * MigrateToAccountsModal — 「将所选令牌迁移到独立账号」二步弹窗。
 *
 * 第一屏：确认提示 + 选中令牌简表 + 「确认迁移」按钮；
 * 第二屏：迁移结果表格（令牌名 / 新用户名 / 状态），失败行高亮。
 *
 * Props:
 *   - visible: 是否显示弹窗
 *   - selectedTokens: 当前勾选的令牌完整对象数组（至少含 id、name、key）
 *   - onCancel: 取消 / 关闭
 *   - onSuccess: 成功后的回调（一般用于刷新令牌列表 + 关闭弹窗）
 *   - t: i18next 翻译函数
 */
const MigrateToAccountsModal = ({
  visible,
  selectedTokens = [],
  onCancel,
  onSuccess,
  t,
}) => {
  // step: 'confirm' = 第一屏（确认）；'result' = 第二屏（结果）
  const [step, setStep] = useState('confirm');
  const [loading, setLoading] = useState(false);
  const [results, setResults] = useState([]);

  // 弹窗每次重新打开时重置状态，避免上次的结果残留
  useEffect(() => {
    if (visible) {
      setStep('confirm');
      setLoading(false);
      setResults([]);
    }
  }, [visible]);

  const totalCount = selectedTokens.length;
  const successCount = useMemo(
    () => results.filter((r) => r.status === 'success').length,
    [results],
  );
  const failedCount = useMemo(
    () => results.filter((r) => r.status === 'failed').length,
    [results],
  );

  // 第一屏：选中令牌简表的列定义
  const previewColumns = useMemo(
    () => [
      {
        title: t('令牌名'),
        dataIndex: 'name',
        render: (text) => text || '-',
      },
      {
        title: t('密钥'),
        dataIndex: 'key',
        // key 已经在 list 接口被 mask 处理过，这里仅展示
        render: (text) => text || '-',
      },
    ],
    [t],
  );

  // 第二屏：结果表格的列定义
  const resultColumns = useMemo(
    () => [
      {
        title: t('令牌名'),
        dataIndex: 'token_name',
        render: (text) => text || '-',
      },
      {
        title: t('新用户名'),
        dataIndex: 'new_username',
        render: (text, record) =>
          record.status === 'success' ? text || '-' : '-',
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        render: (status, record) => {
          if (status === 'success') {
            return <Tag color='green'>{t('迁移成功')}</Tag>;
          }
          return (
            <Space>
              <Tag color='red'>{t('迁移失败')}</Tag>
              {record.error ? (
                <span style={{ color: 'var(--semi-color-danger)' }}>
                  {record.error}
                </span>
              ) : null}
            </Space>
          );
        },
      },
    ],
    [t],
  );

  // 触发迁移请求
  const handleConfirm = async () => {
    if (totalCount === 0) {
      return;
    }
    setLoading(true);
    try {
      const ids = selectedTokens.map((tk) => tk.id);
      const data = await migrateTokensToAccounts(ids);
      setResults(Array.isArray(data?.results) ? data.results : []);
      setStep('result');
    } catch (e) {
      // showError 会被全局响应拦截器调用一次；这里再保险一次本地展示。
      showError(e?.message || t('迁移失败'));
    } finally {
      setLoading(false);
    }
  };

  const handleClose = () => {
    if (step === 'result') {
      // 结果屏关闭时通知父组件刷新列表
      if (typeof onSuccess === 'function') {
        onSuccess();
      }
    } else if (typeof onCancel === 'function') {
      onCancel();
    }
  };

  const renderConfirmStep = () => (
    <>
      <Banner
        type='warning'
        description={t(
          '将为每个所选令牌创建一个新用户、并把令牌的归属切换到新用户上。原令牌的密钥与分组保持不变，外部调用方无感。后续登录新用户需在「用户管理 → 编辑用户」中重置密码。',
        )}
        closeIcon={null}
        style={{ marginBottom: 16 }}
      />
      <div style={{ marginBottom: 8 }}>
        {t('确认要迁移以下 {{count}} 个令牌吗？', { count: totalCount })}
      </div>
      <Table
        size='small'
        rowKey='id'
        columns={previewColumns}
        dataSource={selectedTokens}
        pagination={false}
        scroll={{ y: 320 }}
      />
    </>
  );

  const renderResultStep = () => (
    <>
      <Banner
        type={failedCount === 0 ? 'success' : 'warning'}
        description={t(
          '迁移完成：成功 {{success}} 个，失败 {{failed}} 个，共 {{total}} 个。',
          {
            success: successCount,
            failed: failedCount,
            total: results.length,
          },
        )}
        closeIcon={null}
        style={{ marginBottom: 16 }}
      />
      <Table
        size='small'
        rowKey='token_id'
        columns={resultColumns}
        dataSource={results}
        pagination={false}
        scroll={{ y: 360 }}
      />
    </>
  );

  return (
    <Modal
      title={t('迁移到独立账号')}
      visible={visible}
      onCancel={handleClose}
      maskClosable={!loading}
      closeOnEsc={!loading}
      footer={
        step === 'confirm' ? (
          <Space>
            <Button
              type='tertiary'
              onClick={handleClose}
              disabled={loading}
            >
              {t('取消')}
            </Button>
            <Button
              type='primary'
              theme='solid'
              loading={loading}
              onClick={handleConfirm}
              disabled={totalCount === 0}
            >
              {t('确认迁移')}
            </Button>
          </Space>
        ) : (
          <Button type='primary' onClick={handleClose}>
            {t('关闭')}
          </Button>
        )
      }
      width={720}
    >
      {step === 'confirm' ? renderConfirmStep() : renderResultStep()}
    </Modal>
  );
};

export default MigrateToAccountsModal;
