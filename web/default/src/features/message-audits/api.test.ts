import assert from 'node:assert/strict'
import { test } from 'node:test'

import { api } from '@/lib/api'

import {
  getCurrentMessageAuditCleanupTask,
  getMessageAuditSessionRequests,
  startMessageAuditReview,
} from './api'

test('当前清理任务查询复用系统任务 current 接口', async () => {
  const originalGet = api.get
  let requestedUrl = ''
  let requestedType = ''
  api.get = (async (url, config) => {
    requestedUrl = url
    requestedType = String(config?.params?.type ?? '')
    return {
      data: {
        success: true,
        message: '',
        data: {
          id: 1,
          task_id: 'cleanup-1',
          type: 'message_audit_cleanup',
          status: 'running',
          created_at: 1,
          updated_at: 1,
        },
      },
    }
  }) as typeof api.get

  try {
    const task = await getCurrentMessageAuditCleanupTask()
    assert.equal(requestedUrl, '/api/system-task/current')
    assert.equal(requestedType, 'message_audit_cleanup')
    assert.equal(task?.task_id, 'cleanup-1')
    assert.equal(task?.status, 'running')
  } finally {
    api.get = originalGet
  }
})

test('推断会话查询通过 audit_session_id 获取单次请求', async () => {
  const originalGet = api.get
  let requestedSessionId = ''
  let requestedPage = 0
  api.get = (async (_url, config) => {
    requestedSessionId = String(config?.params?.audit_session_id ?? '')
    requestedPage = Number(config?.params?.p ?? 0)
    return {
      data: {
        success: true,
        message: '',
        data: { page: 2, page_size: 20, total: 0, items: [] },
      },
    }
  }) as typeof api.get

  try {
    const data = await getMessageAuditSessionRequests('audsess_test', 2, 20)
    assert.equal(requestedSessionId, 'audsess_test')
    assert.equal(requestedPage, 2)
    assert.equal(data.total, 0)
  } finally {
    api.get = originalGet
  }
})

test('发起 AI 审核时只通过路径传递会话且不发送请求正文', async () => {
  const originalPost = api.post
  let callCount = 0
  api.post = (async (...args: Parameters<typeof api.post>) => {
    callCount++
    assert.equal(args[0], '/api/message-audit/session/session%2F1/review')
    assert.equal(args[1], undefined)
    return {
      data: {
        success: true,
        message: '',
        data: {
          created: true,
          task: {
            id: 1,
            task_id: 'review-task-1',
            type: 'message_audit_review',
            status: 'pending',
            payload: {
              audit_session_id: 'session/1',
              target_request_id: 'request-1',
              source_request_ids: ['request-1'],
              user_id: 1,
              operator_id: 2,
              config: { channel_id: 3, model: 'review-model' },
            },
            state: null,
            result: null,
            error: '',
            locked_by: '',
            created_at: 1,
            updated_at: 1,
          },
        },
      },
    } as never
  }) as typeof api.post

  try {
    const result = await startMessageAuditReview('session/1')

    assert.equal(result.created, true)
    assert.equal(result.task.task_id, 'review-task-1')
    assert.equal(callCount, 1)
  } finally {
    api.post = originalPost
  }
})
