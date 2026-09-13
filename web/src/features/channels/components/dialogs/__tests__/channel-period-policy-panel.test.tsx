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
  ChannelBudgetPreview,
  ChannelBudgetUsageView,
  ChannelPeriodConfig,
  ChannelPeriodTarget,
  ChannelPeriodView,
} from '../../../period-types'

const testModule = 'bun:test'
const { test, afterEach } = (await import(testModule)) as {
  test: typeof import('node:test').test
  afterEach: typeof import('node:test').afterEach
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

Object.defineProperty(globalThis, 'IS_REACT_ACT_ENVIRONMENT', {
  configurable: true,
  value: true,
  writable: true,
})
const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider, notifyManager } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { api } = await import('@/lib/api')
const { parseQuotaFromDollars } = await import('@/lib/format')
const { ChannelPeriodPolicyPanel } =
  await import('../channel-period-policy-panel')
const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {},
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
})
notifyManager.setScheduler((callback) => callback())
type RequestInput = { method: string; url: string; data: unknown }
const transport = api as unknown as {
  get: (
    url: string,
    config?: { params?: Record<string, unknown> }
  ) => Promise<{ data: unknown }>
  request: (input: RequestInput) => Promise<{ data: unknown }>
}
const oldGet = transport.get,
  oldRequest = transport.request
let root: ReturnType<typeof createRoot> | null = null
let client: InstanceType<typeof QueryClient> | null = null

/** @returns 含一条已保存池子日预算的 v2 权威配置。 */
function view(revision = 3): ChannelPeriodView {
  return {
    revision,
    timezone: 'Asia/Shanghai',
    now: 1900000000,
    config: {
      schema_version: 2,
      default_on_exceed: { mode: 'reject', channel_id: 0, model: '' },
      schedules: [],
      budgets: [
        {
          id: 'b-pool',
          name: 'Pool daily',
          enabled: true,
          scope: 'pool',
          window: 'daily',
          schedule_id: '',
          models: [],
          limit: 500000,
          on_exceed: { mode: 'inherit', channel_id: 0, model: '' },
          created_at: 1,
        },
      ],
    },
  }
}
/** @param config 预览输入。 @returns 每行都生效的只读预览。 */
function previewOf(config: ChannelPeriodConfig): ChannelBudgetPreview {
  return {
    config,
    revision: 3,
    timezone: 'Asia/Shanghai',
    now: 1900000000,
    next_change_at: 0,
    rows: config.budgets.map((row) => ({
      budget_id: row.id,
      active: true,
      enforced: true,
      source: { kind: 'default' },
    })),
  }
}
const selfTarget: ChannelPeriodTarget = {
  id: 80,
  name: 'Self',
  models: ['gpt-6-astra', 'gpt-6-mini'],
  self: true,
}
/** @returns 清空当前 React 异步更新。 */
async function flush() {
  await act(async () => {
    await new Promise<void>((resolve) => setImmediate(resolve))
  })
}
/** @param predicate 可观察状态。 @returns 等待 DOM 完成查询或表单提交。 */
async function until(predicate: () => boolean) {
  for (let i = 0; i < 30; i++) {
    if (predicate()) return
    await flush()
  }
  assert.ok(predicate(), document.body.textContent ?? '')
}
/** @param label 按钮文本。 @returns 可操作按钮。 */
function button(label: string) {
  return [...document.querySelectorAll('button')].find(
    (item) => item.textContent?.trim() === label
  )
}
/** @param input 受控或注册的输入框。 @param value 新值。 @returns 触发 React onChange。 */
async function type(input: HTMLInputElement | null | undefined, value: string) {
  assert.ok(input)
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(
      domWindow.HTMLInputElement.prototype,
      'value'
    )?.set
    assert.ok(setter)
    setter.call(input, value)
    input.dispatchEvent(
      new domWindow.Event('input', { bubbles: true }) as unknown as Event
    )
  })
}
/** @returns 提交表单触发预览。 */
async function submit() {
  await act(async () => {
    document
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}
/** @param operate 操作权限。 @returns 实际挂载的面板。 */
async function render(operate = true, channelId = 80) {
  if (!root) {
    const host = document.createElement('div')
    document.body.append(host)
    root = createRoot(host)
  }
  client ??= new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { retry: false },
    },
  })
  const currentClient = client
  await act(async () => {
    root?.render(
      <QueryClientProvider client={currentClient}>
        <I18nextProvider i18n={i18n}>
          <ChannelPeriodPolicyPanel
            key={channelId}
            channelId={channelId}
            canOperate={operate}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
}
afterEach(async () => {
  if (root) await act(async () => root?.unmount())
  root = null
  client?.clear()
  client = null
  transport.get = oldGet
  transport.request = oldRequest
  document.body.innerHTML = ''
})

test('周期策略先预览再写入，版本冲突锁定草稿并允许重新加载', async () => {
  transport.get = async (url) => ({
    data: { success: true, data: url.endsWith('/targets') ? [] : view() },
  })
  const calls: RequestInput[] = []
  transport.request = async (input) => {
    calls.push(input)
    if (input.method === 'PUT') {
      throw { isAxiosError: true, response: { status: 409 } }
    }
    const body = input.data as { config: ChannelPeriodConfig }
    return { data: { success: true, data: previewOf(body.config) } }
  }
  await render()
  await until(() => Boolean(document.querySelector('form')))
  assert.equal(button('Confirm and save policy'), undefined)
  await submit()
  await until(() => Boolean(button('Confirm and save policy')))
  assert.equal(calls.length, 1)
  assert.equal(calls[0].method, 'POST')
  assert.equal(
    (calls[0].data as { expected_revision: number }).expected_revision,
    3
  )
  assert.match(document.body.textContent ?? '', /Pool daily/)
  await act(async () => button('Confirm and save policy')?.click())
  await until(() => Boolean(document.querySelector('[role="alert"]')))
  assert.match(
    document.querySelector('[role="alert"]')?.textContent ?? '',
    /policy has changed/
  )
  assert.equal(document.querySelector('fieldset')?.disabled, true)
  await act(async () => button('Reload')?.click())
  await until(() => !document.querySelector('fieldset')?.disabled)
  assert.equal(button('Confirm and save policy'), undefined)
})

test('新增按模型预算行后预览与保存都携带临时 id、模型与上限', async () => {
  transport.get = async (url) => ({
    data: {
      success: true,
      data: url.endsWith('/targets') ? [selfTarget] : view(),
    },
  })
  const calls: RequestInput[] = []
  transport.request = async (input) => {
    calls.push(input)
    const body = input.data as { config: ChannelPeriodConfig }
    return {
      data: {
        success: true,
        data:
          input.method === 'PUT'
            ? { ...view(4), config: body.config }
            : previewOf(body.config),
      },
    }
  }
  await render()
  await until(() => Boolean(button('Add budget')))
  await act(async () => button('Add budget')?.click())
  const editor = document.querySelector('[aria-label="Budget editor"]')
  assert.ok(editor)
  await type(
    editor.querySelector<HTMLInputElement>('input[name="budgets.1.name"]'),
    'Astra'
  )
  await type(
    editor.querySelector<HTMLInputElement>(
      'input[placeholder="Blank applies to all models"]'
    ),
    'gpt-6-astra, gpt-6-astra'
  )
  await type(
    editor.querySelector<HTMLInputElement>('input[type="number"]'),
    '2'
  )
  await submit()
  await until(() => calls.length === 1)
  const previewed = (calls[0].data as { config: ChannelPeriodConfig }).config
  assert.equal(previewed.budgets.length, 2)
  assert.deepEqual(previewed.budgets[1], {
    id: 'new-budget-1',
    name: 'Astra',
    enabled: true,
    scope: 'pool',
    window: 'daily',
    schedule_id: '',
    models: ['gpt-6-astra', 'gpt-6-astra'],
    limit: parseQuotaFromDollars(2),
    on_exceed: { mode: 'inherit', channel_id: 0, model: '' },
    created_at: 0,
  })
  await until(() => Boolean(button('Confirm and save policy')))
  await act(async () => button('Confirm and save policy')?.click())
  await until(() => calls.length === 2)
  assert.equal(calls[1].method, 'PUT')
  assert.deepEqual(calls[1].data, { expected_revision: 3, config: previewed })
})

test('已保存的预算行可按行查看池子用量并直接设置已用额度', async () => {
  const gets: { url: string; params?: Record<string, unknown> }[] = []
  transport.get = async (url, config) => {
    gets.push({ url, params: config?.params })
    if (url.includes('/usage')) {
      const usage: ChannelBudgetUsageView = {
        channel_id: 80,
        budget_id: 'b-pool',
        scope: 'pool',
        window_start: 1900000000,
        window_end: 1900086400,
        tracking_since: 0,
        storage_mode: 'redis',
        used_quota: 1000,
        page: 1,
        page_size: 20,
        total: 0,
        items: [],
      }
      return { data: { success: true, data: usage } }
    }
    return {
      data: { success: true, data: url.endsWith('/targets') ? [] : view() },
    }
  }
  const calls: RequestInput[] = []
  transport.request = async (input) => {
    calls.push({ method: input.method, url: input.url, data: input.data })
    return { data: { success: true, data: { used_quota: 0 } } }
  }
  await render()
  await until(() => Boolean(button('Usage')))
  await act(async () => button('Usage')?.click())
  await until(() => Boolean(button('Set usage')))
  const usageGet = gets.find((item) => item.url.includes('/usage'))
  assert.equal(usageGet?.url, '/api/channel/80/budgets/b-pool/usage')
  assert.equal(usageGet?.params?.scope, 'pool')
  assert.match(document.body.textContent ?? '', /Pool used/)
  await act(async () => button('Set usage')?.click())
  const panel = document.querySelector('[aria-label="Budget usage"]')
  assert.ok(panel)
  await type(panel.querySelector<HTMLInputElement>('input[type="number"]'), '0')
  await act(async () => button('Confirm')?.click())
  await until(() => calls.length === 1)
  assert.deepEqual(calls[0], {
    method: 'PUT',
    url: '/api/channel/80/budgets/b-pool/usage',
    data: { scope: 'pool', user_id: 0, used_quota: 0 },
  })
})

test('只读权限不能提交，跨渠道迟到的查询不能覆盖当前面板', async () => {
  let resolveOld: (value: { data: unknown }) => void = () => {}
  transport.get = async (url) => {
    if (url.endsWith('/targets')) return { data: { success: true, data: [] } }
    if (url.includes('/80/')) {
      return new Promise((resolve) => {
        resolveOld = resolve
      })
    }
    return { data: { success: true, data: { ...view(9), channel_id: 81 } } }
  }
  let writes = 0
  transport.request = async () => {
    writes++
    return { data: {} }
  }
  await render(true, 80)
  await render(false, 81)
  await until(() => Boolean(document.querySelector('form')))
  await act(async () => resolveOld({ data: { success: true, data: view() } }))
  assert.equal(document.querySelector('fieldset')?.disabled, true)
  await submit()
  await flush()
  assert.equal(writes, 0)
})

test('预算表展示日预算时段和用量进度，抽屉关闭保留草稿且默认动作位于表下方', async () => {
  const policy = view()
  policy.config.schedules = [
    {
      id: 'schedule-1',
      name: 'Holiday',
      enabled: true,
      kind: 'weekly',
      start_local: '',
      end_local: '',
      start_at: 0,
      end_at: 0,
      start_weekday: 0,
      end_weekday: 5,
      start_time: '00:00',
      end_time: '00:00',
      created_at: 1,
    },
  ]
  policy.config.budgets[0].schedule_id = 'schedule-1'
  transport.get = async (url) => {
    if (url.includes('/usage')) {
      return {
        data: {
          success: true,
          data: { used_quota: 100000, items: [], total: 0 },
        },
      }
    }
    return {
      data: { success: true, data: url.endsWith('/targets') ? [] : policy },
    }
  }
  await render()
  await until(() => Boolean(document.querySelector('[role="progressbar"]')))
  const table = document.querySelector('table')
  assert.ok(table)
  assert.match(table.textContent ?? '', /Holiday/)
  assert.equal(
    document
      .querySelector('[role="progressbar"]')
      ?.getAttribute('aria-valuenow'),
    '20'
  )
  const action = document.querySelector(
    '[aria-label="Default action when exceeded"]'
  )
  assert.ok(action)
  assert.ok(
    table.compareDocumentPosition(action) & Node.DOCUMENT_POSITION_FOLLOWING
  )
  await act(async () => button('Edit')?.click())
  await until(() => Boolean(document.querySelector('[role="dialog"]')))
  const editor = document.querySelector('[role="dialog"]')
  assert.ok(editor)
  assert.ok(editor.querySelector('select[name="budgets.0.schedule_id"]'))
  await type(
    editor.querySelector('input[name="budgets.0.name"]'),
    'Changed budget'
  )
  await act(async () => button('Done')?.click())
  await until(() => !document.querySelector('[role="dialog"]'))
  assert.match(table.textContent ?? '', /Changed budget/)
  await act(async () => button('Edit')?.click())
  await until(() => Boolean(document.querySelector('[role="dialog"]')))
  assert.equal(
    document.querySelector<HTMLInputElement>('input[name="budgets.0.name"]')
      ?.value,
    'Changed budget'
  )
  await act(async () => {
    document
      .querySelector<HTMLInputElement>('input[name="budgets.0.name"]')
      ?.focus()
    document.activeElement?.dispatchEvent(
      new domWindow.KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
      }) as unknown as Event
    )
  })
  await until(() => !document.querySelector('[role="dialog"]'))
  assert.match(table.textContent ?? '', /Changed budget/)
})

test('个人预算摘要读取最高用量用户的提额，读取失败可重试且新行不请求用量', async () => {
  const policy = view()
  policy.config.budgets[0].scope = 'user'
  const urls: string[] = []
  let failed = true
  transport.get = async (url) => {
    urls.push(url)
    if (url.includes('/usage')) {
      if (failed) throw { isAxiosError: true, response: { status: 503 } }
      return {
        data: {
          success: true,
          data: {
            total: 1,
            items: [
              {
                user_id: 7,
                username: 'alice',
                display_name: 'Alice',
                used_quota: 100000,
              },
            ],
          },
        },
      }
    }
    if (url.includes('/user-limit-status/')) {
      return {
        data: {
          success: true,
          data: {
            period_limits: {
              metrics: [{ budget_id: 'b-pool', limit: 1000000 }],
            },
          },
        },
      }
    }
    return {
      data: { success: true, data: url.endsWith('/targets') ? [] : policy },
    }
  }
  await render()
  await until(() => Boolean(button('Retry')))
  assert.equal(document.querySelector('[role="progressbar"]'), null)
  failed = false
  await act(async () => button('Retry')?.click())
  await until(() => Boolean(document.querySelector('[role="progressbar"]')))
  assert.match(
    document.querySelector('table')?.textContent ?? '',
    /Highest usage: Alice/
  )
  assert.equal(
    document
      .querySelector('[role="progressbar"]')
      ?.getAttribute('aria-valuenow'),
    '10'
  )
  assert.ok(urls.includes('/api/channel/80/user-limit-status/7'))
  await act(async () => button('Add budget')?.click())
  await act(async () => button('Done')?.click())
  assert.match(
    document.querySelector('table')?.textContent ?? '',
    /Save to view usage/
  )
  assert.ok(urls.every((url) => !url.includes('/budgets/new-')))
})

test('渠道模型累计默认关闭，预览保存保留开关和服务端来源，切换渠道恢复各自配置', async () => {
  const policy = view()
  policy.config.model_usage_tracking = {
    first_enabled_at: 1900000000,
    enabled_at: 1900000000,
    disabled_at: 1900000001,
  }
  policy.config.budgets[0].models = ['A']
  policy.config.budgets[0].counter_source = 'continuous_model'
  transport.get = async (url) => ({
    data: {
      success: true,
      data: url.endsWith('/targets')
        ? []
        : url.includes('/80/')
          ? policy
          : view(8),
    },
  })
  const calls: RequestInput[] = []
  transport.request = async (input) => {
    calls.push(input)
    const body = input.data as { config: ChannelPeriodConfig }
    return {
      data: {
        success: true,
        data:
          input.method === 'PUT'
            ? { ...view(4), config: body.config }
            : previewOf(body.config),
      },
    }
  }
  await render()
  await until(() => Boolean(document.querySelector('form')))
  const toggle = document.querySelector<HTMLButtonElement>(
    '[role="switch"][aria-label="Continuously track model usage"]'
  )
  assert.ok(toggle)
  assert.equal(toggle.getAttribute('aria-checked'), 'false')
  await act(async () => toggle.click())
  await submit()
  await until(() => Boolean(button('Confirm and save policy')))
  const sent = (calls[0].data as { config: ChannelPeriodConfig }).config
  assert.equal(sent.model_usage_tracking_enabled, true)
  assert.deepEqual(
    sent.model_usage_tracking,
    policy.config.model_usage_tracking
  )
  assert.equal(sent.budgets[0].counter_source, 'continuous_model')
  await act(async () => button('Confirm and save policy')?.click())
  await until(() => calls.length === 2)
  assert.deepEqual(calls[1].data, calls[0].data)
  await render(false, 81)
  await until(() => Boolean(document.querySelector('form')))
  const readonly = document.querySelector<HTMLButtonElement>(
    '[role="switch"][aria-label="Continuously track model usage"]'
  )
  assert.ok(readonly)
  assert.equal(readonly.getAttribute('aria-checked'), 'false')
  assert.equal(readonly.getAttribute('aria-disabled'), 'true')
})
