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

/**
 * getUserAgentLogDetail 构造管理员日志中的原始 User-Agent 展开详情。
 * @param {object | null | undefined} other 日志附加信息。
 * @param {boolean} isAdminUser 当前查看者是否为管理员。
 * @param {(key: string) => string} t 国际化函数。
 * @returns {{key: string, value: React.ReactNode} | null} 可追加到展开详情的数据项。
 */
export const getUserAgentLogDetail = (other, isAdminUser, t) => {
  const userAgent = other?.admin_info?.user_agent;
  if (!isAdminUser || typeof userAgent !== 'string' || userAgent === '') {
    return null;
  }

  return {
    key: t('User Agent'),
    value: <span style={{ wordBreak: 'break-all' }}>{userAgent}</span>,
  };
};
