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

import { api } from '@/lib/api'

import { getChannelModelOptions } from './api'

test('渠道模型选项使用只读精简接口', async () => {
  const originalGet = api.get
  let requestedUrl = ''
  api.get = (async (url) => {
    requestedUrl = url
    return {
      data: {
        success: true,
        message: '',
        data: [{ id: 12, name: 'vision', models: ['vision-model'] }],
      },
    }
  }) as typeof api.get

  try {
    const options = await getChannelModelOptions()
    assert.equal(requestedUrl, '/api/channel/model-options')
    assert.deepEqual(options, [
      { id: 12, name: 'vision', models: ['vision-model'] },
    ])
  } finally {
    api.get = originalGet
  }
})
