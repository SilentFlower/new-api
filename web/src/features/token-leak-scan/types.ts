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

export type TokenLeakScanCredentials = {
  github_token_configured: boolean
  scan_secret_configured: boolean
  dingtalk_webhook_configured: boolean
  dingtalk_signing_configured: boolean
}

export type TokenLeakScanTaskState = {
  total: number
  processed: number
  progress: number
}

export type TokenLeakScanTaskResult = {
  total: number
  processed: number
  found: number
  not_found: number
  incomplete: number
  failed: number
  search_request_count: number
  stopped_reason?: string
}

export type TokenLeakScanTask = SystemTask<
  { token_id?: number },
  TokenLeakScanTaskState,
  TokenLeakScanTaskResult
>

export type TokenLeakScanCoverageStatus = {
  status: 'enabled' | 'disabled' | 'exhausted' | 'expired' | 'other'
  total_tokens: number
  pending_tokens: number
  last_scan_completed_at: number
}

export type TokenLeakScanStatus = {
  enabled: boolean
  interval_hours: number
  credentials: TokenLeakScanCredentials
  github_auth_status: 'unknown' | 'ok' | 'failed'
  github_auth_checked_at: number
  total_tokens: number
  enabled_tokens: number
  other_tokens: number
  scanned_tokens: number
  pending_tokens: number
  estimated_full_scan_minutes: number
  open_findings: number
  mitigated_findings: number
  coverage_by_status: TokenLeakScanCoverageStatus[]
  current_task: TokenLeakScanTask | null
  last_task: TokenLeakScanTask | null
  last_scheduled_task: TokenLeakScanTask | null
}

export type TokenLeakNotification = {
  id: number
  finding_id: number
  channel: 'root' | 'user' | 'dingtalk' | string
  trigger: string
  status: 'pending' | 'succeeded' | 'failed' | string
  attempt_count: number
  error_code: string
  completed_at: number
  created_at: number
  updated_at: number
}

export type TokenLeakFinding = {
  id: number
  token_id: number
  user_id: number
  token_name: string
  repository_id: number
  repository_name: string
  file_path: string
  blob_sha: string
  html_url: string
  status: 'open' | 'mitigated'
  first_found_at: number
  last_found_at: number
  last_notified_at: number
  last_reminder_at: number
  mitigated_at: number
  mitigation_reason: string
  notifications: TokenLeakNotification[]
}

export type TokenLeakFindingPage = {
  items: TokenLeakFinding[]
  total: number
  page: number
  page_size: number
}

export type TokenLeakScanSettingsValues = {
  'token_leak_scan.enabled': boolean
  'token_leak_scan.interval_hours': number
}

export type TokenLeakApiResponse<T> = {
  success: boolean
  message: string
  data?: T
}
