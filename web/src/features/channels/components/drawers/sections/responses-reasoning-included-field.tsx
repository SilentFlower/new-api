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
import { useId } from 'react'
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import type { ChannelFormValues } from '../../../lib'

/**
 * 渲染渠道级历史推理计数标识开关。
 *
 * @returns 与当前渠道表单绑定的开关字段。
 */
export function ResponsesReasoningIncludedField() {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const labelId = useId()

  return (
    <FormField
      control={form.control}
      name='responses_reasoning_included_enabled'
      render={({ field }) => (
        <FormItem className='flex items-start justify-between gap-3 px-4 py-3'>
          <div className='space-y-0.5'>
            <FormLabel id={labelId}>
              {t('Supplement historical reasoning accounting header')}
            </FormLabel>
            <FormDescription>
              {t(
                'Enable only when upstream Responses usage already includes historical reasoning. Add X-Reasoning-Included: true when missing and preserve upstream values.'
              )}
              <span className='mt-1 block'>
                {t(
                  'Applies to native Responses HTTP/SSE only. Pauses keepalive until upstream headers arrive; does not fix Codex resetting this flag on a new turn.'
                )}
              </span>
            </FormDescription>
          </div>
          <FormControl>
            <Switch
              checked={field.value}
              aria-labelledby={labelId}
              onCheckedChange={field.onChange}
            />
          </FormControl>
        </FormItem>
      )}
    />
  )
}
