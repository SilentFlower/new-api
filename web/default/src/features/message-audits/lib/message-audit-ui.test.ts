import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { MessageAuditCleanupTask } from '../types'
import {
  filterMessageAuditMessages,
  getMessageAuditCleanupProgress,
  getMessageAuditCleanupTitleKey,
  getMessageAuditErrorMessage,
  getMessageAuditSessionMatchLabelKey,
  isMessageAuditCleanupActive,
  isMessageAuditClearConfirmed,
  keepMessageAuditSessionPlaceholder,
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

describe('消息审计详情过滤', () => {
  const messages = [
    {
      sequence: 0,
      role: 'developer',
      content_type: 'instructions',
      content: 'a',
    },
    { sequence: 1, role: 'user', content_type: 'input', content: 'b' },
    { sequence: 2, role: 'assistant', content_type: 'input', content: 'c' },
    { sequence: 3, role: 'system', content_type: 'tools', content: {} },
  ]

  test('角色和内容类型可组合过滤且不改变原始顺序', () => {
    const visible = filterMessageAuditMessages(
      messages,
      ['developer'],
      ['tools']
    )

    assert.deepEqual(
      visible.map((message) => message.sequence),
      [1, 2]
    )
  })

  test('没有筛选时返回全部消息', () => {
    assert.deepEqual(filterMessageAuditMessages(messages, [], []), messages)
  })
})

describe('消息审计会话续接标识', () => {
  test('覆盖精确、前缀、压缩和新会话', () => {
    assert.equal(
      getMessageAuditSessionMatchLabelKey('exact'),
      'Exact history match'
    )
    assert.equal(
      getMessageAuditSessionMatchLabelKey('prefix'),
      'History continuation'
    )
    assert.equal(
      getMessageAuditSessionMatchLabelKey('compressed'),
      'Compressed continuation'
    )
    assert.equal(
      getMessageAuditSessionMatchLabelKey('unknown'),
      'New inferred session'
    )
  })
})

describe('消息审计会话分页占位', () => {
  const previousData = { items: [{ request_id: 'request-old' }], total: 1 }

  test('同一会话翻页时保留上一页数据', () => {
    assert.equal(
      keepMessageAuditSessionPlaceholder(
        previousData,
        'session-1',
        'session-1'
      ),
      previousData
    )
  })

  test('切换会话时不展示上一会话数据', () => {
    assert.equal(
      keepMessageAuditSessionPlaceholder(
        previousData,
        'session-1',
        'session-2'
      ),
      undefined
    )
    assert.equal(
      keepMessageAuditSessionPlaceholder(previousData, 'session-1', null),
      undefined
    )
  })
})
