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

const bunTestModule = 'bun:test'
const { test, afterEach, afterAll } = await import(bunTestModule)
const domWindow = new Window()
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'HTMLSelectElement',
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
] as const) {
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
const { QueryClient, QueryClientProvider, notifyManager } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { api } = await import('@/lib/api')
const { ChannelLimitParentPanel } =
  await import('../channel-limit-parent-panel')
const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: { en: { translation: {} } },
})
;(
  globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true
notifyManager.setNotifyFunction((callback) => act(callback))

const client = api as unknown as {
  get: (url: string) => Promise<{ data: unknown }>
  put: (url: string, body: unknown) => Promise<{ data: unknown }>
}
const originalGet = client.get
const originalPut = client.put
let root: ReturnType<typeof createRoot> | null = null
let queryClient: InstanceType<typeof QueryClient> | null = null

/** @returns 一个已关联父渠道的权威响应。 */
function relation() {
  return {
    channel_id: 77,
    parent_channel_id: 80,
    revision: 2,
    read_only: true,
    concurrency_limit: 4,
    parent: { id: 80, name: 'Same name' },
  }
}

/** @param predicate 预期可见状态。 @returns 状态完成后的 Promise。 */
async function until(predicate: () => boolean) {
  for (let attempt = 0; attempt < 100; attempt++) {
    if (predicate()) return
    await act(async () => {
      await new Promise<void>((resolve) => setImmediate(resolve))
    })
  }
  assert.fail('预期界面状态未出现')
}

/** @param blocked 是否存在预算草稿。 @returns 面板及安全的边界请求夹具。 */
async function renderPanel(blocked = false) {
  const host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const currentClient = queryClient
  await act(async () => {
    root?.render(
      <QueryClientProvider client={currentClient}>
        <I18nextProvider i18n={i18n}>
          <ChannelLimitParentPanel
            channelId={77}
            canOperate
            blockedByDraft={blocked}
            onManageParent={() => undefined}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
  await until(() => document.querySelector('select') !== null)
}

/** @param label 按钮可访问文本。 @returns 对应按钮。 */
function button(label: string) {
  const value = [...document.querySelectorAll('button')].find(
    (item) => item.textContent?.trim() === label
  )
  assert.ok(value, label)
  return value
}

/** @param value 选中的父渠道 ID。 @returns 完成变更事件。 */
async function selectParent(value: string) {
  const select = document.querySelector('select')
  assert.ok(select)
  await act(async () => {
    select.value = value
    select.dispatchEvent(
      new domWindow.Event('change', { bubbles: true }) as unknown as Event
    )
  })
}

function setupRequests() {
  client.get = async (url) => ({
    data: {
      success: true,
      data: url.endsWith('/options')
        ? {
            items: [{ id: 90, name: 'Same name', status: 1 }],
            total: 1,
            page: 1,
            page_size: 20,
          }
        : relation(),
    },
  })
}

afterEach(async () => {
  await act(async () => root?.unmount())
  root = null
  queryClient?.clear()
  queryClient = null
  client.get = originalGet
  client.put = originalPut
  document.body.replaceChildren()
})
afterAll(() => domWindow.close())

test('当前父渠道不在候选页时仍保留其名称和 ID，同名候选可区分', async () => {
  setupRequests()
  await renderPanel()
  await until(() => document.querySelectorAll('option').length === 3)
  const select = document.querySelector('select')
  assert.ok(select)
  assert.equal(select.value, '80')
  assert.match(select.textContent ?? '', /Same name \(#80\)/)
  assert.match(select.textContent ?? '', /Same name \(#90\)/)
  assert.match(
    document.body.textContent ?? '',
    /Inherited limits are read-only/
  )
})

test('解除关联经确认后发送显式零父渠道和确认版本，回显为独立限制', async () => {
  setupRequests()
  const writes: Array<{ url: string; body: unknown }> = []
  client.put = async (url, body) => {
    writes.push({ url, body })
    client.get = async (requestUrl) => ({
      data: {
        success: true,
        data: requestUrl.endsWith('/options')
          ? { items: [], total: 0, page: 1, page_size: 20 }
          : {
              channel_id: 77,
              parent_channel_id: 0,
              revision: 3,
              read_only: false,
              concurrency_limit: 1,
            },
      },
    })
    return client.get(url)
  }
  await renderPanel()
  await selectParent('0')
  await act(async () => button('Save').click())
  assert.equal(writes.length, 0)
  await until(() =>
    [...document.querySelectorAll('button')].some(
      (item) => item.textContent?.trim() === 'Confirm'
    )
  )
  await act(async () => button('Confirm').click())
  await until(() => writes.length === 1)
  assert.deepEqual(writes[0], {
    url: '/api/channel/77/limit-parent',
    body: { expected_revision: 2, parent_channel_id: 0 },
  })
  await until(() =>
    (document.body.textContent ?? '').includes(
      'This channel uses its own limits'
    )
  )
})

test('存在预算草稿时父渠道选择和保存均禁用，不能覆盖未保存配置', async () => {
  setupRequests()
  await renderPanel(true)
  const select = document.querySelector('select')
  assert.ok(select)
  assert.equal(select.disabled, true)
  assert.equal(button('Save').disabled, true)
  assert.match(
    document.body.textContent ?? '',
    /Save or discard budget changes/
  )
})
