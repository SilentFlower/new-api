import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { unwrapMessageAuditResponse } from './message-audit-api'

describe('消息审计 API 响应校验', () => {
  test('成功时返回数据并允许显式 null', () => {
    assert.deepEqual(
      unwrapMessageAuditResponse({
        success: true,
        message: '',
        data: { total: 2 },
      }),
      { total: 2 }
    )
    assert.equal(
      unwrapMessageAuditResponse({ success: true, message: '', data: null }),
      null
    )
  })

  test('业务失败或缺少数据时抛出错误', () => {
    assert.throws(
      () =>
        unwrapMessageAuditResponse({
          success: false,
          message: 'database unavailable',
        }),
      /database unavailable/
    )
    assert.throws(() =>
      unwrapMessageAuditResponse({ success: true, message: '' })
    )
  })
})
