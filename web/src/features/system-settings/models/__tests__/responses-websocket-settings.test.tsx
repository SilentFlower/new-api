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
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'
import type { ComponentProps } from 'react'

const domWindow = new Window({ url: 'http://localhost' })
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'Element',
  'Text',
  'Range',
  'DOMRect',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
  'localStorage',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { api } = await import('@/lib/api')
const { GlobalSettingsCard } = await import('../global-settings-card')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

/**
 * 修改真实数字控件并触发 React 输入事件。
 * @param input 待编辑的控件。
 * @param value 用户输入。
 * @return 无返回值。
 */
function changeTimeout(input: HTMLInputElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(
    domWindow.HTMLInputElement.prototype,
    'value'
  )?.set
  assert.ok(setter)
  setter.call(input, value)
  input.dispatchEvent(
    new domWindow.Event('input', { bubbles: true }) as unknown as Event
  )
}

/**
 * 生成服务端全局设置的最小有效表单值。
 * @param seconds 服务端当前生效秒数。
 * @return 全局设置表单值。
 */
function globalDefaults(
  seconds: number
): ComponentProps<typeof GlobalSettingsCard>['defaultValues'] {
  return {
    global: {
      pass_through_request_enabled: false,
      thinking_model_blacklist: '[]',
      chat_completions_to_responses_policy: '{}',
    },
    general_setting: {
      ping_interval_enabled: false,
      non_stream_keepalive_enabled: false,
      ping_interval_seconds: 60,
    },
    ResponsesWebSocketFirstFrameTimeoutSeconds: seconds,
  }
}

describe('Responses WebSocket 首帧设置', () => {
  after(() => domWindow.close())

  test('读取服务端有效值并在刷新后展示新的值', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const queryClient = new QueryClient()
    try {
      for (const seconds of [120, 180]) {
        await act(async () => {
          root.render(
            <QueryClientProvider client={queryClient}>
              <I18nextProvider i18n={i18n}>
                <GlobalSettingsCard defaultValues={globalDefaults(seconds)} />
              </I18nextProvider>
            </QueryClientProvider>
          )
        })
        const input = container.querySelector<HTMLInputElement>(
          'input[name="ResponsesWebSocketFirstFrameTimeoutSeconds"]'
        )
        assert.ok(input, '应展示首帧等待时间输入框')
        assert.equal(input.value, String(seconds))
        assert.equal(input.disabled, false)
        const label = container.querySelector(`label[for="${input.id}"]`)
        assert.ok(label?.textContent?.includes('First-frame Timeout'))
      }
    } finally {
      await act(async () => root.unmount())
      queryClient.clear()
      container.remove()
    }
  })

  test('非法秒数显示字段错误并阻止保存', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const queryClient = new QueryClient()
    const originalAdapter = api.defaults.adapter
    let requestCount = 0
    api.defaults.adapter = async (config) => {
      requestCount++
      return {
        config,
        data: { success: true },
        headers: {},
        status: 200,
        statusText: 'OK',
      }
    }
    try {
      await act(async () => {
        root.render(
          <QueryClientProvider client={queryClient}>
            <I18nextProvider i18n={i18n}>
              <GlobalSettingsCard defaultValues={globalDefaults(120)} />
            </I18nextProvider>
          </QueryClientProvider>
        )
      })
      const input = container.querySelector<HTMLInputElement>(
        'input[name="ResponsesWebSocketFirstFrameTimeoutSeconds"]'
      )
      const form = container.querySelector('form')
      assert.ok(input)
      assert.ok(form)
      for (const value of ['', '0', '-1', '1.5', '9223372037']) {
        await act(async () => {
          changeTimeout(input, value)
          form.dispatchEvent(
            new domWindow.Event('submit', {
              bubbles: true,
              cancelable: true,
            }) as unknown as Event
          )
        })
        assert.equal(input.getAttribute('aria-invalid'), 'true', value)
        assert.ok(
          input
            .closest('[data-slot="form-item"]')
            ?.querySelector('[data-slot="form-message"]')?.textContent
        )
      }
      assert.equal(requestCount, 0)
    } finally {
      api.defaults.adapter = originalAdapter
      await act(async () => root.unmount())
      queryClient.clear()
      container.remove()
    }
  })

  test('保存有效值时只提交新设置键及整数秒数', { timeout: 5000 }, async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })
    const originalAdapter = api.defaults.adapter
    const requests: Array<{ key: string; value: number }> = []
    let onRequest: () => void = () => undefined
    const requestReceived = new Promise<void>((resolve) => {
      onRequest = resolve
    })
    api.defaults.adapter = async (config) => {
      requests.push(JSON.parse(config.data as string))
      onRequest()
      return {
        config,
        data: { success: true },
        headers: {},
        status: 200,
        statusText: 'OK',
      }
    }
    try {
      await act(async () => {
        root.render(
          <QueryClientProvider client={queryClient}>
            <I18nextProvider i18n={i18n}>
              <GlobalSettingsCard defaultValues={globalDefaults(120)} />
            </I18nextProvider>
          </QueryClientProvider>
        )
      })
      const input = container.querySelector<HTMLInputElement>(
        'input[name="ResponsesWebSocketFirstFrameTimeoutSeconds"]'
      )
      const form = container.querySelector('form')
      assert.ok(input)
      assert.ok(form)
      await act(async () => changeTimeout(input, '180'))
      await act(async () => {
        form.dispatchEvent(
          new domWindow.Event('submit', {
            bubbles: true,
            cancelable: true,
          }) as unknown as Event
        )
        await requestReceived
      })
      assert.deepEqual(requests, [
        { key: 'ResponsesWebSocketFirstFrameTimeoutSeconds', value: 180 },
      ])
    } finally {
      api.defaults.adapter = originalAdapter
      await act(async () => root.unmount())
      queryClient.clear()
      container.remove()
    }
  })
})
