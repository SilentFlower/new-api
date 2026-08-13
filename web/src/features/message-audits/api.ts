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

import {
  type MessageAuditApiResponse,
  unwrapMessageAuditResponse,
} from './lib/message-audit-api'
import type {
  MessageAuditCleanupTask,
  MessageAuditDetail,
  MessageAuditListData,
  MessageAuditReview,
  MessageAuditReviewOptions,
  MessageAuditReviewTask,
  MessageAuditSearch,
  MessageAuditStatus,
} from './types'

/**
 * 返回经过校验的消息审计分页数据。
 *
 * @param search URL 同步的筛选与分页参数。
 * @returns 消息审计分页数据。
 */
export async function getMessageAudits(search: MessageAuditSearch) {
  const res = await api.get<MessageAuditApiResponse<MessageAuditListData>>(
    '/api/message-audit/',
    {
      params: {
        p: search.page,
        page_size: search.pageSize,
        username: search.username,
        token_name: search.token,
        model_name: search.model,
        request_id: search.requestId,
        request_path: search.path,
        status: search.status,
        start_timestamp: search.startTime,
        end_timestamp: search.endTime,
        audit_session_id: search.auditSessionId,
      },
    }
  )
  return unwrapMessageAuditResponse(res.data)
}

/**
 * 返回指定推断会话内的单次请求，按最新请求优先排列。
 *
 * @param auditSessionId 服务端生成的推断会话 ID。
 * @param page 页码，从 1 开始。
 * @param pageSize 每页请求数。
 * @returns 会话内单次请求分页数据。
 */
export async function getMessageAuditSessionRequests(
  auditSessionId: string,
  page: number,
  pageSize: number
) {
  return getMessageAudits({ auditSessionId, page, pageSize })
}

/**
 * 返回经过校验的单条消息审计详情。
 *
 * @param requestId 外部请求 ID。
 * @returns 已解密的单条审计详情。
 */
export async function getMessageAuditDetail(requestId: string) {
  const res = await api.get<MessageAuditApiResponse<MessageAuditDetail>>(
    `/api/message-audit/${encodeURIComponent(requestId)}`,
    { disableDuplicate: true }
  )
  return unwrapMessageAuditResponse(res.data)
}

/**
 * 返回当前节点的消息审计状态。
 *
 * @returns 当前节点配置与队列指标。
 */
export async function getMessageAuditStatus() {
  const res = await api.get<MessageAuditApiResponse<MessageAuditStatus>>(
    '/api/message-audit/status'
  )
  return unwrapMessageAuditResponse(res.data)
}

/**
 * 返回固定审核渠道、模型配置和可选项。
 *
 * @returns 不包含渠道密钥的审核配置。
 */
export async function getMessageAuditReviewOptions() {
  const res = await api.get<MessageAuditApiResponse<MessageAuditReviewOptions>>(
    '/api/message-audit/review-options'
  )
  return unwrapMessageAuditResponse(res.data)
}

/**
 * 返回推断会话当前审核状态和结果。
 *
 * @param auditSessionId 推断会话 ID。
 * @returns 当前结果、任务状态和新鲜度。
 */
export async function getMessageAuditReview(auditSessionId: string) {
  const res = await api.get<MessageAuditApiResponse<MessageAuditReview>>(
    `/api/message-audit/session/${encodeURIComponent(auditSessionId)}/review`,
    { disableDuplicate: true }
  )
  return unwrapMessageAuditResponse(res.data)
}

/**
 * 创建或复用推断会话的手动审核任务。
 *
 * @param auditSessionId 推断会话 ID。
 * @returns 系统任务和是否新建。
 */
export async function startMessageAuditReview(auditSessionId: string) {
  const res = await api.post<
    MessageAuditApiResponse<{ task: MessageAuditReviewTask; created: boolean }>
  >(`/api/message-audit/session/${encodeURIComponent(auditSessionId)}/review`)
  return unwrapMessageAuditResponse(res.data)
}

/**
 * 创建或复用消息审计清理任务。
 *
 * @returns 任务数据以及是否新建。
 */
export async function startMessageAuditCleanup() {
  const res = await api.post<
    MessageAuditApiResponse<{
      task: MessageAuditCleanupTask
      created: boolean
    }>
  >('/api/system-task/message-audit-cleanup')
  return unwrapMessageAuditResponse(res.data)
}

/**
 * 返回指定消息审计清理任务。
 *
 * @param taskId 系统任务 ID。
 * @returns 当前任务状态、进度和结果。
 */
export async function getMessageAuditCleanupTask(taskId: string) {
  const res = await api.get<MessageAuditApiResponse<MessageAuditCleanupTask>>(
    `/api/system-task/${taskId}`,
    { disableDuplicate: true }
  )
  return unwrapMessageAuditResponse(res.data)
}

/**
 * 返回当前活动的消息审计清理任务。
 *
 * @returns 活动任务；无活动任务时返回 null。
 */
export async function getCurrentMessageAuditCleanupTask() {
  const res = await api.get<
    MessageAuditApiResponse<MessageAuditCleanupTask | null>
  >('/api/system-task/current', {
    params: { type: 'message_audit_cleanup' },
    disableDuplicate: true,
  })
  return unwrapMessageAuditResponse(res.data)
}
