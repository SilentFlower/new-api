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
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { getCurrencyLabel } from '@/lib/currency'
import { getEditableQuotaStep } from '@/lib/format'

import { FIELD_DESCRIPTIONS } from '../../../constants'
import type { ChannelFormValues } from '../../../lib'

/**
 * 渲染渠道单用户每日额度上限字段，并保留输入中的临时空值。
 *
 * @returns 与当前渠道表单绑定的每日额度输入字段。
 */
export function ChannelUserDailyQuotaLimitField() {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()

  return (
    <FormField
      control={form.control}
      name='user_daily_quota_limit'
      render={({ field }) => (
        <FormItem>
          <FormLabel>
            {t('User daily quota limit ({{unit}})', {
              unit: getCurrencyLabel(),
            })}
          </FormLabel>
          <FormControl>
            <Input
              type='number'
              min={0}
              step={getEditableQuotaStep()}
              placeholder='0'
              {...field}
              onChange={(event) => field.onChange(event.target.value)}
            />
          </FormControl>
          <FormDescription>
            <span className='block'>
              {t(FIELD_DESCRIPTIONS.USER_DAILY_QUOTA_LIMIT)}
            </span>
            <span className='block'>
              {t(
                'This is a soft limit based on settled usage. Concurrent requests may exceed it slightly.'
              )}
            </span>
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
