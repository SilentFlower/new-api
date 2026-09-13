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

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatQuota, formatTimestampToDate } from '@/lib/format'

import type { ChannelBudgetPreview } from '../../../period-types'
import { ChannelPeriodSourceLabel } from '../channel-period-metrics'
import type { ChannelPolicyChange } from './policy-diff'

/** @param props 预览结果、改动清单与确认回调。 @returns 逐字段 diff 与保存后生效结果的确认弹层。 */
export function PolicyPreviewDialog(props: {
  open: boolean
  preview: ChannelBudgetPreview | null
  changes: ChannelPolicyChange[]
  disablesRows: boolean
  busy: boolean
  onClose: () => void
  onConfirm: () => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Preview and save')}
      description={
        props.preview
          ? t(
              'Based on policy version {{revision}}. Saving bumps the version; other admins must reload their drafts.',
              {
                revision: props.preview.revision,
              }
            )
          : undefined
      }
      contentClassName='sm:max-w-3xl'
      bodyContainerClassName='visible-scrollbar'
      footer={
        <div className='flex justify-end gap-2'>
          <Button
            type='button'
            variant='outline'
            disabled={props.busy}
            onClick={props.onClose}
          >
            {t('Back to editing')}
          </Button>
          <Button
            type='button'
            disabled={props.busy || !props.preview}
            onClick={props.onConfirm}
          >
            {t('Confirm and save policy')}
          </Button>
        </div>
      }
    >
      {props.preview && (
        <div className='space-y-5' aria-label={t('Policy preview')}>
          {props.disablesRows && (
            <Alert>
              <AlertDescription>
                {t(
                  'Disabled budgets keep their counters; re-enabling continues from the current usage.'
                )}
              </AlertDescription>
            </Alert>
          )}
          <section className='space-y-2'>
            <h4 className='text-muted-foreground text-xs font-semibold tracking-wide uppercase'>
              {t('Changes')}
            </h4>
            <div className='overflow-x-auto rounded-lg border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Budget')}</TableHead>
                    <TableHead>{t('Field')}</TableHead>
                    <TableHead>{t('Before')}</TableHead>
                    <TableHead>{t('After')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {props.changes.map((change) => (
                    <TableRow key={change.key}>
                      <TableCell className='font-medium'>
                        {change.subject}
                      </TableCell>
                      <TableCell className='text-muted-foreground'>
                        {change.field}
                      </TableCell>
                      <TableCell className='text-muted-foreground line-through'>
                        {change.from || '—'}
                      </TableCell>
                      <TableCell className='font-medium'>{change.to}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </section>
          <section className='space-y-2'>
            <h4 className='text-muted-foreground text-xs font-semibold tracking-wide uppercase'>
              {t('Effective limits after saving')}
            </h4>
            <div className='overflow-x-auto rounded-lg border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Budget')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead className='text-right'>{t('Limit')}</TableHead>
                    <TableHead>{t('Source')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {props.preview.rows.map((row) => {
                    const budget = props.preview?.config.budgets.find(
                      (item) => item.id === row.budget_id
                    )
                    return (
                      <TableRow key={row.budget_id}>
                        <TableCell className='font-medium'>
                          {budget?.name ?? row.budget_id}
                        </TableCell>
                        <TableCell>
                          {row.active ? t('Active now') : t('Inactive now')} ·{' '}
                          {row.enforced
                            ? t('Enforced')
                            : t('Overridden by a higher-priority schedule')}
                        </TableCell>
                        <TableCell className='text-right tabular-nums'>
                          {budget && budget.limit > 0
                            ? formatQuota(budget.limit)
                            : t('Unlimited')}
                        </TableCell>
                        <TableCell>
                          <ChannelPeriodSourceLabel source={row.source} />
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
            {!!props.preview.next_change_at && (
              <p className='text-muted-foreground text-sm'>
                {t('Next schedule change: {{time}}', {
                  time: formatTimestampToDate(props.preview.next_change_at),
                })}
              </p>
            )}
          </section>
        </div>
      )}
    </Dialog>
  )
}
