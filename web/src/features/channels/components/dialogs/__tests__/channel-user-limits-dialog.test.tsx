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
  ChannelBudgetUsageView,
  ChannelPeriodView,
} from '../../../period-types'
import type {
  Channel,
  ChannelUserConcurrencyItem,
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
  'HTMLSelectElement',
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
        'Failed to load current concurrency':
          'Localized concurrency load failure',
        'Page {{current}} of {{total}}': 'Page {{current}} of {{total}}',
        'Period policy is unavailable. Reload and try again.':
          'Localized period policy load failure',
        'Set current usage for {{target}}. This only changes the counter, not billing.':
          'Set current usage for {{target}}.',
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
  config?: { params?: { p?: number; page_size?: number; scope?: string } }
) => Promise<{ data: unknown }>
type ApiPut = (
  url: string,
  data?: unknown,
  config?: unknown
) => Promise<{ data: unknown }>
type ApiRequest = (input: {
  method: string
  url: string
  data?: unknown
}) => Promise<{ data: unknown }>
type MockableApi = {
  get: ApiGet
  put: ApiPut
  request: ApiRequest
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
const originalRequest = apiClient.request
const testChannel = { id: 77, name: 'Budget channel' } as Channel
let renderedDialog: RenderedDialog | null = null
const originalSetInterval = globalThis.setInterval
const originalClearInterval = globalThis.clearInterval

/** @returns 含个人日预算行与池子行的 v2 策略。 */
function policyView(): ChannelPeriodView {
  return {
    revision: 2,
    timezone: 'Asia/Shanghai',
    now: 1_787_100_000,
    config: {
      schema_version: 2,
      default_on_exceed: { mode: 'reject', channel_id: 0, model: '' },
      schedules: [],
      budgets: [
        {
          id: 'b-user',
          name: 'User daily',
          enabled: true,
          scope: 'user',
          window: 'daily',
          schedule_id: '',
          models: [],
          limit: 500_000,
          on_exceed: { mode: 'inherit', channel_id: 0, model: '' },
          created_at: 1,
        },
        {
          id: 'b-pool',
          name: 'Pool daily',
          enabled: true,
          scope: 'pool',
          window: 'daily',
          schedule_id: '',
          models: [],
          limit: 900_000,
          on_exceed: { mode: 'inherit', channel_id: 0, model: '' },
          created_at: 1,
        },
      ],
    },
  }
}

function usagePage(
  items: ChannelBudgetUsageView['items'],
  total = items.length,
  storageMode: 'redis' | 'memory' = 'redis'
): ChannelBudgetUsageView {
  return {
    channel_id: testChannel.id,
    budget_id: 'b-user',
    scope: 'user',
    window_start: 1_787_075_200,
    window_end: 1_787_161_600,
    tracking_since: 0,
    storage_mode: storageMode,
    used_quota: items.reduce((sum, item) => sum + item.used_quota, 0),
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

function overridesPage(total: number, items: unknown[]) {
  return { channel_id: testChannel.id, page: 1, page_size: 20, total, items }
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

/** @param budgetId 预算行 id。 @returns 在用量页签的预算选择器里选中该行。 */
async function selectBudget(budgetId: string) {
  await waitForCondition(
    () => document.querySelector('select[data-slot="native-select"]') !== null,
    '预算选择器未加载'
  )
  const select = document.querySelector<HTMLSelectElement>(
    'select[data-slot="native-select"]'
  )
  assert.ok(select)
  await act(async () => {
    select.value = budgetId
    select.dispatchEvent(
      new domWindow.Event('change', { bubbles: true }) as unknown as Event
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
  apiClient.request = originalRequest
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

test('按行用量页签选中预算后展示用户用量，并按行提交目标已用额度', async () => {
  setOperator(true)
  const requests: Array<{ method: string; url: string; data?: unknown }> = []
  apiClient.get = async (url) => {
    if (url.includes('/usage')) {
      return {
        data: {
          success: true,
          data: usagePage(
            [
              {
                user_id: 81,
                username: 'alice',
                display_name: 'Alice',
                used_quota: 1000,
              },
            ],
            1,
            'memory'
          ),
        },
      }
    }
    return { data: { success: true, data: policyView() } }
  }
  apiClient.request = async (input) => {
    requests.push({ method: input.method, url: input.url, data: input.data })
    return { data: { success: true, data: { used_quota: 0 } } }
  }

  await renderChannelUserLimitsDialog()
  await selectBudget('b-user')
  await waitForCondition(
    () => document.body.textContent?.includes('Set usage') === true,
    '按行用量列表未加载'
  )
  assert.ok(document.body.textContent?.includes(formatQuota(1000)))

  await act(async () => findButton('Set usage').click())
  await waitForCondition(
    () =>
      document.body.textContent?.includes('Set current usage for Alice.') ===
      true,
    '调整表单未打开'
  )
  const panel = document.querySelector('[aria-label="Budget usage"]')
  assert.ok(panel)
  const input = panel.querySelector<HTMLInputElement>('input[type="number"]')
  assert.ok(input)
  await changeInput(input, '0')
  await act(async () => findButton('Confirm').click())
  await waitForCondition(() => requests.length === 1, '调整请求未提交')
  assert.deepEqual(requests[0], {
    method: 'PUT',
    url: '/api/channel/77/budgets/b-user/usage',
    data: { scope: 'user', user_id: 81, used_quota: 0 },
  })
})

test('缺少渠道运行权限时禁用按行用量调整', async () => {
  setOperator(false)
  apiClient.get = async (url) => {
    if (url.includes('/usage')) {
      return {
        data: {
          success: true,
          data: usagePage([
            {
              user_id: 82,
              username: 'bob',
              display_name: 'Bob',
              used_quota: 2000,
            },
          ]),
        },
      }
    }
    return { data: { success: true, data: policyView() } }
  }

  await renderChannelUserLimitsDialog()
  await selectBudget('b-user')
  await waitForCondition(
    () => document.body.textContent?.includes('Set usage') === true,
    '按行用量列表未加载'
  )
  assert.equal(findButton('Set usage').disabled, true)
})

test('刷新后总数收缩时从空的末页自动回到有效页', async () => {
  setOperator(true)
  let secondPageRequests = 0
  const overrideItem = (id: number, name: string) => ({
    user: { id, username: name.toLowerCase(), display_name: name },
    user_concurrency_limit: 5,
    effective_concurrency_limit: 5,
    expires_at: 0,
  })
  apiClient.get = async (url, config) => {
    if (url.includes('/budget-user-overrides')) {
      return { data: { success: true, data: overridesPage(0, []) } }
    }
    if (url.includes('/user-limit-overrides')) {
      const page = config?.params?.p ?? 1
      if (page === 2) {
        secondPageRequests++
        return {
          data: {
            success: true,
            data:
              secondPageRequests === 1
                ? overridesPage(21, [overrideItem(99, 'Last Page')])
                : overridesPage(20, []),
          },
        }
      }
      return {
        data: {
          success: true,
          data: overridesPage(21, [overrideItem(83, 'First Page')]),
        },
      }
    }
    return { data: { success: true, data: policyView() } }
  }

  await renderChannelUserLimitsDialog()
  await act(async () => findButton('Personal overrides').click())
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
    return { data: { success: true, data: policyView() } }
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
  apiClient.get = async () => ({ data: { success: false } })

  await renderChannelUserLimitsDialog()
  await waitForCondition(
    () =>
      document.body.textContent?.includes(
        'Localized period policy load failure'
      ) === true,
    '预算策略查询失败文案未经过国际化'
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

test('无请求记录的用户可通过搜索提前配置并发覆盖，行级提额列表按行展示', async () => {
  setOperator(true)
  const putCalls: Array<{ url: string; data: unknown }> = []
  apiClient.get = async (url) => {
    if (url.includes('/budget-user-overrides')) {
      return {
        data: {
          success: true,
          data: overridesPage(1, [
            {
              user: { id: 92, username: 'raised', display_name: 'Raised' },
              budget_id: 'b-user',
              budget_name: 'User daily',
              base_limit: 500_000,
              limit: 800_000,
              expires_at: 0,
            },
          ]),
        },
      }
    }
    if (url.includes('/user-limit-overrides')) {
      return { data: { success: true, data: overridesPage(0, []) } }
    }
    if (url.includes('/user-limit-users')) {
      return {
        data: {
          success: true,
          data: {
            page: 1,
            page_size: 10,
            total: 1,
            items: [
              {
                id: 91,
                username: 'future-user',
                display_name: 'Future User',
              },
            ],
          },
        },
      }
    }
    if (url.includes('/user-limit-status/91')) {
      return {
        data: {
          success: true,
          data: {
            channel_id: 77,
            user: {
              id: 91,
              username: 'future-user',
              display_name: 'Future User',
            },
            concurrency: {
              base_limit: 3,
              effective_limit: 3,
              current: 0,
              remaining: 3,
              storage_mode: 'memory',
            },
            period_limits: {
              schema_version: 2,
              revision: 2,
              timezone: 'Asia/Shanghai',
              storage_mode: 'memory',
              next_change_at: 0,
              metrics: [],
              fallback_enabled: false,
              blocked: false,
            },
            override_active: false,
            override_expires_at: 0,
          },
        },
      }
    }
    return { data: { success: true, data: policyView() } }
  }
  apiClient.put = async (url, data) => {
    putCalls.push({ url, data })
    return { data: { success: true, data: {} } }
  }

  await renderChannelUserLimitsDialog()
  await act(async () => findButton('Personal overrides').click())
  await waitForCondition(
    () => document.body.textContent?.includes('Raised') === true,
    '行级提额列表未加载'
  )
  assert.match(document.body.textContent ?? '', /User daily/)
  assert.ok(document.body.textContent?.includes(formatQuota(800_000)))
  const searchInput = document.querySelector<HTMLInputElement>(
    'input[aria-label="Search users"]'
  )
  assert.ok(searchInput)
  await changeInput(searchInput, 'future-user')
  await act(async () => findButton('Search').click())
  await waitForCondition(
    () => document.body.textContent?.includes('Future User') === true,
    '用户搜索结果未出现'
  )
  await act(async () => findButton('Temporarily increase').click())
  await waitForCondition(
    () => document.querySelector('#personal-concurrency') !== null,
    '个人覆盖编辑器未打开'
  )
  assert.equal(document.querySelector('#personal-daily'), null)
  const concurrencyInput = document.querySelector<HTMLInputElement>(
    '#personal-concurrency'
  )
  assert.ok(concurrencyInput)
  await changeInput(concurrencyInput, '5')
  await act(async () => findButton('Save override').click())
  await waitForCondition(() => putCalls.length === 1, '个人覆盖请求未提交')
  assert.deepEqual(putCalls[0], {
    url: '/api/channel/77/user-limit-overrides/91',
    data: { user_concurrency_limit: 5, expires_at: 0 },
  })
})
