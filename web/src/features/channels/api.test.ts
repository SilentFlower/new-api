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

import {
  getChannelModelOptions,
  getChannelUserConcurrency,
  getChannelUserDailyQuota,
  getChannelUserLimitStatus,
  getChannelUserWeeklyQuota,
  setChannelUserLimitOverride,
  setChannelUserDailyQuota,
  setChannelUserWeeklyQuota,
} from './api'

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

test('渠道用户限制接口使用指定渠道、分页和目标额度载荷', async () => {
  const originalGet = api.get
  const originalPut = api.put
  const getRequests: Array<{ url: string; params: unknown }> = []
  let putRequest: { url: string; data: unknown } | null = null

  api.get = (async (url, config) => {
    getRequests.push({ url, params: config?.params })
    return {
      data: {
        success: true,
        data: {
          channel_id: 12,
          limit: 100,
          storage_mode: 'memory',
          page: 2,
          page_size: 20,
          total: 0,
          items: [],
        },
      },
    }
  }) as typeof api.get
  api.put = (async (url, data) => {
    putRequest = { url, data }
    return { data: { success: true } }
  }) as typeof api.put

  try {
    await getChannelUserDailyQuota(12, { p: 2, page_size: 20 })
    await getChannelUserWeeklyQuota(12, { p: 4, page_size: 20 })
    await getChannelUserConcurrency(12, { p: 3, page_size: 20 })
    await setChannelUserDailyQuota(12, 34, 500000)
    await setChannelUserWeeklyQuota(12, 34, 800000)

    assert.deepEqual(getRequests, [
      {
        url: '/api/channel/12/user-daily-quota',
        params: { p: 2, page_size: 20 },
      },
      {
        url: '/api/channel/12/user-weekly-quota',
        params: { p: 4, page_size: 20 },
      },
      {
        url: '/api/channel/12/user-concurrency',
        params: { p: 3, page_size: 20 },
      },
    ])
    assert.deepEqual(putRequest, {
      url: '/api/channel/12/user-weekly-quota/34',
      data: { used_quota: 800000 },
    })
  } finally {
    api.get = originalGet
    api.put = originalPut
  }
})

test('个人覆盖接口只提交白名单字段并读取指定用户状态', async () => {
  const originalGet = api.get
  const originalPut = api.put
  const calls: Array<{ method: string; url: string; data?: unknown }> = []
  const status = {
    channel_id: 12,
    user: { id: 34, username: 'alice', display_name: 'Alice' },
    concurrency: {
      base_limit: 2,
      effective_limit: 4,
      current: 0,
      remaining: 4,
      storage_mode: 'memory' as const,
    },
    daily_quota: {
      base_limit: 100,
      effective_limit: 200,
      current: 0,
      remaining: 200,
      storage_mode: 'memory' as const,
    },
    weekly_quota: {
      base_limit: 500,
      effective_limit: 900,
      current: 0,
      remaining: 900,
      storage_mode: 'memory' as const,
    },
    override_active: true,
    override_expires_at: 0,
  }
  api.get = (async (url) => {
    calls.push({ method: 'GET', url })
    return { data: { success: true, data: status } }
  }) as typeof api.get
  api.put = (async (url, data) => {
    calls.push({ method: 'PUT', url, data })
    return { data: { success: true, data: status } }
  }) as typeof api.put

  try {
    await getChannelUserLimitStatus(12, 34)
    await setChannelUserLimitOverride(12, 34, {
      user_concurrency_limit: 4,
      user_daily_quota_limit: 200,
      user_weekly_quota_limit: 900,
      expires_at: 0,
    })
    assert.deepEqual(calls, [
      {
        method: 'GET',
        url: '/api/channel/12/user-limit-status/34',
      },
      {
        method: 'PUT',
        url: '/api/channel/12/user-limit-overrides/34',
        data: {
          user_concurrency_limit: 4,
          user_daily_quota_limit: 200,
          user_weekly_quota_limit: 900,
          expires_at: 0,
        },
      },
    ])
  } finally {
    api.get = originalGet
    api.put = originalPut
  }
})
