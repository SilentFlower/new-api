/*
Copyright (C) 2023-2026 QuantumNous

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
export type MessageAuditApiResponse<T> = {
  success: boolean
  message: string
  data?: T
}

/**
 * 校验消息审计管理 API 的统一响应，并返回必需的数据字段。
 *
 * @param response 管理 API 返回的统一响应。
 * @returns 响应中的 data，允许调用方显式使用 null 表示无活动任务。
 */
export function unwrapMessageAuditResponse<T>(
  response: MessageAuditApiResponse<T>
): T {
  if (!response.success) {
    throw new Error(response.message)
  }
  if (response.data === undefined) {
    throw new Error(response.message)
  }
  return response.data
}
