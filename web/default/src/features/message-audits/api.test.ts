import assert from 'node:assert/strict'
import { test } from 'node:test'

import { api } from '@/lib/api'

import {
  getCurrentMessageAuditCleanupTask,
  getMessageAuditSessionRequests,
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
