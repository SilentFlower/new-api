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

import React, { useRef, useState } from 'react';
import { Modal, Form } from '@douyinfe/semi-ui';
import { timestamp2string } from '../../../helpers';

const ExportModal = ({
  visible,
  onConfirm,
  onCancel,
  loading,
  isMobile,
  t,
}) => {
  const formRef = useRef();

  // 默认起始时间：30天前，结束时间：当前时间+1小时
  const now = new Date();
  const [startTime, setStartTime] = useState(
    timestamp2string(now.getTime() / 1000 - 86400 * 30),
  );
  const [endTime, setEndTime] = useState(
    timestamp2string(now.getTime() / 1000 + 3600),
  );

  const FORM_FIELD_PROPS = {
    className: 'w-full mb-2 !rounded-lg',
  };

  const createFormField = (Component, props) => (
    <Component {...FORM_FIELD_PROPS} {...props} />
  );

  const handleOk = () => {
    onConfirm(startTime, endTime);
  };

  return (
    <Modal
      title={t('导出报表')}
      visible={visible}
      onOk={handleOk}
      onCancel={onCancel}
      closeOnEsc={true}
      okText={t('导出')}
      cancelText={t('取消')}
      confirmLoading={loading}
      size={isMobile ? 'full-width' : 'small'}
      centered
    >
      <Form ref={formRef} layout='vertical' className='w-full'>
        {createFormField(Form.DatePicker, {
          field: 'export_start_timestamp',
          label: t('起始时间'),
          initValue: startTime,
          value: startTime,
          type: 'dateTime',
          name: 'export_start_timestamp',
          onChange: (value) => setStartTime(value),
        })}

        {createFormField(Form.DatePicker, {
          field: 'export_end_timestamp',
          label: t('结束时间'),
          initValue: endTime,
          value: endTime,
          type: 'dateTime',
          name: 'export_end_timestamp',
          onChange: (value) => setEndTime(value),
        })}
      </Form>
    </Modal>
  );
};

export default ExportModal;
