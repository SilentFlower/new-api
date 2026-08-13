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
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, Loader2, Minimize2 } from 'lucide-react'
import { Fragment, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'

import { getMessageAuditSessionRequests } from '../api'
import {
  getMessageAuditSessionMatchLabelKey,
  keepMessageAuditSessionPlaceholder,
} from '../lib/message-audit-ui'

type MessageAuditSessionDialogProps = {
  sessionId: string | null
  onOpenChange: (open: boolean) => void
  onSelectRequest: (requestId: string) => void
}

/**
 * 展示指定推断会话内的全部单次请求。
 *
 * @param props 会话 ID、开关回调和请求选择回调。
 * @returns 可分页的会话历史对话框。
 */
export function MessageAuditSessionDialog(
  props: MessageAuditSessionDialogProps
) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const pageSize = 50
  const open = Boolean(props.sessionId)

  useEffect(() => {
    setPage(1)
  }, [props.sessionId])

  const sessionQuery = useQuery({
    queryKey: ['message-audit-session', props.sessionId, page, pageSize],
    queryFn: () => {
      if (!props.sessionId) {
        throw new Error(t('Inferred session ID is required'))
      }
      return getMessageAuditSessionRequests(props.sessionId, page, pageSize)
    },
    enabled: open,
    placeholderData: (previousData, previousQuery) =>
      keepMessageAuditSessionPlaceholder(
        previousData,
        previousQuery?.queryKey[1],
        props.sessionId
      ),
  })
  const data = sessionQuery.data
  const pageCount = Math.max(1, Math.ceil((data?.total ?? 0) / pageSize))

  return (
    <Dialog open={open} onOpenChange={props.onOpenChange}>
      <DialogContent className='grid max-h-[85vh] grid-rows-[auto_minmax(0,1fr)_auto] sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{t('Inferred session history')}</DialogTitle>
          <DialogDescription className='font-mono text-xs break-all'>
            {props.sessionId}
          </DialogDescription>
        </DialogHeader>

        <div className='min-h-0 overflow-y-auto border-y'>
          {sessionQuery.isLoading && (
            <div className='text-muted-foreground flex min-h-48 items-center justify-center gap-2 text-sm'>
              <Loader2 className='size-4 animate-spin' aria-hidden='true' />
              {t('Loading session history...')}
            </div>
          )}
          {sessionQuery.isError && (
            <div className='text-destructive px-3 py-8 text-sm'>
              {sessionQuery.error instanceof Error
                ? sessionQuery.error.message
                : t('Failed to load session history')}
            </div>
          )}
          {!sessionQuery.isLoading && !sessionQuery.isError && data && (
            <div className='divide-y'>
              {data.items.map((request) => {
                const isCompressed = request.session_match === 'compressed'
                return (
                  <Fragment key={request.request_id}>
                    {isCompressed && (
                      <div className='border-y border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs font-medium text-amber-700 dark:text-amber-300'>
                        <span className='flex items-center gap-2'>
                          <Minimize2 className='size-4' aria-hidden='true' />
                          {t('Context compressed here')}
                        </span>
                      </div>
                    )}
                    <button
                      type='button'
                      className={cn(
                        'hover:bg-muted/40 focus-visible:ring-ring grid w-full gap-2 px-3 py-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset sm:grid-cols-[150px_minmax(0,1fr)_auto] sm:items-center',
                        isCompressed &&
                          'border-l-4 border-l-amber-500 bg-amber-500/5'
                      )}
                      onClick={() => {
                        props.onSelectRequest(request.request_id)
                        props.onOpenChange(false)
                      }}
                    >
                      <span className='text-sm'>
                        {dayjs
                          .unix(request.captured_at)
                          .format('YYYY-MM-DD HH:mm:ss')}
                      </span>
                      <span className='min-w-0'>
                        <span className='block truncate font-mono text-xs'>
                          {request.request_id}
                        </span>
                        <span className='text-muted-foreground mt-1 block truncate text-xs'>
                          {request.token_name || `#${request.token_id}`} ·{' '}
                          {request.model_name}
                        </span>
                      </span>
                      <span className='flex flex-wrap items-center gap-2 sm:justify-end'>
                        <Badge
                          variant='outline'
                          className={
                            isCompressed
                              ? 'border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-300'
                              : undefined
                          }
                        >
                          {isCompressed && <Minimize2 aria-hidden='true' />}
                          {t(
                            getMessageAuditSessionMatchLabelKey(
                              request.session_match
                            )
                          )}
                        </Badge>
                        <Badge
                          variant={
                            request.status === 'failed'
                              ? 'destructive'
                              : 'outline'
                          }
                        >
                          {t(request.status || 'pending')}
                        </Badge>
                      </span>
                    </button>
                  </Fragment>
                )
              })}
              {data.items.length === 0 && (
                <p className='text-muted-foreground px-3 py-10 text-center text-sm'>
                  {t('No requests found in this inferred session.')}
                </p>
              )}
            </div>
          )}
        </div>

        <DialogFooter className='items-center sm:justify-between'>
          <span className='text-muted-foreground text-xs'>
            {t('{{count}} requests', { count: data?.total ?? 0 })}
          </span>
          <div className='flex items-center gap-2'>
            <Button
              type='button'
              variant='outline'
              size='icon-sm'
              title={t('Previous page')}
              aria-label={t('Previous page')}
              disabled={page <= 1 || sessionQuery.isFetching}
              onClick={() => setPage((value) => Math.max(1, value - 1))}
            >
              <ChevronLeft aria-hidden='true' />
            </Button>
            <span className='min-w-16 text-center text-xs'>
              {page}/{pageCount}
            </span>
            <Button
              type='button'
              variant='outline'
              size='icon-sm'
              title={t('Next page')}
              aria-label={t('Next page')}
              disabled={page >= pageCount || sessionQuery.isFetching}
              onClick={() => setPage((value) => Math.min(pageCount, value + 1))}
            >
              <ChevronRight aria-hidden='true' />
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
