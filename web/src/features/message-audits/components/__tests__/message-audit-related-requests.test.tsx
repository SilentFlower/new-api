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
import { after, test } from 'node:test'

import { Window } from 'happy-dom'

import dayjs from '@/lib/dayjs'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'MouseEvent',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { MessageAuditRelatedRequests } =
  await import('../message-audit-related-requests')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: {
    en: {
      translation: {
        'Attempt {{index}}': 'Attempt {{index}}',
        failed: 'failed',
        succeeded: 'succeeded',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

after(() => {
  domWindow.close()
})

test('关联视觉辅助调用显示时间、模型、状态和请求 ID，并可打开详情', async () => {
  const capturedAt = dayjs('2026-08-16 12:34:56').unix()
  const selected: string[] = []
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () =>
    root.render(
      <I18nextProvider i18n={i18n}>
        <MessageAuditRelatedRequests
          requests={[
            {
              request_id: 'main:vision_assist:first',
              model_name: 'vision-model-a',
              status: 'succeeded',
              captured_at: capturedAt,
            },
            {
              request_id: 'main:vision_assist:second',
              model_name: 'vision-model-b',
              status: 'failed',
              captured_at: capturedAt + 2,
            },
          ]}
          onSelectRequest={(requestId) => selected.push(requestId)}
        />
      </I18nextProvider>
    )
  )

  assert.ok(container.textContent?.includes('Attempt 1 · vision-model-a'))
  assert.ok(container.textContent?.includes('main:vision_assist:first'))
  assert.ok(container.textContent?.includes('2026-08-16 12:34:56'))
  assert.ok(container.textContent?.includes('succeeded'))
  assert.ok(container.textContent?.includes('failed'))

  const buttons = container.querySelectorAll<HTMLButtonElement>('button')
  assert.equal(buttons.length, 2)
  await act(async () => buttons[1]?.click())
  assert.deepEqual(selected, ['main:vision_assist:second'])

  await act(async () => root.unmount())
  container.remove()
})
