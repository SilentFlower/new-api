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
 * 渲染渠道级 Responses Compact 透传能力开关。
 *
 * @returns 与当前渠道表单绑定的开关字段。
 */
export function ResponsesCompactPassthroughField() {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const labelId = useId()

  return (
    <FormField
      control={form.control}
      name='responses_compact_passthrough_enabled'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between px-4 py-3'>
          <div className='space-y-0.5'>
            <FormLabel id={labelId}>
              {t('Enable Responses Compact passthrough')}
            </FormLabel>
            <FormDescription>
              {t(
                'Forward Compact requests to this channel after normal routing and affinity selection.'
              )}
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
