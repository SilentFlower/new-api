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
import type { ChannelModelOption } from '../../../../types'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'HTMLLabelElement',
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
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { Form } = await import('@/components/ui/form')
const { CHANNEL_FORM_DEFAULT_VALUES } = await import('../../../../lib')
const { VisionAssistModelFields } =
  await import('../vision-assist-model-fields')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: {
    en: {
      translation: {
        'Assist channel': 'Assist channel',
        'Assist model': 'Assist model',
        'Failed to load channel options': 'Failed to load channel options',
        'Loading channels...': 'Loading channels...',
        'No enabled channels': 'No enabled channels',
        Retry: 'Retry',
        'Search channels by name or ID': 'Search channels by name or ID',
        'Search or enter a model': 'Search or enter a model',
        'Select an assist channel first': 'Select an assist channel first',
        Unavailable: 'Unavailable',
        'Unavailable channel (#{{id}})': 'Unavailable channel (#{{id}})',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

type HarnessProps = {
  defaultValues?: Partial<ChannelFormValues>
}

function Harness(props: HarnessProps) {
  const form = useForm<ChannelFormValues>({
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      ...props.defaultValues,
    },
  })
  const channelID = form.watch('vision_assist_channel_id')
  const model = form.watch('vision_assist_model')

  return (
    <I18nextProvider i18n={i18n}>
      <Form {...form}>
        <VisionAssistModelFields />
        <output data-testid='channel-id'>{channelID}</output>
        <output data-testid='model'>{model}</output>
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

function findInputByLabel(
  container: ParentNode,
  labelText: string
): HTMLInputElement {
  const label = [...container.querySelectorAll<HTMLLabelElement>('label')].find(
    (item) => item.textContent === labelText
  )
  assert.ok(label)
  const input = container.querySelector<HTMLInputElement>(
    `input[id="${label.htmlFor}"]`
  )
  assert.ok(input)
  return input
}

function findOption(container: ParentNode, label: string): HTMLLIElement {
  const option = [
    ...container.querySelectorAll<HTMLLIElement>('li[role="option"]'),
  ].find((item) => item.textContent?.includes(label))
  assert.ok(option)
  return option
}

async function renderHarness(
  options: ChannelModelOption[],
  defaultValues?: Partial<ChannelFormValues>
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnMount: false },
    },
  })
  queryClient.setQueryData(['channel-model-options'], options)
  return renderWithQueryClient(queryClient, defaultValues)
}

async function renderWithQueryClient(
  queryClient: InstanceType<typeof QueryClient>,
  defaultValues?: Partial<ChannelFormValues>
) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () =>
    root.render(
      <QueryClientProvider client={queryClient}>
        <Harness defaultValues={defaultValues} />
      </QueryClientProvider>
    )
  )

  return { container, queryClient, root }
}

after(() => {
  domWindow.close()
})

test('同名渠道显示各自 ID，按 ID 搜索后切换渠道会清空旧模型', async () => {
  const rendered = await renderHarness(
    [
      { id: 12, name: '同名渠道', models: ['old-model'] },
      { id: 34, name: '同名渠道', models: ['vision-new'] },
    ],
    {
      vision_assist_channel_id: 12,
      vision_assist_model: 'old-model',
    }
  )
  const channelInput = findInputByLabel(rendered.container, 'Assist channel')
  const channelLabel = rendered.container.querySelector<HTMLLabelElement>(
    `label[for="${channelInput.id}"]`
  )
  assert.ok(channelLabel)
  assert.equal(channelInput.value, '同名渠道 (#12)')
  assert.equal(channelInput.getAttribute('aria-invalid'), 'false')
  assert.ok(channelInput.getAttribute('aria-describedby'))
  assert.equal(channelLabel.htmlFor, channelInput.id)

  await act(async () => channelInput.focus())
  assert.equal(document.activeElement, channelInput)

  await act(async () => changeInputValue(channelInput, '34'))
  assert.equal(
    findOption(rendered.container, '(#34)').textContent,
    '同名渠道 (#34)'
  )
  assert.equal(
    rendered.container.querySelectorAll('li[role="option"]').length,
    1
  )

  await act(async () =>
    findOption(rendered.container, '(#34)').dispatchEvent(
      new domWindow.MouseEvent('mousedown', {
        bubbles: true,
      }) as unknown as MouseEvent
    )
  )
  assert.equal(
    rendered.container.querySelector('[data-testid="channel-id"]')?.textContent,
    '34'
  )
  assert.equal(
    rendered.container.querySelector('[data-testid="model"]')?.textContent,
    ''
  )

  const modelInput = findInputByLabel(rendered.container, 'Assist model')
  assert.equal(modelInput.disabled, false)
  await act(async () => changeInputValue(modelInput, 'custom-vision-model'))
  assert.equal(
    rendered.container.querySelector('[data-testid="model"]')?.textContent,
    'custom-vision-model'
  )

  await act(async () => rendered.root.unmount())
  rendered.container.remove()
  rendered.queryClient.clear()
})

test('历史失效渠道和模型保持可见，未选择渠道时模型禁用', async () => {
  const historical = await renderHarness(
    [{ id: 12, name: '可用渠道', models: ['vision-model'] }],
    {
      vision_assist_channel_id: 99,
      vision_assist_model: 'legacy-model',
    }
  )
  assert.equal(
    findInputByLabel(historical.container, 'Assist channel').value,
    'Unavailable channel (#99)'
  )
  const historicalModel = findInputByLabel(historical.container, 'Assist model')
  assert.equal(historicalModel.value, 'legacy-model (Unavailable)')
  assert.equal(historicalModel.disabled, false)
  await act(async () => historical.root.unmount())
  historical.container.remove()
  historical.queryClient.clear()

  const empty = await renderHarness([])
  const emptyModel = findInputByLabel(empty.container, 'Assist model')
  assert.equal(emptyModel.disabled, true)
  const emptyChannel = findInputByLabel(empty.container, 'Assist channel')
  await act(async () => emptyChannel.focus())
  assert.ok(empty.container.textContent?.includes('No enabled channels'))
  await act(async () => empty.root.unmount())
  empty.container.remove()
  empty.queryClient.clear()
})

test('渠道加载期间禁用选择，失败后保留历史值并提供重试入口', async () => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnMount: false },
    },
  })
  let rejectRequest: (reason?: unknown) => void = () => undefined
  const pendingRequest = new Promise<ChannelModelOption[]>((_, reject) => {
    rejectRequest = reject
  })
  void queryClient
    .fetchQuery({
      queryKey: ['channel-model-options'],
      queryFn: () => pendingRequest,
    })
    .catch(() => undefined)
  const rendered = await renderWithQueryClient(queryClient, {
    vision_assist_channel_id: 99,
    vision_assist_model: 'legacy-model',
  })
  const channelInput = findInputByLabel(rendered.container, 'Assist channel')
  assert.equal(channelInput.disabled, true)
  assert.equal(channelInput.getAttribute('aria-expanded'), 'false')
  assert.equal(channelInput.value, 'Unavailable channel (#99)')

  await act(async () => {
    rejectRequest(new Error('load failed'))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })

  assert.equal(channelInput.disabled, false)
  assert.ok(
    rendered.container.textContent?.includes('Failed to load channel options')
  )
  assert.ok(
    [...rendered.container.querySelectorAll('button')].some(
      (button) => button.textContent === 'Retry'
    )
  )
  assert.equal(
    rendered.container.querySelector('[data-testid="channel-id"]')?.textContent,
    '99'
  )
  assert.equal(
    rendered.container.querySelector('[data-testid="model"]')?.textContent,
    'legacy-model'
  )

  await act(async () => rendered.root.unmount())
  rendered.container.remove()
  rendered.queryClient.clear()
})
