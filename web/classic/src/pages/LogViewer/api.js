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

import axios from 'axios';

/**
 * createTokenAPI - 创建一个带有 API Key 认证的 axios 实例
 * 用于公共日志查看器的所有 API 请求
 * @param {string} apiKey - 用户输入的 API Key
 * @returns {import('axios').AxiosInstance}
 */
export const createTokenAPI = (apiKey) => {
  return axios.create({
    baseURL: import.meta.env.VITE_REACT_APP_SERVER_URL
      ? import.meta.env.VITE_REACT_APP_SERVER_URL
      : '',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      'Cache-Control': 'no-store',
    },
  });
};
