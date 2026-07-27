import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { MessageAuditCleanupTask } from '../types'
import {
  getMessageAuditCleanupProgress,
  getMessageAuditCleanupTitleKey,
  getMessageAuditErrorMessage,
  isMessageAuditCleanupActive,
  isMessageAuditClearConfirmed,
} from './message-audit-ui'

function cleanupTask(
  status: MessageAuditCleanupTask['status'],
  progress: number
): MessageAuditCleanupTask {
  return {
    id: 1,
    task_id: 'cleanup-1',
    type: 'message_audit_cleanup',
    status,
    state: { total: 10, processed: 5, remaining: 5, progress },
    created_at: 1,
    updated_at: 1,
  }
}

describe('消息审计清理交互状态', () => {
  test('确认文本必须精确匹配', () => {
    assert.equal(isMessageAuditClearConfirmed(''), false)
    assert.equal(isMessageAuditClearConfirmed('clear'), false)
    assert.equal(isMessageAuditClearConfirmed('CLEAR '), false)
    assert.equal(isMessageAuditClearConfirmed('CLEAR'), true)
  })

  test('仅 pending 和 running 禁止重复提交', () => {
    assert.equal(isMessageAuditCleanupActive(cleanupTask('pending', 0)), true)
    assert.equal(isMessageAuditCleanupActive(cleanupTask('running', 50)), true)
    assert.equal(
      isMessageAuditCleanupActive(cleanupTask('succeeded', 100)),
      false
    )
    assert.equal(isMessageAuditCleanupActive(cleanupTask('failed', 50)), false)
  })

  test('进度值被限制在进度条有效范围内', () => {
    assert.equal(getMessageAuditCleanupProgress(cleanupTask('running', -1)), 0)
    assert.equal(getMessageAuditCleanupProgress(cleanupTask('running', 45)), 45)
    assert.equal(
      getMessageAuditCleanupProgress(cleanupTask('succeeded', 120)),
      100
    )
  })

  test('任务标题覆盖进行中、完成和失败状态', () => {
    assert.equal(
      getMessageAuditCleanupTitleKey(cleanupTask('running', 50)),
      'Clearing message audits...'
    )
    assert.equal(
      getMessageAuditCleanupTitleKey(cleanupTask('succeeded', 100)),
      'Completed'
    )
    assert.equal(
      getMessageAuditCleanupTitleKey(cleanupTask('failed', 50)),
      'Cleanup failed'
    )
  })

  test('错误信息优先使用真实错误并支持回退', () => {
    assert.equal(
      getMessageAuditErrorMessage(
        new Error('database unavailable'),
        'fallback'
      ),
      'database unavailable'
    )
    assert.equal(getMessageAuditErrorMessage(null, 'fallback'), 'fallback')
  })
})
