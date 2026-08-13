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
