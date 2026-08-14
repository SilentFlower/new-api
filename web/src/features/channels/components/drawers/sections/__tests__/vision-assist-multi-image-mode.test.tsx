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
const { CHANNEL_FORM_DEFAULT_VALUES } = await import('../../../../lib')
const { BuildChannelExtraSettingsFields } =
  await import('../build-channel-settings')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: {
    en: {
      translation: {
        'Combine images': 'Combine images',
        'Multi-image mode': 'Multi-image mode',
        'Separate images': 'Separate images',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function Harness() {
  const form = useForm<ChannelFormValues>({
    defaultValues: CHANNEL_FORM_DEFAULT_VALUES,
  })
  const mode = form.watch('vision_assist_multi_image_mode')

  return (
    <I18nextProvider i18n={i18n}>
      <Form {...form}>
        <BuildChannelExtraSettingsFields />
        <output data-testid='multi-image-mode'>{mode}</output>
      </Form>
    </I18nextProvider>
  )
}

function findModeButton(
  container: ParentNode,
  label: string
): HTMLButtonElement {
  const button = [
    ...container.querySelectorAll<HTMLButtonElement>('button'),
  ].find((item) => item.textContent === label)
  assert.ok(button)
  return button
}

after(() => {
  domWindow.close()
})

test('多图模式默认合并且可在两个选项间切换', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => root.render(<Harness />))

  const separateButton = findModeButton(container, 'Separate images')
  const combinedButton = findModeButton(container, 'Combine images')
  const output = container.querySelector('[data-testid="multi-image-mode"]')

  assert.equal(output?.textContent, 'combined')
  assert.equal(separateButton.getAttribute('aria-pressed'), 'false')
  assert.equal(combinedButton.getAttribute('aria-pressed'), 'true')

  await act(async () => separateButton.click())

  assert.equal(output?.textContent, 'separate')
  assert.equal(separateButton.getAttribute('aria-pressed'), 'true')
  assert.equal(combinedButton.getAttribute('aria-pressed'), 'false')

  await act(async () => separateButton.click())

  assert.equal(output?.textContent, 'separate')
  assert.equal(separateButton.getAttribute('aria-pressed'), 'true')

  await act(async () => combinedButton.click())

  assert.equal(output?.textContent, 'combined')
  assert.equal(separateButton.getAttribute('aria-pressed'), 'false')
  assert.equal(combinedButton.getAttribute('aria-pressed'), 'true')

  await act(async () => root.unmount())
  container.remove()
})
