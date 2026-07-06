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

import React, { useRef } from 'react';
import { Modal, Form, Button } from '@douyinfe/semi-ui';
import { QUICK_TIME_RANGE_OPTIONS } from '../../../constants/dashboard.constants';
import { getDashboardQuickTimeRange } from '../../../helpers/dashboard';

const SearchModal = ({
  searchModalVisible,
  handleSearchConfirm,
  handleCloseModal,
  isMobile,
  isAdminUser,
  inputs,
  dataExportDefaultTime,
  timeOptions,
  groupOptions,
  tokenOptions,
  handleInputChange,
  handleTokenSelect,
  t,
}) => {
  const formRef = useRef();

  const FORM_FIELD_PROPS = {
    className: 'w-full mb-2 !rounded-lg',
  };

  const createFormField = (Component, props) => (
    <Component {...FORM_FIELD_PROPS} {...props} />
  );

  const {
    start_timestamp,
    end_timestamp,
    username,
    token_names = [],
    groups = [],
  } = inputs;

  const handleQuickTimeRange = (rangeType) => {
    const range = getDashboardQuickTimeRange(rangeType);
    formRef.current?.setValue('start_timestamp', range.start_timestamp);
    formRef.current?.setValue('end_timestamp', range.end_timestamp);
    handleInputChange(range.start_timestamp, 'start_timestamp');
    handleInputChange(range.end_timestamp, 'end_timestamp');
  };

  return (
    <Modal
      title={t('搜索条件')}
      visible={searchModalVisible}
      onOk={handleSearchConfirm}
      onCancel={handleCloseModal}
      closeOnEsc={true}
      keepDOM={true}
      size={isMobile ? 'full-width' : 'small'}
      centered
    >
      <Form
        getFormApi={(formAPI) => (formRef.current = formAPI)}
        layout='vertical'
        className='w-full'
      >
        <div className='mb-3'>
          <div className='mb-2 text-sm font-medium'>{t('快速查询')}</div>
          <div className='flex flex-wrap gap-2'>
            {QUICK_TIME_RANGE_OPTIONS.map((option) => (
              <Button
                key={option.value}
                size='small'
                type='tertiary'
                htmlType='button'
                onClick={() => handleQuickTimeRange(option.value)}
              >
                {t(option.label)}
              </Button>
            ))}
          </div>
        </div>

        {createFormField(Form.DatePicker, {
          field: 'start_timestamp',
          label: t('起始时间'),
          initValue: start_timestamp,
          value: start_timestamp,
          type: 'dateTime',
          name: 'start_timestamp',
          onChange: (value) => handleInputChange(value, 'start_timestamp'),
        })}

        {createFormField(Form.DatePicker, {
          field: 'end_timestamp',
          label: t('结束时间'),
          initValue: end_timestamp,
          value: end_timestamp,
          type: 'dateTime',
          name: 'end_timestamp',
          onChange: (value) => handleInputChange(value, 'end_timestamp'),
        })}

        {createFormField(Form.Select, {
          field: 'data_export_default_time',
          label: t('时间粒度'),
          initValue: dataExportDefaultTime,
          placeholder: t('时间粒度'),
          name: 'data_export_default_time',
          optionList: timeOptions,
          onChange: (value) =>
            handleInputChange(value, 'data_export_default_time'),
        })}

        {isAdminUser &&
          createFormField(Form.Input, {
            field: 'username',
            label: t('用户名称'),
            value: username,
            placeholder: t('可选值'),
            name: 'username',
            onChange: (value) => handleInputChange(value, 'username'),
          })}

        {isAdminUser &&
          createFormField(Form.Select, {
            field: 'groups',
            label: t('分组'),
            value: groups,
            placeholder: t('全部'),
            name: 'groups',
            optionList: groupOptions,
            multiple: true,
            filter: true,
            showClear: true,
            autoClearSearchValue: false,
            searchPosition: 'dropdown',
            onChange: (value) =>
              handleInputChange(Array.isArray(value) ? value : [], 'groups'),
          })}

        {createFormField(Form.Select, {
          field: 'token_names',
          label: t('令牌名称'),
          value: token_names,
          placeholder: t('全部'),
          name: 'token_names',
          optionList: tokenOptions,
          multiple: true,
          filter: true,
          showClear: true,
          autoClearSearchValue: false,
          searchPosition: 'dropdown',
          onChange: (value) =>
            handleTokenSelect(Array.isArray(value) ? value : []),
        })}
      </Form>
    </Modal>
  );
};

export default SearchModal;
