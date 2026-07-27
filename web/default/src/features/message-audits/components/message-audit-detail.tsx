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
import { Check, Copy, ListFilter, Loader2, RotateCcw } from 'lucide-react'
import { type ReactNode, useMemo, useState } from 'react'
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
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
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
import { filterMessageAuditMessages } from '../lib/message-audit-ui'
import type { MessageAuditMessage } from '../types'

type MessageAuditDetailPanelProps = {
  requestId: string | null
  onOpenChange: (open: boolean) => void
}

type AuditMessageListProps = {
  messages: MessageAuditMessage[]
  copiedSequence: number | null
  onCopy: (sequence: number, content: unknown) => void
}

function AuditMessageList(props: AuditMessageListProps) {
  const { t } = useTranslation()

  return (
    <ol className='relative space-y-0' aria-label={t('Message timeline')}>
      <span
        className='bg-border absolute top-3 bottom-3 left-3 w-px'
        aria-hidden='true'
      />
      {props.messages.map((message) => {
        const collapseContent = ['tools', 'functions'].includes(
          message.content_type
        )
        return (
          <li key={message.sequence} className='relative pb-6 pl-9 last:pb-0'>
            <span
              className='bg-background absolute top-1 left-0 flex size-6 items-center justify-center rounded-full border font-mono text-[10px]'
              aria-hidden='true'
            >
              {message.sequence + 1}
            </span>
            <article>
              <header className='mb-2 flex items-start justify-between gap-3'>
                <div className='flex min-w-0 flex-wrap items-center gap-2'>
                  <Badge variant='outline'>
                    {message.role || t('Unknown')}
                  </Badge>
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
                  onClick={() =>
                    props.onCopy(message.sequence, message.content)
                  }
                >
                  {props.copiedSequence === message.sequence ? (
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
          </li>
        )
      })}
    </ol>
  )
}

type MessageFilterMenuProps = {
  label: string
  options: string[]
  hiddenOptions: string[]
  onToggle: (option: string, visible: boolean) => void
}

function MessageFilterMenu(props: MessageFilterMenuProps) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant='outline' size='sm' />}>
        <ListFilter aria-hidden='true' />
        {props.label}
      </DropdownMenuTrigger>
      <DropdownMenuContent align='start' className='max-h-72 min-w-48'>
        <DropdownMenuLabel>{props.label}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {props.options.map((option) => (
          <DropdownMenuCheckboxItem
            key={option}
            checked={!props.hiddenOptions.includes(option)}
            onCheckedChange={(checked) => props.onToggle(option, checked)}
          >
            {option}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function DetailBody(props: { requestId: string }) {
  const { t } = useTranslation()
  const [copiedSequence, setCopiedSequence] = useState<number | null>(null)
  const [hiddenRoles, setHiddenRoles] = useState<string[]>([])
  const [hiddenContentTypes, setHiddenContentTypes] = useState<string[]>([])
  const detailQuery = useQuery({
    queryKey: ['message-audit-detail', props.requestId],
    queryFn: () => getMessageAuditDetail(props.requestId),
  })
  const detail = detailQuery.data
  const roles = useMemo(
    () =>
      [...new Set(detail?.messages.map((message) => message.role) ?? [])]
        .filter(Boolean)
        .sort(),
    [detail?.messages]
  )
  const contentTypes = useMemo(
    () =>
      [
        ...new Set(
          detail?.messages.map((message) => message.content_type) ?? []
        ),
      ]
        .filter(Boolean)
        .sort(),
    [detail?.messages]
  )
  const visibleMessages = useMemo(
    () =>
      filterMessageAuditMessages(
        detail?.messages ?? [],
        hiddenRoles,
        hiddenContentTypes
      ),
    [detail?.messages, hiddenContentTypes, hiddenRoles]
  )

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
  const toggleRole = (role: string, visible: boolean) => {
    setHiddenRoles((current) =>
      visible ? current.filter((value) => value !== role) : [...current, role]
    )
  }
  const toggleContentType = (contentType: string, visible: boolean) => {
    setHiddenContentTypes((current) =>
      visible
        ? current.filter((value) => value !== contentType)
        : [...current, contentType]
    )
  }
  const hasActiveFilters =
    hiddenRoles.length > 0 || hiddenContentTypes.length > 0
  let messageContent: ReactNode
  if (detail.messages.length === 0) {
    messageContent = (
      <p className='text-muted-foreground py-8 text-center text-sm'>
        {t('Message body was not captured for this request.')}
      </p>
    )
  } else if (visibleMessages.length === 0) {
    messageContent = (
      <p className='text-muted-foreground py-8 text-center text-sm'>
        {t('No messages match the selected filters.')}
      </p>
    )
  } else {
    messageContent = (
      <AuditMessageList
        messages={visibleMessages}
        copiedSequence={copiedSequence}
        onCopy={copyMessage}
      />
    )
  }

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
        <dt className='text-muted-foreground'>{t('Inferred session')}</dt>
        <dd className='font-mono text-xs break-all'>
          {detail.request.audit_session_id}
        </dd>
      </dl>

      <section className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div>
            <h3 className='text-sm font-medium'>{t('Messages')}</h3>
            <p className='text-muted-foreground mt-0.5 text-xs'>
              {t('{{visible}} of {{total}} messages visible', {
                visible: visibleMessages.length,
                total: detail.messages.length,
              })}
            </p>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <MessageFilterMenu
              label={t('Roles')}
              options={roles}
              hiddenOptions={hiddenRoles}
              onToggle={toggleRole}
            />
            <MessageFilterMenu
              label={t('Content types')}
              options={contentTypes}
              hiddenOptions={hiddenContentTypes}
              onToggle={toggleContentType}
            />
            {hasActiveFilters && (
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => {
                  setHiddenRoles([])
                  setHiddenContentTypes([])
                }}
              >
                <RotateCcw aria-hidden='true' />
                {t('Show all')}
              </Button>
            )}
          </div>
        </div>
        {messageContent}
      </section>
    </div>
  )
}

/**
 * 展示单次消息审计详情，并按屏幕宽度选择 Sheet 或 Drawer。
 *
 * @param props 请求 ID 和开关回调。
 * @returns 响应式消息审计详情面板。
 */
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
