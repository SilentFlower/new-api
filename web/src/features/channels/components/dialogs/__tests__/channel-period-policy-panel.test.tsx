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
  ChannelBudgetUsageSummaryView,
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
const { formatTimestampToDate, parseQuotaFromDollars } =
  await import('@/lib/format')
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
/** @param items 各行用量。 @returns 聚合用量视图。 */
function summaryOf(
  items: ChannelBudgetUsageSummaryView['items']
): ChannelBudgetUsageSummaryView {
  return {
    channel_id: 80,
    revision: 3,
    storage_mode: 'redis',
    now: 1900000000,
    items,
  }
}
/**
 * 默认 GET 分派：策略、候选、聚合用量；request 默认把所有 POST 当作预览回显。
 * @param policy 权威策略。 @param summary 聚合用量。 @param targets 候选。 @returns 无。
 */
function mockReads(
  policy: ChannelPeriodView,
  summary: ChannelBudgetUsageSummaryView['items'] = [],
  targets: ChannelPeriodTarget[] = []
) {
  transport.get = async (url) => {
    if (url.endsWith('/targets')) {
      return { data: { success: true, data: targets } }
    }
    if (url.endsWith('/budgets/usage-summary')) {
      return { data: { success: true, data: summaryOf(summary) } }
    }
    return { data: { success: true, data: policy } }
  }
}
/** @returns 打开的对话框列表（Sheet 与 Dialog 都是 role=dialog）。 */
function dialogs() {
  return [...document.querySelectorAll('[role="dialog"]')]
}
/** @param label 按钮文本。 @returns 当前打开的确认框内的按钮。 */
function alertButton(label: string) {
  return [...document.querySelectorAll('[role="alertdialog"] button')].find(
    (item) => item.textContent?.trim() === label
  ) as HTMLButtonElement | undefined
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
/** @param select 受控或注册的下拉框。 @param value 新值。 @returns 触发 React onChange。 */
async function choose(
  select: HTMLSelectElement | null | undefined,
  value: string
) {
  assert.ok(select)
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(
      domWindow.HTMLSelectElement.prototype,
      'value'
    )?.set
    assert.ok(setter)
    setter.call(select, value)
    select.dispatchEvent(
      new domWindow.Event('change', { bubbles: true }) as unknown as Event
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

test('修改后出现保存栏，预览 diff 后写入；版本冲突锁定草稿并允许重新加载', async () => {
  mockReads(view())
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
  assert.equal(button('Preview and save'), undefined)
  assert.equal(button('Confirm and save policy'), undefined)
  const toggle = document.querySelector<HTMLButtonElement>(
    '[role="switch"][aria-label="Enable Pool daily"]'
  )
  assert.ok(toggle)
  await act(async () => toggle.click())
  await until(() => Boolean(button('Preview and save')))
  assert.match(
    document.querySelector('[aria-label="Unsaved changes"]')?.textContent ?? '',
    /1 unsaved changes/
  )
  await act(async () => button('Preview and save')?.click())
  await until(() => Boolean(button('Confirm and save policy')))
  const manual = calls.findLast((item) => item.method === 'POST')
  assert.ok(manual)
  assert.equal(
    (manual.data as { expected_revision: number }).expected_revision,
    3
  )
  const preview = document.querySelector('[aria-label="Policy preview"]')
  assert.ok(preview)
  assert.match(preview.textContent ?? '', /Pool daily/)
  assert.match(preview.textContent ?? '', /Enabled/)
  assert.match(preview.textContent ?? '', /Disabled/)
  await act(async () => button('Confirm and save policy')?.click())
  await until(() => Boolean(document.querySelector('form [role="alert"]')))
  assert.match(
    document.querySelector('form [role="alert"]')?.textContent ?? '',
    /policy has changed/
  )
  assert.equal(
    document.querySelector<HTMLFieldSetElement>('form fieldset')?.disabled,
    true
  )
  await act(async () => button('Reload')?.click())
  await until(() => Boolean(alertButton('Discard')))
  await act(async () => alertButton('Discard')?.click())
  await until(
    () =>
      !document.querySelector<HTMLFieldSetElement>('form fieldset')?.disabled
  )
  assert.equal(button('Preview and save'), undefined)
})

test('新增按模型预算行：模型按钮与上限进入草稿，预览与保存携带临时 id 且 body 一致', async () => {
  mockReads(view(), [], [selfTarget])
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
  await until(() =>
    Boolean(document.querySelector('[aria-label="Budget editor"]'))
  )
  const editor = document.querySelector('[aria-label="Budget editor"]')
  assert.ok(editor)
  // 名称为空时不能完成，并有行内提示。
  assert.equal(button('Done')?.disabled, true)
  assert.match(editor.textContent ?? '', /Budget name is required/)
  await type(
    editor.querySelector<HTMLInputElement>('input[name="budgets.1.name"]'),
    'Astra'
  )
  const chip = [...editor.querySelectorAll('button')].find(
    (item) => item.textContent?.trim() === 'gpt-6-astra'
  )
  assert.ok(chip)
  await act(async () => chip.click())
  await type(
    editor.querySelector<HTMLInputElement>('input[type="number"]'),
    '2'
  )
  await until(() => button('Done')?.disabled === false)
  await act(async () => button('Done')?.click())
  await until(() => Boolean(button('Preview and save')))
  await act(async () => button('Preview and save')?.click())
  await until(() => Boolean(button('Confirm and save policy')))
  const manual = calls.findLast((item) => item.method === 'POST')
  assert.ok(manual)
  const previewed = (manual.data as { config: ChannelPeriodConfig }).config
  assert.equal(previewed.budgets.length, 2)
  assert.deepEqual(previewed.budgets[1], {
    id: 'new-budget-1',
    name: 'Astra',
    enabled: true,
    scope: 'pool',
    window: 'daily',
    schedule_id: '',
    models: ['gpt-6-astra'],
    limit: parseQuotaFromDollars(2),
    on_exceed: { mode: 'inherit', channel_id: 0, model: '' },
    created_at: 0,
  })
  await act(async () => button('Confirm and save policy')?.click())
  await until(() => calls.some((item) => item.method === 'PUT'))
  const put = calls.find((item) => item.method === 'PUT')
  assert.deepEqual(put?.data, { expected_revision: 3, config: previewed })
})

test('已保存的预算行从行上打开用量抽屉，确认后才提交目标已用额度', async () => {
  const gets: { url: string; params?: Record<string, unknown> }[] = []
  transport.get = async (url, config) => {
    gets.push({ url, params: config?.params })
    if (url.endsWith('/budgets/usage-summary')) {
      return {
        data: {
          success: true,
          data: summaryOf([
            {
              budget_id: 'b-pool',
              scope: 'pool',
              window_start: 1900000000,
              window_end: 1900086400,
              tracking_since: 0,
              used_quota: 1000,
              pool_used_quota: 1000,
            },
          ]),
        },
      }
    }
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
    if (input.method === 'PUT') {
      return { data: { success: true, data: { used_quota: 0 } } }
    }
    const body = input.data as { config: ChannelPeriodConfig }
    return { data: { success: true, data: previewOf(body.config) } }
  }
  await render()
  await until(() => Boolean(button('Usage')))
  await act(async () => button('Usage')?.click())
  await until(() => Boolean(button('Set usage')))
  const usageGet = gets.find((item) => item.url.includes('/b-pool/usage'))
  assert.equal(usageGet?.url, '/api/channel/80/budgets/b-pool/usage')
  assert.equal(usageGet?.params?.scope, 'pool')
  assert.match(document.body.textContent ?? '', /Pool used/)
  await act(async () => button('Set usage')?.click())
  const panel = document.querySelector('[aria-label="Budget usage"]')
  assert.ok(panel)
  await type(panel.querySelector<HTMLInputElement>('input[type="number"]'), '0')
  await act(async () => button('Confirm')?.click())
  await until(() => Boolean(button('Adjust')))
  assert.equal(calls.filter((item) => item.method === 'PUT').length, 0)
  await act(async () => button('Adjust')?.click())
  await until(() => calls.some((item) => item.method === 'PUT'))
  assert.deepEqual(
    calls.find((item) => item.method === 'PUT'),
    {
      method: 'PUT',
      url: '/api/channel/80/budgets/b-pool/usage',
      data: { scope: 'pool', user_id: 0, used_quota: 0 },
    }
  )
})

test('只读权限不能提交，跨渠道迟到的查询不能覆盖当前面板', async () => {
  let resolveOld: (value: { data: unknown }) => void = () => {}
  transport.get = async (url) => {
    if (url.endsWith('/targets')) return { data: { success: true, data: [] } }
    if (url.endsWith('/budgets/usage-summary')) {
      return { data: { success: true, data: summaryOf([]) } }
    }
    if (url.includes('/80/')) {
      return new Promise((resolve) => {
        resolveOld = resolve
      })
    }
    return { data: { success: true, data: { ...view(9), channel_id: 81 } } }
  }
  let writes = 0
  transport.request = async (input) => {
    if (input.method === 'PUT') writes++
    const body = input.data as { config: ChannelPeriodConfig }
    return { data: { success: true, data: previewOf(body.config) } }
  }
  await render(true, 80)
  await render(false, 81)
  await until(() => Boolean(document.querySelector('form')))
  await act(async () => resolveOld({ data: { success: true, data: view() } }))
  assert.equal(
    document.querySelector<HTMLFieldSetElement>('form fieldset')?.disabled,
    true
  )
  await submit()
  await flush()
  assert.equal(writes, 0)
  assert.equal(button('Confirm and save policy'), undefined)
})

test('预算表展示时段、状态徽标与聚合进度；抽屉关闭和 Esc 都保留草稿', async () => {
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
  mockReads(policy, [
    {
      budget_id: 'b-pool',
      scope: 'pool',
      window_start: 1900000000,
      window_end: 1900086400,
      tracking_since: 0,
      used_quota: 100000,
      pool_used_quota: 100000,
    },
  ])
  transport.request = async (input) => {
    const body = input.data as { config: ChannelPeriodConfig }
    return { data: { success: true, data: previewOf(body.config) } }
  }
  await render()
  await until(() => Boolean(document.querySelector('[role="progressbar"]')))
  const table = document.querySelector('table')
  assert.ok(table)
  assert.match(table.textContent ?? '', /Holiday/)
  assert.equal(
    table.querySelector('[data-status]')?.getAttribute('data-status'),
    'active'
  )
  assert.equal(
    document
      .querySelector('[role="progressbar"]')
      ?.getAttribute('aria-valuenow'),
    '20'
  )
  assert.ok(
    document.querySelector('[aria-label="Default action when exceeded"]')
  )
  await act(async () => button('Edit')?.click())
  await until(() => dialogs().length > 0)
  const editor = document.querySelector('[aria-label="Budget editor"]')
  assert.ok(editor)
  assert.ok(editor.querySelector('select[name="budgets.0.schedule_id"]'))
  await type(
    editor.querySelector('input[name="budgets.0.name"]'),
    'Changed budget'
  )
  await act(async () => button('Done')?.click())
  await until(() => dialogs().length === 0)
  assert.match(table.textContent ?? '', /Changed budget/)
  assert.match(
    document.querySelector('[aria-label="Unsaved changes"]')?.textContent ?? '',
    /1 unsaved changes/
  )
  await act(async () => button('Edit')?.click())
  await until(() => dialogs().length > 0)
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
  await until(() => dialogs().length === 0)
  assert.match(table.textContent ?? '', /Changed budget/)
})

test('个人行进度来自聚合摘要的最高用量用户及其提额，读取失败可重试，新行不请求用量', async () => {
  const policy = view()
  policy.config.budgets[0].scope = 'user'
  const urls: string[] = []
  let failed = true
  transport.get = async (url) => {
    urls.push(url)
    if (url.endsWith('/budgets/usage-summary')) {
      if (failed) throw { isAxiosError: true, response: { status: 503 } }
      return {
        data: {
          success: true,
          data: summaryOf([
            {
              budget_id: 'b-pool',
              scope: 'user',
              window_start: 1900000000,
              window_end: 1900086400,
              tracking_since: 0,
              used_quota: 100000,
              pool_used_quota: 100000,
              top_user: {
                user_id: 7,
                username: 'alice',
                display_name: 'Alice',
                used_quota: 100000,
                effective_limit: 1000000,
                override: true,
              },
            },
          ]),
        },
      }
    }
    return {
      data: { success: true, data: url.endsWith('/targets') ? [] : policy },
    }
  }
  transport.request = async (input) => {
    const body = input.data as { config: ChannelPeriodConfig }
    return { data: { success: true, data: previewOf(body.config) } }
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
  assert.equal(
    urls.filter((url) => url.endsWith('/budgets/usage-summary')).length,
    2
  )
  assert.ok(urls.every((url) => !url.includes('/user-limit-status/')))
  await act(async () => button('Add budget')?.click())
  await until(() =>
    Boolean(document.querySelector('[aria-label="Budget editor"]'))
  )
  await type(
    document.querySelector<HTMLInputElement>('input[name="budgets.1.name"]'),
    'New'
  )
  await until(() => button('Done')?.disabled === false)
  await act(async () => button('Done')?.click())
  await until(() => dialogs().length === 0)
  assert.match(
    document.querySelector('table')?.textContent ?? '',
    /Save to view usage/
  )
  assert.ok(urls.every((url) => !url.includes('/budgets/new-')))
})

test('放弃草稿需确认，确认后恢复权威配置并隐藏保存栏', async () => {
  mockReads(view())
  transport.request = async (input) => {
    const body = input.data as { config: ChannelPeriodConfig }
    return { data: { success: true, data: previewOf(body.config) } }
  }
  await render()
  await until(() => Boolean(document.querySelector('form')))
  const toggle = document.querySelector<HTMLButtonElement>(
    '[role="switch"][aria-label="Enable Pool daily"]'
  )
  assert.ok(toggle)
  await act(async () => toggle.click())
  await until(() => Boolean(button('Preview and save')))
  await act(async () => button('Discard')?.click())
  await until(() => Boolean(alertButton('Discard')))
  await act(async () => alertButton('Discard')?.click())
  await until(() => !document.querySelector('[aria-label="Unsaved changes"]'))
  assert.equal(toggle.getAttribute('aria-checked'), 'true')
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
  transport.get = async (url) => {
    if (url.endsWith('/targets')) return { data: { success: true, data: [] } }
    if (url.endsWith('/budgets/usage-summary')) {
      return { data: { success: true, data: summaryOf([]) } }
    }
    return {
      data: { success: true, data: url.includes('/80/') ? policy : view(8) },
    }
  }
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
  const manual = calls.findLast((item) => item.method === 'POST')
  assert.ok(manual)
  const sent = (manual.data as { config: ChannelPeriodConfig }).config
  assert.equal(sent.model_usage_tracking_enabled, true)
  assert.deepEqual(
    sent.model_usage_tracking,
    policy.config.model_usage_tracking
  )
  assert.equal(sent.budgets[0].counter_source, 'continuous_model')
  await act(async () => button('Confirm and save policy')?.click())
  await until(() => calls.some((item) => item.method === 'PUT'))
  assert.deepEqual(
    calls.find((item) => item.method === 'PUT')?.data,
    manual.data
  )
  await render(false, 81)
  await until(() => Boolean(document.querySelector('form')))
  const readonly = document.querySelector<HTMLButtonElement>(
    '[role="switch"][aria-label="Continuously track model usage"]'
  )
  assert.ok(readonly)
  assert.equal(readonly.getAttribute('aria-checked'), 'false')
  assert.equal(readonly.getAttribute('aria-disabled'), 'true')
})

test('带时段的日预算与同范围同模型的无时段预算共用计数：只画一条进度条，另一行显示文字说明', async () => {
  const policy = view()
  policy.config.schedules = [
    {
      id: 'schedule-1',
      name: 'Weekend',
      enabled: true,
      kind: 'weekly',
      start_local: '',
      end_local: '',
      start_at: 0,
      end_at: 0,
      start_weekday: 5,
      end_weekday: 0,
      start_time: '00:00',
      end_time: '00:00',
      created_at: 1,
    },
  ]
  policy.config.budgets.push({
    id: 'b-weekend',
    name: 'Weekend pool daily',
    enabled: true,
    scope: 'pool',
    window: 'daily',
    schedule_id: 'schedule-1',
    models: [],
    limit: 200000,
    on_exceed: { mode: 'inherit', channel_id: 0, model: '' },
    created_at: 2,
  })
  mockReads(policy, [
    {
      budget_id: 'b-pool',
      scope: 'pool',
      window_start: 1900000000,
      window_end: 1900086400,
      tracking_since: 0,
      used_quota: 100000,
      pool_used_quota: 100000,
    },
    {
      budget_id: 'b-weekend',
      scope: 'pool',
      window_start: 1900000000,
      window_end: 1900086400,
      tracking_since: 0,
      used_quota: 100000,
      pool_used_quota: 100000,
    },
  ])
  transport.request = async (input) => {
    const body = input.data as { config: ChannelPeriodConfig }
    return { data: { success: true, data: previewOf(body.config) } }
  }
  await render()
  await until(() => Boolean(document.querySelector('[role="progressbar"]')))
  assert.equal(document.querySelectorAll('[role="progressbar"]').length, 1)
  const shared = [...document.querySelectorAll('tbody tr')].find((row) =>
    row.textContent?.includes('Weekend pool daily')
  )
  assert.ok(shared)
  assert.match(shared.textContent ?? '', /Shares the counter with Pool daily/)
  assert.match(shared.textContent ?? '', /Current .* · row limit/)
  assert.equal(shared.querySelector('[role="progressbar"]'), null)
})

test('编辑抽屉选择「本渠道换模型」后草稿写入 channel_id=0，预览 body 与表格摘要一致', async () => {
  const policy = view()
  policy.config.budgets[0].models = ['gpt-6-astra']
  const other: ChannelPeriodTarget = {
    id: 81,
    name: 'Other',
    models: ['gpt-6-mini'],
    self: false,
  }
  mockReads(policy, [], [selfTarget, other])
  const calls: RequestInput[] = []
  transport.request = async (input) => {
    calls.push(input)
    const body = input.data as { config: ChannelPeriodConfig }
    return { data: { success: true, data: previewOf(body.config) } }
  }
  await render()
  await until(() => Boolean(button('Edit')))
  await act(async () => button('Edit')?.click())
  await until(() =>
    Boolean(document.querySelector('[aria-label="Budget editor"]'))
  )
  const editor = document.querySelector('[aria-label="Budget editor"]')
  assert.ok(editor)
  const fallback = [...editor.querySelectorAll('button')].find(
    (item) => item.textContent?.trim() === 'Fallback'
  )
  assert.ok(fallback)
  await act(async () => fallback.click())
  await until(() => Boolean(editor.querySelector('select:not([name])')))
  // 先选其他渠道再选回本渠道，确认写入的是 0 而不是本渠道 id。
  await choose(
    editor.querySelector<HTMLSelectElement>('select:not([name])'),
    '81'
  )
  await choose(
    editor.querySelector<HTMLSelectElement>('select:not([name])'),
    '0'
  )
  await choose(
    editor.querySelector<HTMLSelectElement>(
      'select[name="budgets.0.on_exceed.model"]'
    ),
    'gpt-6-mini'
  )
  await until(() => button('Done')?.disabled === false)
  await act(async () => button('Done')?.click())
  await until(() => dialogs().length === 0)
  assert.match(
    document.querySelector('table')?.textContent ?? '',
    /Fallback → This channel · gpt-6-mini/
  )
  await act(async () => button('Preview and save')?.click())
  await until(() => Boolean(button('Confirm and save policy')))
  const manual = calls.findLast((item) => item.method === 'POST')
  assert.ok(manual)
  assert.deepEqual(
    (manual.data as { config: ChannelPeriodConfig }).config.budgets[0]
      .on_exceed,
    { mode: 'fallback', channel_id: 0, model: 'gpt-6-mini' }
  )
})

test('后端 400 点名的预算行被高亮并显示具体原因，打开该行编辑抽屉时同样可见', async () => {
  mockReads(view())
  transport.request = async () => {
    throw {
      isAxiosError: true,
      response: {
        status: 400,
        data: {
          success: false,
          message: '渠道周期策略参数无效: 预算引用的时段不存在：Pool daily',
        },
      },
    }
  }
  await render()
  await until(() => Boolean(document.querySelector('form')))
  const toggle = document.querySelector<HTMLButtonElement>(
    '[role="switch"][aria-label="Enable Pool daily"]'
  )
  assert.ok(toggle)
  await act(async () => toggle.click())
  await until(() => Boolean(button('Preview and save')))
  await act(async () => button('Preview and save')?.click())
  await until(() => Boolean(document.querySelector('tr[data-error="true"]')))
  const row = document.querySelector('tr[data-error="true"]')
  assert.ok(row)
  assert.match(row.textContent ?? '', /Pool daily/)
  assert.equal(
    row.querySelector('[data-slot="budget-row-error"]')?.textContent,
    '预算引用的时段不存在：Pool daily'
  )
  assert.match(
    document.querySelector('form [role="alert"]')?.textContent ?? '',
    /Check schedules.*预算引用的时段不存在：Pool daily/
  )
  // 预览失败不锁定草稿，可以直接打开该行修正；抽屉里同样显示原因。
  await act(async () => button('Edit')?.click())
  await until(() =>
    Boolean(document.querySelector('[aria-label="Budget editor"]'))
  )
  assert.match(
    document.querySelector('[aria-label="Budget editor"] [role="alert"]')
      ?.textContent ?? '',
    /预算引用的时段不存在：Pool daily/
  )
})

test('未到时段的预算行在时段列显示下次开始时间，时段列表同样标注', async () => {
  const policy = view()
  policy.config.schedules = [
    {
      id: 'schedule-1',
      name: 'Weekend',
      enabled: true,
      kind: 'weekly',
      start_local: '',
      end_local: '',
      start_at: 0,
      end_at: 0,
      start_weekday: 4,
      end_weekday: 5,
      start_time: '18:00',
      end_time: '23:30',
      created_at: 1,
    },
  ]
  policy.config.budgets[0].schedule_id = 'schedule-1'
  const nextStart = 1900404000
  mockReads(policy, [
    {
      budget_id: 'b-pool',
      scope: 'pool',
      window_start: 0,
      window_end: 0,
      tracking_since: 0,
      used_quota: 0,
      pool_used_quota: 0,
    },
  ])
  transport.request = async (input) => {
    const body = input.data as { config: ChannelPeriodConfig }
    const preview = previewOf(body.config)
    preview.rows = body.config.budgets.map((row) => ({
      budget_id: row.id,
      active: false,
      enforced: false,
      source: {
        kind: 'weekly',
        schedule_id: 'schedule-1',
        start_at: nextStart,
        end_at: nextStart + 106200,
      },
    }))
    return { data: { success: true, data: preview } }
  }
  await render()
  await until(
    () =>
      document
        .querySelector('table [data-status]')
        ?.getAttribute('data-status') === 'pending'
  )
  const time = formatTimestampToDate(nextStart)
  assert.ok(
    document.querySelector('table')?.textContent?.includes(`Next start ${time}`)
  )
  assert.ok(
    document
      .querySelector('[aria-label="Schedules"]')
      ?.textContent?.includes(`Not started · ${time}`)
  )
})
