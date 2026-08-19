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
import { test } from 'node:test'

import { renderAuditContent } from '../format'

test('渠道用户每日额度调整使用结构化审计模板渲染', () => {
  const rendered = renderAuditContent(
    {
      op: {
        action: 'channel.user_daily_quota_set',
        params: {
          channel_id: 1201,
          user_id: 1202,
          used_quota: 300,
        },
      },
    },
    (key, options = {}) =>
      key.replaceAll(/{{(\w+)}}/g, (_, name: string) =>
        String(options[name] ?? '')
      )
  )

  assert.equal(
    rendered,
    'Set daily used quota for user 1202 on channel 1201 to 300'
  )
})
