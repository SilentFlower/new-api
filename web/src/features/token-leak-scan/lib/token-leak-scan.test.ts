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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type {
  TokenLeakFinding,
  TokenLeakNotification,
  TokenLeakScanTask,
} from '../types'
import {
  canSubmitTokenLeakDisable,
  isTokenLeakScanTaskActive,
  parseTokenLeakIntervalInput,
  parseTokenLeakTokenID,
  selectLatestTokenLeakNotificationsByChannel,
} from './token-leak-scan'

const buildTask = (status: TokenLeakScanTask['status']): TokenLeakScanTask => ({
  id: 1,
  task_id: 'task-1',
  type: 'token_leak_scan_manual',
  status,
  created_at: 1,
  updated_at: 1,
})

const buildNotification = (
  id: number,
  channel: string
): TokenLeakNotification => ({
  id,
  finding_id: 1,
  channel,
  trigger: 'first',
  status: 'succeeded',
  attempt_count: 1,
  error_code: '',
  completed_at: 1,
  created_at: 1,
  updated_at: 1,
})

const openFinding: TokenLeakFinding = {
  id: 1,
  token_id: 2,
  user_id: 3,
  token_name: 'production',
  repository_id: 4,
  repository_name: 'public/repo',
  file_path: 'config/key.txt',
  blob_sha: 'sha',
  html_url: 'https://github.com/public/repo/blob/main/config/key.txt',
  status: 'open',
  first_found_at: 1,
  last_found_at: 1,
  last_notified_at: 1,
  last_reminder_at: 0,
  mitigated_at: 0,
  mitigation_reason: '',
  notifications: [],
}

describe('token leak scan UI logic', () => {
  test('只对等待中和运行中的任务继续轮询', () => {
    assert.equal(isTokenLeakScanTaskActive(buildTask('pending')), true)
    assert.equal(isTokenLeakScanTaskActive(buildTask('running')), true)
    assert.equal(isTokenLeakScanTaskActive(buildTask('succeeded')), false)
    assert.equal(isTokenLeakScanTaskActive(buildTask('failed')), false)
  })

  test('严格拒绝混合字符、零值和小数 Token ID', () => {
    assert.equal(parseTokenLeakTokenID('12'), 12)
    assert.equal(parseTokenLeakTokenID('12abc'), null)
    assert.equal(parseTokenLeakTokenID('0'), null)
    assert.equal(parseTokenLeakTokenID('1.5'), null)
  })

  test('非法小时输入不会把 NaN 写入表单', () => {
    assert.equal(parseTokenLeakIntervalInput('', Number.NaN), 0)
    assert.equal(parseTokenLeakIntervalInput('invalid', Number.NaN), 0)
    assert.equal(parseTokenLeakIntervalInput('24', 24), 24)
  })

  test('finding 列表按渠道展示最新通知审计', () => {
    const result = selectLatestTokenLeakNotificationsByChannel([
      buildNotification(1, 'root'),
      buildNotification(4, 'user'),
      buildNotification(3, 'root'),
      buildNotification(2, 'dingtalk'),
    ])

    assert.deepEqual(
      result.map((notification) => [notification.channel, notification.id]),
      [
        ['user', 4],
        ['root', 3],
        ['dingtalk', 2],
      ]
    )
  })

  test('只有开放 finding 且请求空闲时允许确认禁用', () => {
    assert.equal(canSubmitTokenLeakDisable(openFinding, false), true)
    assert.equal(canSubmitTokenLeakDisable(openFinding, true), false)
    assert.equal(
      canSubmitTokenLeakDisable({ ...openFinding, status: 'mitigated' }, false),
      false
    )
    assert.equal(canSubmitTokenLeakDisable(null, false), false)
  })
})
