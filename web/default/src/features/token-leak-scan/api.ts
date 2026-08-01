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
import { api } from '@/lib/api'

import type {
  TokenLeakApiResponse,
  TokenLeakFindingPage,
  TokenLeakScanStatus,
  TokenLeakScanTask,
} from './types'

function unwrapTokenLeakResponse<T>(response: TokenLeakApiResponse<T>): T {
  if (!response.success || response.data === undefined) {
    throw new Error(response.message)
  }
  return response.data
}

/**
 * 获取 root 管理端的泄露扫描状态。
 *
 * @returns 不含任何凭据明文的扫描状态。
 */
export async function getTokenLeakScanStatus(): Promise<TokenLeakScanStatus> {
  const response = await api.get<TokenLeakApiResponse<TokenLeakScanStatus>>(
    '/api/token-leak-scan/status'
  )
  return unwrapTokenLeakResponse(response.data)
}

/**
 * 分页获取已确认的公开泄露位置。
 *
 * @param page 页码，从 1 开始。
 * @param pageSize 每页数量。
 * @param status 可选处置状态。
 * @returns 泄露位置分页数据。
 */
export async function getTokenLeakFindings(
  page: number,
  pageSize: number,
  status?: string
): Promise<TokenLeakFindingPage> {
  const response = await api.get<TokenLeakApiResponse<TokenLeakFindingPage>>(
    '/api/token-leak-scan/findings',
    {
      params: {
        page,
        page_size: pageSize,
        status,
      },
    }
  )
  return unwrapTokenLeakResponse(response.data)
}

/**
 * 创建或复用一个全量或单 Token 扫描任务。
 *
 * @param tokenId 可选 Token ID；省略时扫描全部令牌。
 * @returns 系统任务及是否为本次新建。
 */
export async function startTokenLeakScan(
  tokenId?: number
): Promise<{ task: TokenLeakScanTask; created: boolean }> {
  const response = await api.post<
    TokenLeakApiResponse<{ task: TokenLeakScanTask; created: boolean }>
  >(
    '/api/token-leak-scan/run',
    { token_id: tokenId ?? 0 },
    { skipBusinessError: true }
  )
  return unwrapTokenLeakResponse(response.data)
}

/**
 * 禁用泄露位置对应的用户 Token。
 *
 * @param findingId 泄露位置 ID。
 * @returns 被禁用的 Token ID 与状态。
 */
export async function disableTokenLeakFinding(
  findingId: number
): Promise<{ token_id: number; status: number }> {
  const response = await api.post<
    TokenLeakApiResponse<{ token_id: number; status: number }>
  >(`/api/token-leak-scan/findings/${findingId}/disable-token`, undefined, {
    skipBusinessError: true,
  })
  return unwrapTokenLeakResponse(response.data)
}
