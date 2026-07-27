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
import { Check, Copy, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
} from '@/components/ui/drawer'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { useMediaQuery } from '@/hooks'
import dayjs from '@/lib/dayjs'

import { getMessageAuditDetail } from '../api'
import type { MessageAuditMessage } from '../types'

type MessageAuditDetailPanelProps = {
  requestId: string | null
  onOpenChange: (open: boolean) => void
}

type AuditMessageListProps = {
  messages: MessageAuditMessage[]
  collapseContent?: boolean
  copiedSequence: number | null
  onCopy: (sequence: number, content: unknown) => void
}

function AuditMessageList({
  messages,
  collapseContent = false,
  copiedSequence,
  onCopy,
}: AuditMessageListProps) {
  const { t } = useTranslation()

  return messages.map((message) => (
    <article key={message.sequence} className='border-b pb-4 last:border-b-0'>
      <header className='mb-2 flex items-center justify-between gap-3'>
        <div className='flex min-w-0 items-center gap-2'>
          <Badge variant='outline'>{message.role || t('Unknown')}</Badge>
          <span className='text-muted-foreground truncate text-xs'>
            {message.content_type}
          </span>
        </div>
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          title={t('Copy message')}
          aria-label={t('Copy message')}
          onClick={() => onCopy(message.sequence, message.content)}
        >
          {copiedSequence === message.sequence ? (
            <Check aria-hidden='true' />
          ) : (
            <Copy aria-hidden='true' />
          )}
        </Button>
      </header>
      {collapseContent ? (
        <details>
          <summary className='text-muted-foreground cursor-pointer text-xs'>
            {t('Details')}
          </summary>
          <pre className='bg-muted/50 mt-2 max-h-96 overflow-auto rounded-md border p-3 text-xs leading-5 break-words whitespace-pre-wrap'>
            {typeof message.content === 'string'
              ? message.content
              : JSON.stringify(message.content, null, 2)}
          </pre>
        </details>
      ) : (
        <pre className='bg-muted/50 max-h-96 overflow-auto rounded-md border p-3 text-xs leading-5 break-words whitespace-pre-wrap'>
          {typeof message.content === 'string'
            ? message.content
            : JSON.stringify(message.content, null, 2)}
        </pre>
      )}
    </article>
  ))
}

function DetailBody(props: { requestId: string }) {
  const { t } = useTranslation()
  const [copiedSequence, setCopiedSequence] = useState<number | null>(null)
  const detailQuery = useQuery({
    queryKey: ['message-audit-detail', props.requestId],
    queryFn: () => getMessageAuditDetail(props.requestId),
  })
  const detail = detailQuery.data

  if (detailQuery.isLoading) {
    return (
      <div className='text-muted-foreground flex min-h-48 items-center justify-center gap-2 text-sm'>
        <Loader2 className='size-4 animate-spin' aria-hidden='true' />
        {t('Loading audit detail...')}
      </div>
    )
  }

  if (!detail) {
    return (
      <div className='text-destructive px-1 py-8 text-sm'>
        {detailQuery.error instanceof Error
          ? detailQuery.error.message
          : t('Failed to load audit detail')}
      </div>
    )
  }

  const copyMessage = async (sequence: number, content: unknown) => {
    await navigator.clipboard.writeText(
      typeof content === 'string' ? content : JSON.stringify(content, null, 2)
    )
    setCopiedSequence(sequence)
    window.setTimeout(() => setCopiedSequence(null), 1200)
  }

  const toolMessages = detail.messages.filter((message) =>
    ['tools', 'functions'].includes(message.content_type)
  )
  const regularMessages = detail.messages.filter(
    (message) => !['tools', 'functions'].includes(message.content_type)
  )

  return (
    <div className='space-y-5 px-1 pb-8'>
      <dl className='grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 border-b pb-4 text-sm'>
        <dt className='text-muted-foreground'>{t('Captured at')}</dt>
        <dd>
          {dayjs.unix(detail.request.captured_at).format('YYYY-MM-DD HH:mm:ss')}
        </dd>
        <dt className='text-muted-foreground'>{t('User')}</dt>
        <dd>{detail.request.username || `#${detail.request.user_id}`}</dd>
        <dt className='text-muted-foreground'>{t('Token')}</dt>
        <dd>{detail.request.token_name || `#${detail.request.token_id}`}</dd>
        <dt className='text-muted-foreground'>{t('Model')}</dt>
        <dd className='break-all'>{detail.request.model_name}</dd>
        <dt className='text-muted-foreground'>{t('Request path')}</dt>
        <dd className='font-mono text-xs break-all'>
          {detail.request.request_path}
        </dd>
      </dl>

      <section className='space-y-3'>
        <h3 className='text-sm font-medium'>{t('Messages')}</h3>
        {detail.messages.length === 0 ? (
          <p className='text-muted-foreground py-8 text-center text-sm'>
            {t('Message body was not captured for this request.')}
          </p>
        ) : (
          <AuditMessageList
            messages={regularMessages}
            copiedSequence={copiedSequence}
            onCopy={copyMessage}
          />
        )}
      </section>

      {toolMessages.length > 0 && (
        <section className='space-y-3'>
          <h3 className='text-sm font-medium'>{t('Tools')}</h3>
          <AuditMessageList
            messages={toolMessages}
            collapseContent
            copiedSequence={copiedSequence}
            onCopy={copyMessage}
          />
        </section>
      )}
    </div>
  )
}

export function MessageAuditDetailPanel(props: MessageAuditDetailPanelProps) {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const open = Boolean(props.requestId)

  if (isMobile) {
    return (
      <Drawer open={open} onOpenChange={props.onOpenChange} direction='right'>
        <DrawerContent className='h-full max-h-none w-full'>
          <DrawerHeader>
            <DrawerTitle>{t('Message audit detail')}</DrawerTitle>
            <DrawerDescription className='font-mono text-xs break-all'>
              {props.requestId}
            </DrawerDescription>
          </DrawerHeader>
          <div className='min-h-0 flex-1 overflow-y-auto px-4'>
            {props.requestId && <DetailBody requestId={props.requestId} />}
          </div>
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Sheet open={open} onOpenChange={props.onOpenChange}>
      <SheetContent
        side='right'
        className='w-full overflow-y-auto sm:max-w-2xl'
      >
        <SheetHeader>
          <SheetTitle>{t('Message audit detail')}</SheetTitle>
          <SheetDescription className='font-mono text-xs break-all'>
            {props.requestId}
          </SheetDescription>
        </SheetHeader>
        <div className='px-4'>
          {props.requestId && <DetailBody requestId={props.requestId} />}
        </div>
      </SheetContent>
    </Sheet>
  )
}
