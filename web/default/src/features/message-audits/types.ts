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
  captured_plaintext_bytes: number | null
  stored_payload_bytes: number | null
  dedup_saved_bytes: number
  review_status: MessageAuditReviewStatus
  review_risk_level: MessageAuditRiskLevel | ''
  review_stale: boolean
  reviewed_at: number
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

export type MessageAuditRiskLevel = 'none' | 'low' | 'medium' | 'high'

export type MessageAuditReviewStatus =
  | 'unreviewed'
  | 'pending'
  | 'running'
  | 'failed'
  | 'succeeded'

export type MessageAuditReviewFinding = {
  category: string
  severity: Exclude<MessageAuditRiskLevel, 'none'>
  file_id: string
  start_sequence: number
  end_sequence: number
  reason: string
}

export type MessageAuditReviewCoverage = {
  file_id: string
  start_sequence: number
  end_sequence: number
  start_cursor?: number
  end_cursor?: number
  estimated_tokens: number
}

export type MessageAuditReviewResult = {
  summary: string
  risk_level: MessageAuditRiskLevel
  categories: string[]
  findings: MessageAuditReviewFinding[]
  coverage: MessageAuditReviewCoverage[]
  uncovered: { file_id: string; reason: string }[]
}

export type MessageAuditReviewCallDiagnostic = {
  attempt: number
  phase: string
  protocol: string
  outcome: string
  duration_ms: number
  tool_call_count: number
  tool_names: string[]
  http_status: number
  error_stage: string
}

export type MessageAuditReviewDiagnostics = {
  channel_id: number
  model: string
  started_at: number
  finished_at: number
  duration_ms: number
  model_calls: number
  tool_calls: number
  tool_tokens: number
  tool_call_limit: number
  text_tool_fallback: boolean
  stage: string
  failure_code: string
  calls: MessageAuditReviewCallDiagnostic[]
}

export type MessageAuditReview = {
  audit_session_id: string
  status: MessageAuditReviewStatus
  risk_level: MessageAuditRiskLevel | ''
  stale: boolean
  reviewed_request_id: string
  current_request_id: string
  task_id: string
  review_channel_id: number
  review_model: string
  failure_code: string
  reviewed_at: number
  diagnostics?: MessageAuditReviewDiagnostics
  result?: MessageAuditReviewResult
}

export type MessageAuditReviewOptions = {
  config: { channel_id: number; model: string; tool_call_limit: number }
  channels: { id: number; name: string; models: string[] }[]
}

export type MessageAuditReviewTask = SystemTask<
  {
    audit_session_id: string
    target_request_id: string
    source_request_ids: string[]
    user_id: number
    operator_id: number
    config: { channel_id: number; model: string; tool_call_limit: number }
  },
  null,
  null
>

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
