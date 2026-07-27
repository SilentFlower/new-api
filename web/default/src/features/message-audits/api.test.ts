import assert from 'node:assert/strict'
import { test } from 'node:test'

import { api } from '@/lib/api'

import { getCurrentMessageAuditCleanupTask } from './api'

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
