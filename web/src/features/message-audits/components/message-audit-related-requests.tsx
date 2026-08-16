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

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import dayjs from '@/lib/dayjs'

import type { MessageAuditRequest } from '../types'

type RelatedMessageAuditRequest = Pick<
  MessageAuditRequest,
  'request_id' | 'model_name' | 'status' | 'captured_at'
>

type MessageAuditRelatedRequestsProps = {
  requests: RelatedMessageAuditRequest[]
  onSelectRequest: (requestId: string) => void
}

/**
 * 展示主请求关联的视觉辅助调用，并提供独立审计详情入口。
 *
 * @param props 关联请求元数据和选择回调。
 * @returns 包含调用时间、模型、状态和请求 ID 的可点击列表。
 */
export function MessageAuditRelatedRequests(
  props: MessageAuditRelatedRequestsProps
) {
  const { t } = useTranslation()

  return (
    <ol className='grid min-w-0 gap-2'>
      {props.requests.map((request, index) => {
        const capturedAt = dayjs.unix(request.captured_at)
        const formattedCapturedAt = capturedAt.format('YYYY-MM-DD HH:mm:ss')

        return (
          <li key={request.request_id} className='min-w-0'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='h-auto w-full min-w-0 justify-start px-3 py-2 text-left whitespace-normal'
              onClick={() => props.onSelectRequest(request.request_id)}
            >
              <span className='min-w-0 flex-1'>
                <span className='flex min-w-0 items-center justify-between gap-2'>
                  <span className='truncate text-xs font-medium'>
                    {t('Attempt {{index}}', { index: index + 1 })} ·{' '}
                    {request.model_name || '-'}
                  </span>
                  <Badge
                    variant={
                      request.status === 'failed' ? 'destructive' : 'outline'
                    }
                    className='shrink-0'
                  >
                    {t(request.status || 'pending')}
                  </Badge>
                </span>
                <span className='text-muted-foreground mt-1 block font-mono text-[11px] break-all'>
                  {request.request_id}
                </span>
                <time
                  dateTime={capturedAt.toISOString()}
                  className='text-muted-foreground mt-1 block text-[11px]'
                >
                  {formattedCapturedAt}
                </time>
              </span>
            </Button>
          </li>
        )
      })}
    </ol>
  )
}
