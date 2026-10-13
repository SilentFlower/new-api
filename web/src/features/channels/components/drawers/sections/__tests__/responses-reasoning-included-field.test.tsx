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

import type { ChannelFormValues } from '../../../../lib'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'HTMLSelectElement',
  'HTMLTextAreaElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'MouseEvent',
  'PointerEvent',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
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
const { useForm } = await import('react-hook-form')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { Form } = await import('@/components/ui/form')
const { ResponsesReasoningIncludedField } =
  await import('../responses-reasoning-included-field')
const i18n = createInstance()
await i18n
  .use(initReactI18next)
  .init({
    lng: 'en',
    fallbackLng: 'en',
    resources: { en: { translation: {} } },
  })
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function Harness() {
  const form = useForm<ChannelFormValues>({
    defaultValues: { responses_reasoning_included_enabled: false },
  })
  const enabled = form.watch('responses_reasoning_included_enabled')
  return (
    <I18nextProvider i18n={i18n}>
      <Form {...form}>
        <ResponsesReasoningIncludedField />
        <output>{String(enabled)}</output>
      </Form>
    </I18nextProvider>
  )
}

after(() => domWindow.close())

test('渠道历史推理计数开关有可访问标签，点击后同步表单并可关闭', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  try {
    await act(async () => root.render(<Harness />))
    const toggle = container.querySelector<HTMLElement>('[role="switch"]')
    assert.ok(toggle)
    const labelID = toggle.getAttribute('aria-labelledby')
    assert.ok(labelID)
    assert.equal(
      [...container.querySelectorAll('label')].find(
        (label) => label.id === labelID
      )?.textContent,
      'Supplement historical reasoning accounting header'
    )
    assert.match(
      container.textContent || '',
      /does not fix Codex resetting this flag/
    )
    assert.equal(toggle.getAttribute('aria-checked'), 'false')
    for (const value of ['true', 'false']) {
      await act(async () => toggle.click())
      assert.equal(toggle.getAttribute('aria-checked'), value)
      assert.equal(container.querySelector('output')?.textContent, value)
    }
  } finally {
    await act(async () => root.unmount())
    container.remove()
  }
})
