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
import type { Resolver } from 'react-hook-form'

import type { ChannelFormValues } from '../../../../lib'

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
  'HTMLFormElement',
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
  'localStorage',
  'sessionStorage',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { zodResolver } = await import('@hookform/resolvers/zod')
const { createInstance } = await import('i18next')
const { useForm } = await import('react-hook-form')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { Form } = await import('@/components/ui/form')
const { DEFAULT_CURRENCY_CONFIG, useSystemConfigStore } =
  await import('@/stores/system-config-store')
const { CHANNEL_FORM_DEFAULT_VALUES, channelFormSchema } =
  await import('../../../../lib')
const { ChannelUserWeeklyQuotaLimitField } =
  await import('../channel-user-weekly-quota-limit-field')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: {
    en: {
      translation: {
        'User weekly quota limit ({{unit}})':
          'User weekly quota limit ({{unit}})',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

type HarnessProps = {
  onSubmit: (values: ChannelFormValues) => void
}

function Harness(props: HarnessProps) {
  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelFormSchema) as Resolver<ChannelFormValues>,
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'daily-quota-test',
      models: 'gpt-5',
      user_weekly_quota_limit: 0,
    },
  })

  return (
    <I18nextProvider i18n={i18n}>
      <Form {...form}>
        <form onSubmit={form.handleSubmit(props.onSubmit)}>
          <ChannelUserWeeklyQuotaLimitField />
          <button type='submit'>Submit</button>
        </form>
      </Form>
    </I18nextProvider>
  )
}

function changeInputValue(input: HTMLInputElement, value: string) {
  const valueSetter = Object.getOwnPropertyDescriptor(
    domWindow.HTMLInputElement.prototype,
    'value'
  )?.set
  assert.ok(valueSetter)
  valueSetter.call(input, value)
  input.dispatchEvent(
    new domWindow.Event('input', { bubbles: true }) as unknown as Event
  )
}

async function flushAsyncWork() {
  await act(async () => {
    await new Promise<void>((resolve) => setImmediate(resolve))
  })
}

afterEach(() => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG },
  })
  document.body.replaceChildren()
})

afterAll(() => {
  domWindow.close()
})

test('每周额度输入允许清空、重输 600 并按稳定步长提交', async () => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG },
  })
  const submissions: ChannelFormValues[] = []
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)

  await act(async () =>
    root.render(<Harness onSubmit={(values) => submissions.push(values)} />)
  )

  const input = document.querySelector<HTMLInputElement>('input[type="number"]')
  const form = document.querySelector<HTMLFormElement>('form')
  assert.ok(input)
  assert.ok(form)
  assert.equal(input.value, '0')
  assert.equal(input.step, '0.0001')

  await act(async () => changeInputValue(input, ''))
  assert.equal(input.value, '')
  await act(async () =>
    form.dispatchEvent(
      new domWindow.Event('submit', {
        bubbles: true,
        cancelable: true,
      }) as unknown as Event
    )
  )
  await flushAsyncWork()
  assert.equal(submissions.at(-1)?.user_weekly_quota_limit, 0)

  await act(async () => changeInputValue(input, '600'))
  assert.equal(input.value, '600')
  assert.equal(Number.isInteger(Number(input.value) / Number(input.step)), true)
  await act(async () =>
    form.dispatchEvent(
      new domWindow.Event('submit', {
        bubbles: true,
        cancelable: true,
      }) as unknown as Event
    )
  )
  await flushAsyncWork()
  assert.equal(submissions.at(-1)?.user_weekly_quota_limit, 600)

  await act(async () => root.unmount())
})
