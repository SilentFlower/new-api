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
import { Form } from '@douyinfe/semi-ui';

/**
 * 渲染渠道级 Responses Compact 透传能力开关。
 *
 * @param {{ onChange: (value: boolean) => void }} props 组件参数。
 * @returns {React.ReactElement} 与渠道表单绑定的开关字段。
 */
const ResponsesCompactPassthroughSetting = (props) => {
  const { t } = useTranslation();

  return (
    <Form.Switch
      field='responses_compact_passthrough_enabled'
      label={t('启用 Responses Compact 透传')}
      checkedText={t('开')}
      uncheckedText={t('关')}
      onChange={props.onChange}
      extraText={t(
        '完成常规渠道选择和亲和性命中后，将 Compact 请求透传到此渠道',
      )}
    />
  );
};

export default ResponsesCompactPassthroughSetting;
