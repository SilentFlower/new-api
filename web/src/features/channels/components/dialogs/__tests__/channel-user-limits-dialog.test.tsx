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

import { Window } from 'happy-dom'

import type {
  Channel,
  ChannelUserConcurrencyItem,
  ChannelUserDailyQuotaItem,
  ChannelUserLimitPage,
} from '../../../types'

// 运行时使用 Bun 测试 API，避免 node:test 在全量执行时把后续文件误判为嵌套测试。
const bunTestModule = 'bun:test'
const { afterAll, afterEach, test } = (await import(bunTestModule)) as {
  afterAll: typeof import('node:test').after
  afterEach: typeof import('node:test').afterEach
  test: typeof import('node:test').test
}

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'HTMLLabelElement',
  'HTMLDivElement',
  'HTMLSpanElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'KeyboardEvent',
  'PointerEvent',
  'MouseEvent',
  'FocusEvent',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
  'IntersectionObserver',
  'DOMRect',
  'ShadowRoot',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
  'localStorage',
  'sessionStorage',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}
Object.defineProperty(globalThis, 'self', {
  configurable: true,
  value: domWindow,
})

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { notifyManager, QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { api } = await import('@/lib/api')
const { formatQuota } = await import('@/lib/format')
const { useAuthStore } = await import('@/stores/auth-store')
const { DEFAULT_CURRENCY_CONFIG, useSystemConfigStore } =
  await import('@/stores/system-config-store')
const { ChannelUserLimitsDialog } =
  await import('../channel-user-limits-dialog')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: {
    en: {
      translation: {
        'Adjust daily usage for {{user}} (ID: {{id}}).':
          'Adjust daily usage for {{user}} (ID: {{id}}).',
        'Adjusted used amount ({{unit}})': 'Adjusted used amount ({{unit}})',
        'Failed to load current concurrency':
          'Localized concurrency load failure',
        'Failed to load daily quota usage':
          'Localized daily quota load failure',
        'Page {{current}} of {{total}}': 'Page {{current}} of {{total}}',
        'Resets at {{time}}': 'Resets at {{time}}',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true
notifyManager.setNotifyFunction((callback) => {
  act(callback)
})

type ApiGet = (
  url: string,
  config?: { params?: { p?: number; page_size?: number } }
) => Promise<{ data: unknown }>
type ApiPut = (
  url: string,
  data?: unknown,
  config?: unknown
) => Promise<{ data: unknown }>
type MockableApi = {
  get: ApiGet
  put: ApiPut
}
type RenderedDialog = {
  host: HTMLDivElement
  queryClient: InstanceType<typeof QueryClient>
  root: ReturnType<typeof createRoot>
  render: (open: boolean) => Promise<void>
}

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPut = apiClient.put
const testChannel = { id: 77, name: 'Daily quota channel' } as Channel
let renderedDialog: RenderedDialog | null = null
const originalSetInterval = globalThis.setInterval
const originalClearInterval = globalThis.clearInterval

function dailyPage(
  items: ChannelUserDailyQuotaItem[],
  total = items.length,
  storageMode: 'redis' | 'memory' = 'redis'
): ChannelUserLimitPage<ChannelUserDailyQuotaItem> {
  return {
    channel_id: testChannel.id,
    limit: 500_000,
    storage_mode: storageMode,
    reset_at: 1_787_161_600,
    page: 1,
    page_size: 20,
    total,
    items,
  }
}

function concurrencyPage(
  items: ChannelUserConcurrencyItem[],
  total = items.length
): ChannelUserLimitPage<ChannelUserConcurrencyItem> {
  return {
    channel_id: testChannel.id,
    limit: 3,
    storage_mode: 'redis',
    page: 1,
    page_size: 20,
    total,
    items,
  }
}

function setOperator(canOperate: boolean) {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'operator',
    role: canOperate ? 100 : 10,
    permissions: canOperate
      ? undefined
      : {
          admin_permissions: {
            channel: { read: true, operate: false },
          },
        },
  })
}

async function flushAsyncWork() {
  await act(async () => {
    await new Promise<void>((resolve) => setImmediate(resolve))
  })
}

async function waitForCondition(
  condition: () => boolean,
  failureMessage: string
) {
  for (let attempt = 0; attempt < 30; attempt++) {
    if (condition()) return
    await flushAsyncWork()
  }
  throw new Error(`${failureMessage}: ${document.body.textContent}`)
}

function findButton(text: string): HTMLButtonElement {
  const button = [
    ...document.querySelectorAll<HTMLButtonElement>('button'),
  ].find((candidate) => candidate.textContent?.trim() === text)
  assert.ok(button, `Expected button "${text}"`)
  return button
}

async function changeInput(input: HTMLInputElement, value: string) {
  await act(async () => {
    const valueSetter = Object.getOwnPropertyDescriptor(
      domWindow.HTMLInputElement.prototype,
      'value'
    )?.set
    assert.ok(valueSetter)
    valueSetter.call(input, value)
    input.dispatchEvent(
      new domWindow.Event('input', { bubbles: true }) as unknown as Event
    )
  })
}

async function renderChannelUserLimitsDialog(open = true) {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Number.POSITIVE_INFINITY },
      mutations: { retry: false },
    },
  })
  const render = async (nextOpen: boolean) => {
    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <I18nextProvider i18n={i18n}>
            <ChannelUserLimitsDialog
              open={nextOpen}
              onOpenChange={() => undefined}
              channel={testChannel}
            />
          </I18nextProvider>
        </QueryClientProvider>
      )
      await new Promise<void>((resolve) => setImmediate(resolve))
    })
  }
  renderedDialog = { host, queryClient, root, render }
  await render(open)
  return renderedDialog
}

afterEach(async () => {
  apiClient.get = originalGet
  apiClient.put = originalPut
  globalThis.setInterval = originalSetInterval
  globalThis.clearInterval = originalClearInterval
  if (renderedDialog) {
    await act(async () => renderedDialog?.root.unmount())
    renderedDialog.queryClient.clear()
    renderedDialog.host.remove()
    renderedDialog = null
  }
  useAuthStore.getState().auth.reset()
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG },
  })
  document.body.replaceChildren()
})

afterAll(() => {
  domWindow.close()
})

test('调整确认展示用户和调整前后金额，并提交目标额度', async () => {
  setOperator(true)
  const putCalls: Array<{ url: string; data: unknown }> = []
  apiClient.get = async () => ({
    data: {
      success: true,
      data: dailyPage(
        [
          {
            user_id: 81,
            username: 'alice',
            display_name: 'Alice',
            used_quota: 1000,
            limit: 500_000,
            remaining_quota: 499_000,
          },
        ],
        1,
        'memory'
      ),
    },
  })
  apiClient.put = async (url, data) => {
    putCalls.push({ url, data })
    return { data: { success: true } }
  }

  await renderChannelUserLimitsDialog()
  await waitForCondition(
    () => document.body.textContent?.includes('Set usage') === true,
    '每日额度列表未加载'
  )
  assert.match(
    document.body.textContent ?? '',
    /Usage data is available for this instance only\./
  )

  await act(async () => findButton('Set usage').click())
  await waitForCondition(
    () =>
      document.body.textContent?.includes(
        'Adjust daily usage for Alice (ID: 81).'
      ) === true,
    '调整确认弹窗未打开'
  )
  assert.match(document.body.textContent ?? '', /Before adjustment/)
  assert.match(document.body.textContent ?? '', /After adjustment/)
  assert.ok(document.body.textContent?.includes(formatQuota(1000)))

  const input = document.querySelector<HTMLInputElement>(
    '#channel-user-daily-quota-amount'
  )
  assert.ok(input)
  await changeInput(input, '0')
  assert.ok(document.body.textContent?.includes(formatQuota(0)))

  await act(async () => findButton('Confirm').click())
  await waitForCondition(() => putCalls.length === 1, '调整请求未提交')
  assert.deepEqual(putCalls[0], {
    url: '/api/channel/77/user-daily-quota/81',
    data: { used_quota: 0 },
  })
})

test('个人调整在人民币显示下回填无浮点尾数并保持额度往返', async () => {
  setOperator(true)
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CNY',
      usdExchangeRate: 7.2,
    },
  })
  const putCalls: Array<{ url: string; data: unknown }> = []
  apiClient.get = async () => ({
    data: {
      success: true,
      data: dailyPage([
        {
          user_id: 85,
          username: 'currency-user',
          display_name: 'Currency User',
          used_quota: 50_000,
          limit: 500_000,
          remaining_quota: 450_000,
        },
      ]),
    },
  })
  apiClient.put = async (url, data) => {
    putCalls.push({ url, data })
    return { data: { success: true } }
  }

  await renderChannelUserLimitsDialog()
  await waitForCondition(
    () => document.body.textContent?.includes('Set usage') === true,
    '每日额度列表未加载'
  )
  await act(async () => findButton('Set usage').click())

  const input = document.querySelector<HTMLInputElement>(
    '#channel-user-daily-quota-amount'
  )
  assert.ok(input)
  assert.equal(input.value, '0.72')

  await act(async () => findButton('Confirm').click())
  await waitForCondition(() => putCalls.length === 1, '调整请求未提交')
  assert.deepEqual(putCalls[0], {
    url: '/api/channel/77/user-daily-quota/85',
    data: { used_quota: 50_000 },
  })
})

test('缺少渠道运行权限时禁用调整并提供可聚焦说明', async () => {
  setOperator(false)
  apiClient.get = async () => ({
    data: {
      success: true,
      data: dailyPage([
        {
          user_id: 82,
          username: 'bob',
          display_name: 'Bob',
          used_quota: 2000,
          limit: 500_000,
          remaining_quota: 498_000,
        },
      ]),
    },
  })

  await renderChannelUserLimitsDialog()
  await waitForCondition(
    () => document.body.textContent?.includes('Set usage') === true,
    '每日额度列表未加载'
  )

  const button = findButton('Set usage')
  assert.equal(button.disabled, true)
  const permissionTrigger = button.parentElement
  assert.ok(permissionTrigger)
  assert.equal(
    permissionTrigger.getAttribute('aria-label'),
    'No permission to perform this action'
  )
  assert.equal(permissionTrigger.tabIndex, 0)
})

test('刷新后总数收缩时从空的末页自动回到有效页', async () => {
  setOperator(true)
  let secondPageRequests = 0
  apiClient.get = async (_url, config) => {
    const page = config?.params?.p ?? 1
    if (page === 2) {
      secondPageRequests++
      return {
        data: {
          success: true,
          data:
            secondPageRequests === 1
              ? dailyPage(
                  [
                    {
                      user_id: 99,
                      username: 'last-page',
                      display_name: 'Last Page',
                      used_quota: 100,
                      limit: 500_000,
                      remaining_quota: 499_900,
                    },
                  ],
                  21
                )
              : dailyPage([], 20),
        },
      }
    }
    return {
      data: {
        success: true,
        data: dailyPage(
          [
            {
              user_id: 83,
              username: 'first-page',
              display_name: 'First Page',
              used_quota: 100,
              limit: 500_000,
              remaining_quota: 499_900,
            },
          ],
          21
        ),
      },
    }
  }

  await renderChannelUserLimitsDialog()
  await waitForCondition(
    () => document.body.textContent?.includes('Page 1 of 2') === true,
    '第一页分页状态未加载'
  )
  await act(async () => findButton('Next').click())
  await waitForCondition(
    () => document.body.textContent?.includes('Last Page') === true,
    '第二页未加载'
  )

  const refreshButton = document.querySelector<HTMLButtonElement>(
    'button[aria-label="Refresh"]'
  )
  assert.ok(refreshButton)
  await act(async () => refreshButton.click())
  await waitForCondition(
    () =>
      document.body.textContent?.includes('First Page') === true &&
      document.body.textContent?.includes('Last Page') === false,
    '总数收缩后未回到第一页'
  )
  assert.ok(secondPageRequests >= 2)
})

test('当前并发仅在对应页签打开时轮询，Dialog 关闭后停止', async () => {
  setOperator(true)
  let pollingCallback: (() => void) | null = null
  let pollingCleared = false
  const pollingTimerID = 77_001
  globalThis.setInterval = ((handler: TimerHandler, timeout?: number) => {
    if (timeout === 5000 && typeof handler === 'function') {
      pollingCallback = () => handler()
      return pollingTimerID
    }
    return originalSetInterval(handler, timeout)
  }) as typeof globalThis.setInterval
  globalThis.clearInterval = ((timerID?: number) => {
    if (timerID === pollingTimerID) {
      pollingCleared = true
      pollingCallback = null
      return
    }
    originalClearInterval(timerID)
  }) as typeof globalThis.clearInterval
  let concurrencyRequests = 0
  apiClient.get = async (url) => {
    if (url.endsWith('/user-concurrency')) {
      concurrencyRequests++
      return {
        data: {
          success: true,
          data: concurrencyPage([
            {
              user_id: 84,
              username: 'active-user',
              display_name: 'Active User',
              current_concurrency: 2,
              limit: 3,
            },
          ]),
        },
      }
    }
    return { data: { success: true, data: dailyPage([]) } }
  }

  const rendered = await renderChannelUserLimitsDialog()
  await flushAsyncWork()
  assert.equal(concurrencyRequests, 0)

  await act(async () => findButton('Current concurrency').click())
  await waitForCondition(
    () => concurrencyRequests === 1,
    '切换到并发页签后未发起查询'
  )
  assert.ok(pollingCallback)
  await act(async () => {
    pollingCallback?.()
  })
  await flushAsyncWork()
  assert.equal(concurrencyRequests, 2)

  await rendered.render(false)
  const requestsAfterClose = concurrencyRequests
  await flushAsyncWork()
  assert.equal(pollingCleared, true)
  assert.equal(pollingCallback, null)
  assert.equal(concurrencyRequests, requestsAfterClose)
})

test('查询失败时使用国际化后的兜底文案', async () => {
  setOperator(true)
  apiClient.get = async (url) => {
    if (url.endsWith('/user-concurrency')) {
      return { data: { success: false } }
    }
    return { data: { success: false } }
  }

  await renderChannelUserLimitsDialog()
  await waitForCondition(
    () =>
      document.body.textContent?.includes(
        'Localized daily quota load failure'
      ) === true,
    '每日额度查询失败文案未经过国际化'
  )

  await act(async () => findButton('Current concurrency').click())
  await waitForCondition(
    () =>
      document.body.textContent?.includes(
        'Localized concurrency load failure'
      ) === true,
    '并发查询失败文案未经过国际化'
  )
})
