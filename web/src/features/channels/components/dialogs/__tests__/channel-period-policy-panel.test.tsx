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

import type { ChannelPeriodView } from '../../../period-types'

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
const transport = api as unknown as {
  get: (url: string) => Promise<{ data: unknown }>
  request: (input: {
    method: string
    url: string
    data: unknown
  }) => Promise<{ data: unknown }>
}
const oldGet = transport.get,
  oldRequest = transport.request
let root: ReturnType<typeof createRoot> | null = null
let client: InstanceType<typeof QueryClient> | null = null

/** @returns 只包含真实接口定义字段的初始配置。 */
function view(revision = 3): ChannelPeriodView {
  return {
    revision,
    timezone: 'Asia/Shanghai',
    now: 1900000000,
    config: {
      schema_version: 1,
      pool_daily_quota_limit: 500000,
      pool_weekly_quota_limit: 0,
      rules: [],
      fallback: { enabled: false, channel_id: 0, model: '' },
    },
  }
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
  const calls: { method: string; data: unknown }[] = []
  transport.request = async (input) => {
    calls.push(input)
    if (input.method === 'PUT') {
      throw { isAxiosError: true, response: { status: 409 } }
    }
    return { data: { success: true, data: view() } }
  }
  await render()
  await until(() => Boolean(document.querySelector('form')))
  assert.equal(button('Confirm and save policy'), undefined)
  await act(async () => {
    document
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
  await until(() => Boolean(button('Confirm and save policy')))
  assert.equal(calls.length, 1)
  assert.equal(calls[0].method, 'POST')
  assert.equal(
    (calls[0].data as { expected_revision: number }).expected_revision,
    3
  )
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
  await act(async () =>
    document
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  )
  await flush()
  assert.equal(writes, 0)
})
