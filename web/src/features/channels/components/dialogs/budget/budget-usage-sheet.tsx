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

import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import type { ChannelBudgetRow } from '../../../period-types'
import { ChannelBudgetUsagePanel } from '../channel-budget-usage'

/** @param props 渠道、要查看的预算行与权限。 @returns 从行上打开的用量抽屉。 */
export function BudgetUsageSheet(props: {
  channelId: number
  row: ChannelBudgetRow | null
  canOperate: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  return (
    <Sheet
      open={props.row !== null}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <SheetContent
        className='visible-scrollbar w-full overflow-y-auto sm:max-w-xl'
        aria-label={t('Budget usage')}
      >
        <SheetHeader>
          <SheetTitle>{t('Budget usage')}</SheetTitle>
          <SheetDescription>
            {t(
              'Usage for the current window of this budget. Adjustments only change the counter, not billing.'
            )}
          </SheetDescription>
        </SheetHeader>
        {props.row && (
          <div className='px-4 pb-4'>
            <ChannelBudgetUsagePanel
              key={props.row.id}
              channelId={props.channelId}
              row={props.row}
              canOperate={props.canOperate}
              onClose={props.onClose}
            />
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
