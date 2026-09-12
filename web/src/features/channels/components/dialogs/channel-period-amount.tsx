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
import { useTranslation } from 'react-i18next'

import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { getCurrencyLabel } from '@/lib/currency'
import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'

/** @param props 金额输入契约，null 为继承。 @returns 按当前币种转换的输入框。 */
export function ChannelPeriodAmount(props: {
  label: string
  value: number | null
  nullable?: boolean
  onChange: (value: number | null) => void
}) {
  const { t } = useTranslation()
  return (
    <Field>
      <FieldLabel className='grid gap-1 text-sm'>
        {props.label} ({getCurrencyLabel()})
        <Input
          type='number'
          min='0'
          step='any'
          required={!props.nullable}
          value={
            props.value === null || Number.isNaN(props.value)
              ? ''
              : quotaUnitsToDollars(props.value)
          }
          placeholder={
            props.nullable
              ? t('Blank inherits; zero means unlimited')
              : undefined
          }
          onChange={(event) => {
            const text = event.target.value
            if (text === '') {
              props.onChange(props.nullable ? null : Number.NaN)
              return
            }
            const amount = Number(text)
            props.onChange(amount < 0 ? -1 : parseQuotaFromDollars(amount))
          }}
        />
      </FieldLabel>
    </Field>
  )
}
