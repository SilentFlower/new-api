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
import type { SystemTask } from '@/features/system-settings/types'

export type MessageAuditRequest = {
  id: number
  request_id: string
  audit_session_id: string
  parent_request_id: string
  session_match: 'new' | 'exact' | 'prefix' | 'compressed' | string
  session_request_count: number
  compressed_request_count: number
  user_id: number
  username: string
  token_id: number
  token_name: string
  model_name: string
  request_path: string
  protocol: string
  status: string
  audit_status: string
  error_code: string
  finish_reason: string
  http_status: number
  is_stream: boolean
  message_count: number
  tool_count: number
  plaintext_bytes: number
  dedup_saved_bytes: number
  duration_ms: number
  captured_at: number
  finalized_at: number
}

export type MessageAuditListData = {
  page: number
  page_size: number
  total: number
  items: MessageAuditRequest[]
}

export type MessageAuditMessage = {
  sequence: number
  role: string
  content_type: string
  content: unknown
}

export type MessageAuditDetail = {
  request: MessageAuditRequest
  messages: MessageAuditMessage[]
}

export type MessageAuditStatus = {
  enabled: boolean
  key_configured: boolean
  key_fingerprint?: string
  retention_days: number
  queue_depth: number
  queue_capacity: number
  queue_bytes: number
  queue_byte_capacity: number
  succeeded: number
  retries: number
  failed: number
  dropped: number
  storage_bytes: number
  storage_estimated: boolean
  payload_bytes: number
  request_count: number
  blob_count: number
  item_count: number
}

export type MessageAuditCleanupTask = SystemTask<
  { target_timestamp: number; batch_size: number; source: string },
  { total: number; processed: number; progress: number; remaining: number },
  { deleted_requests: number; deleted_blobs: number }
>

export type MessageAuditSearch = {
  page?: number
  pageSize?: number
  username?: string
  token?: string
  model?: string
  requestId?: string
  path?: string
  status?: string
  startTime?: number
  endTime?: number
  auditSessionId?: string
}
