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

import { Button } from '@/components/ui/button'

import type { ChannelPolicyChange } from './policy-diff'

/** @param props 改动清单、禁用状态与放弃/预览回调。 @returns 有未保存改动时显示的常驻保存栏。 */
export function PolicySaveBar(props: {
  changes: ChannelPolicyChange[]
  disabled: boolean
  busy: boolean
  onDiscard: () => void
  onPreview: () => void
}) {
  const { t } = useTranslation()
  if (props.changes.length === 0) return null
  const summary = [
    ...new Set(props.changes.map((item) => `${item.subject} · ${item.field}`)),
  ]
  return (
    <div
      role='region'
      aria-label={t('Unsaved changes')}
      className='bg-background/95 supports-[backdrop-filter]:bg-background/80 sticky bottom-0 z-10 -mx-1 flex flex-wrap items-center gap-3 border-t px-1 py-3 backdrop-blur'
    >
      <span className='bg-warning size-2 shrink-0 rounded-full' aria-hidden />
      <span className='text-sm'>
        <span className='font-medium'>
          {t('{{count}} unsaved changes', { count: props.changes.length })}
        </span>
        <span className='text-muted-foreground'>
          {' · '}
          {summary.slice(0, 3).join(', ')}
          {summary.length > 3 ? ` ${t('and more')}` : ''}
        </span>
      </span>
      <span className='flex-1' />
      <Button
        type='button'
        variant='outline'
        disabled={props.busy}
        onClick={props.onDiscard}
      >
        {t('Discard')}
      </Button>
      <Button
        type='button'
        disabled={props.disabled || props.busy}
        onClick={props.onPreview}
      >
        {t('Preview and save')}
      </Button>
    </div>
  )
}
