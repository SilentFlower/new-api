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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { FieldGroup, FieldSet } from '@/components/ui/field'
import { formatQuota, formatTimestampToDate } from '@/lib/format'

import {
  channelPeriodErrorKey,
  getChannelBudgetUsage,
  setChannelBudgetUsage,
} from '../../period-api'
import type { ChannelBudgetRow } from '../../period-types'
import { ChannelPeriodAmount } from './channel-period-amount'

const PAGE_SIZE = 20

/** @param props 渠道、预算行与操作权限。 @returns 该行当前窗口的用量列表与直接调整。 */
export function ChannelBudgetUsagePanel(props: {
  channelId: number
  row: ChannelBudgetRow
  canOperate: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [target, setTarget] = useState<{
    userId: number
    label: string
  } | null>(null)
  const [amount, setAmount] = useState<number | null>(null)
  const [confirmAdjust, setConfirmAdjust] = useState(false)
  const [error, setError] = useState('')
  const scope = props.row.scope
  const queryKey = [
    'channels',
    props.channelId,
    'budget-usage',
    props.row.id,
    scope,
    page,
  ]
  const query = useQuery({
    queryKey,
    queryFn: () =>
      getChannelBudgetUsage(props.channelId, props.row.id, {
        scope,
        p: page,
        page_size: PAGE_SIZE,
      }),
    retry: false,
  })
  const mutation = useMutation({
    mutationFn: (input: { user_id: number; used_quota: number }) =>
      setChannelBudgetUsage(props.channelId, props.row.id, { scope, ...input }),
    onSuccess: () => {
      toast.success(t('Budget usage updated'))
      setTarget(null)
      setAmount(null)
      void queryClient.invalidateQueries({
        queryKey: ['channels', props.channelId],
      })
    },
    onError: (reason) => setError(t(channelPeriodErrorKey(reason))),
  })
  const pages = Math.max(1, Math.ceil((query.data?.total ?? 0) / PAGE_SIZE))
  return (
    <section
      className='space-y-3 rounded-lg border p-4'
      aria-label={t('Budget usage')}
    >
      <div className='flex items-center justify-between gap-2'>
        <h4 className='font-medium'>
          {t('Usage')} · {props.row.name}
        </h4>
        <Button type='button' variant='ghost' onClick={props.onClose}>
          {t('Close')}
        </Button>
      </div>
      {query.isPending && <p role='status'>{t('Loading...')}</p>}
      {query.isError && (
        <p role='alert' className='text-destructive text-sm'>
          {t(channelPeriodErrorKey(query.error))}
        </p>
      )}
      {query.data && (
        <div className='space-y-2 text-sm'>
          <p className='text-muted-foreground'>
            {t('Window: {{start}} to {{end}}', {
              start: formatTimestampToDate(query.data.window_start),
              end: formatTimestampToDate(query.data.window_end),
            })}
            {query.data.tracking_since > 0 &&
              ` · ${t('Tracking since {{time}}; earlier usage is excluded.', {
                time: formatTimestampToDate(query.data.tracking_since),
              })}`}
          </p>
          {scope === 'pool' && (
            <div className='flex flex-wrap items-center gap-3'>
              <span>
                {t('Pool used')}: {formatQuota(query.data.used_quota)}
              </span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={!props.canOperate}
                onClick={() => {
                  setTarget({ userId: 0, label: t('Pool') })
                  setAmount(query.data?.used_quota ?? 0)
                }}
              >
                {t('Set usage')}
              </Button>
            </div>
          )}
          {scope === 'user' && query.data.items.length === 0 && (
            <p className='text-muted-foreground'>
              {t('No user usage has been recorded in this window.')}
            </p>
          )}
          {scope === 'user' && query.data.items.length > 0 && (
            <table className='w-full text-left'>
              <thead>
                <tr className='text-muted-foreground'>
                  <th className='py-1 font-normal'>{t('User')}</th>
                  <th className='py-1 font-normal'>{t('Used')}</th>
                  <th className='py-1 font-normal' />
                </tr>
              </thead>
              <tbody>
                {query.data.items.map((item) => (
                  <tr key={item.user_id} className='border-t'>
                    <td className='py-1'>
                      {item.display_name || item.username || `#${item.user_id}`}{' '}
                      <span className='text-muted-foreground'>
                        #{item.user_id}
                      </span>
                    </td>
                    <td className='py-1'>{formatQuota(item.used_quota)}</td>
                    <td className='py-1 text-right'>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        disabled={!props.canOperate}
                        onClick={() => {
                          setTarget({
                            userId: item.user_id,
                            label: item.display_name || item.username,
                          })
                          setAmount(item.used_quota)
                        }}
                      >
                        {t('Set usage')}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {scope === 'user' && pages > 1 && (
            <div className='flex items-center gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
              >
                {t('Previous')}
              </Button>
              <span>
                {page} / {pages}
              </span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page >= pages}
                onClick={() => setPage(page + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          )}
        </div>
      )}
      {target && (
        <FieldGroup>
          <FieldSet disabled={mutation.isPending} className='space-y-2'>
            <p className='text-sm'>
              {t(
                'Set current usage for {{target}}. This only changes the counter, not billing.',
                {
                  target: target.label,
                }
              )}
            </p>
            <ChannelPeriodAmount
              label={t('Adjusted used amount')}
              value={amount}
              onChange={setAmount}
            />
            {error && (
              <p role='alert' className='text-destructive text-sm'>
                {error}
              </p>
            )}
            <div className='flex gap-2'>
              <Button
                type='button'
                disabled={amount === null || Number.isNaN(amount) || amount < 0}
                onClick={() => setConfirmAdjust(true)}
              >
                {t('Confirm')}
              </Button>
              <ConfirmDialog
                open={confirmAdjust}
                onOpenChange={setConfirmAdjust}
                title={t('Adjust used amount?')}
                desc={t(
                  'Sets the current window usage of {{target}} to {{amount}}. Other budgets and billing are unaffected; the change is audited.',
                  {
                    target: target.label,
                    amount: formatQuota(amount ?? 0),
                  }
                )}
                confirmText={t('Adjust')}
                handleConfirm={() => {
                  setConfirmAdjust(false)
                  mutation.mutate({
                    user_id: target.userId,
                    used_quota: amount ?? 0,
                  })
                }}
              />
              <Button
                type='button'
                variant='outline'
                onClick={() => {
                  setTarget(null)
                  setError('')
                }}
              >
                {t('Cancel')}
              </Button>
            </div>
          </FieldSet>
        </FieldGroup>
      )}
    </section>
  )
}
