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
import { test } from 'node:test'

import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { useForm } from 'react-hook-form'
import { I18nextProvider, initReactI18next } from 'react-i18next'

import { Form } from '@/components/ui/form'

import type { ChannelFormValues } from '../../../lib'
import { ResponsesCompactPassthroughField } from './responses-compact-passthrough-field'

function ResponsesCompactPassthroughFieldHarness(props: { enabled: boolean }) {
  const form = useForm<ChannelFormValues>({
    defaultValues: {
      responses_compact_passthrough_enabled: props.enabled,
    },
  })

  return (
    <Form {...form}>
      <ResponsesCompactPassthroughField />
    </Form>
  )
}

test('Responses Compact 透传开关绑定表单值与可访问标签', async () => {
  const i18n = createInstance()
  await i18n.use(initReactI18next).init({
    lng: 'en',
    fallbackLng: 'en',
    resources: {
      en: {
        translation: {
          'Enable Responses Compact passthrough':
            'Enable Responses Compact passthrough',
          'Forward Compact requests to this channel after normal routing and affinity selection.':
            'Forward Compact requests to this channel after normal routing and affinity selection.',
        },
      },
    },
  })

  for (const enabled of [false, true]) {
    const html = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <ResponsesCompactPassthroughFieldHarness enabled={enabled} />
      </I18nextProvider>
    )
    const switchTag = html.match(
      /<(?:button|span)\b[^>]*role="switch"[^>]*>/
    )?.[0]
    const labelTag = html.match(/<label\b[^>]*>/)?.[0]
    const descriptionTag = html.match(
      /<p\b[^>]*data-slot="form-description"[^>]*>/
    )?.[0]

    assert.ok(switchTag)
    assert.ok(labelTag)
    assert.ok(descriptionTag)
    assert.match(html, /Enable Responses Compact passthrough/)
    assert.match(
      html,
      /Forward Compact requests to this channel after normal routing and affinity selection\./
    )
    assert.match(switchTag, new RegExp(`aria-checked="${enabled}"`))

    const labelId = labelTag.match(/\bid="([^"]+)"/)?.[1]
    const labelFor = labelTag.match(/\bfor="([^"]+)"/)?.[1]
    const descriptionId = descriptionTag.match(/\bid="([^"]+)"/)?.[1]
    assert.ok(labelId)
    assert.ok(labelFor)
    assert.ok(descriptionId)
    assert.ok(switchTag.includes(`aria-labelledby="${labelId}"`))
    assert.ok(switchTag.includes(`aria-describedby="${descriptionId}"`))
    assert.ok(html.includes(`<input id="${labelFor}"`))
  }
})
