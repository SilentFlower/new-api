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
import {
  Check,
  ChevronLeft,
  ChevronRight,
  Copy,
  ListFilter,
  Loader2,
  Minimize2,
  RotateCcw,
  ShieldCheck,
} from 'lucide-react'
import { type ReactNode, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { useMediaQuery } from '@/hooks'
import dayjs from '@/lib/dayjs'

import {
  getMessageAuditDetail,
  getMessageAuditReview,
  getMessageAuditSessionRequests,
  startMessageAuditReview,
} from '../api'
import {
  filterMessageAuditMessages,
  getMessageAuditErrorMessage,
  getMessageAuditRequestFailureLabelKey,
  getMessageAuditReviewCategoryLabelKey,
  getMessageAuditReviewCallOutcomeLabelKey,
  getMessageAuditReviewCallPhaseLabelKey,
  getMessageAuditReviewErrorStageLabelKey,
  getMessageAuditReviewFailureLabelKey,
  getMessageAuditReviewPollInterval,
  getMessageAuditReviewProtocolLabelKey,
  getMessageAuditReviewStatusLabelKey,
  getMessageAuditReviewUncoveredLabelKey,
  getMessageAuditRiskLabelKey,
  getMessageAuditSessionMatchLabelKey,
  getMessageAuditStorageModeLabelKey,
} from '../lib/message-audit-ui'
import type {
  MessageAuditMessage,
  MessageAuditReview,
  MessageAuditRiskLevel,
} from '../types'

type MessageAuditDetailPanelProps = {
  requestId: string | null
  onOpenChange: (open: boolean) => void
  onSelectRequest: (requestId: string) => void
}

type AuditMessageListProps = {
  messages: MessageAuditMessage[]
  copiedSequence: number | null
  onCopy: (sequence: number, content: unknown) => void
}

const messageAuditDetailScrollClassName = 'visible-scrollbar'

function formatAuditBytes(bytes: number | null): string {
  if (bytes === null || !Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const index = Math.min(
    units.length - 1,
    Math.floor(Math.log(bytes) / Math.log(1024))
  )
  return `${(bytes / Math.pow(1024, index)).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function getRiskBadgeClass(riskLevel: MessageAuditRiskLevel | ''): string {
  switch (riskLevel) {
    case 'high':
      return 'border-red-500/50 bg-red-500/10 text-red-700 dark:text-red-300'
    case 'medium':
      return 'border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-300'
    case 'low':
      return 'border-sky-500/50 bg-sky-500/10 text-sky-700 dark:text-sky-300'
    case 'none':
      return 'border-emerald-500/50 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
    default:
      return ''
  }
}

function MessageAuditReviewOverviewGrid(props: { review: MessageAuditReview }) {
  const { t } = useTranslation()
  const overview = props.review.result?.overview
  if (!overview || overview.source_count <= 0) return null

  const items = [
    {
      label: t('Sources'),
      value: `${overview.covered_source_count}/${overview.source_count}`,
    },
    {
      label: t('Messages covered'),
      value: `${overview.covered_message_count}/${overview.message_count}`,
    },
    {
      label: t('Chunks covered'),
      value: `${overview.covered_chunk_count}/${overview.virtual_chunk_count}`,
    },
    {
      label: t('Unreviewed sources'),
      value: overview.uncovered_source_count,
    },
    {
      label: t('Estimated tokens'),
      value: overview.estimated_tokens,
    },
  ]

  return (
    <div className='grid gap-2 sm:grid-cols-5'>
      {items.map((item) => (
        <div key={item.label} className='rounded-md border px-3 py-2'>
          <div className='text-muted-foreground text-[11px]'>{item.label}</div>
          <div className='mt-1 text-sm font-medium'>{item.value}</div>
        </div>
      ))}
    </div>
  )
}

function getLastFailedReviewCall(review: MessageAuditReview | undefined) {
  const calls = review?.diagnostics?.calls ?? []
  for (let index = calls.length - 1; index >= 0; index -= 1) {
    if (calls[index]?.outcome === 'failed') return calls[index]
  }
  return undefined
}

function MessageAuditReviewDetailsDialog(props: {
  review: MessageAuditReview
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const review = props.review

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('AI review details')}
      description={t(
        'Detailed findings, read coverage, uncovered sources, and safe call diagnostics for this review.'
      )}
      contentClassName='sm:max-w-4xl'
      contentHeight='min(72vh, 42rem)'
      bodyContainerClassName={messageAuditDetailScrollClassName}
      bodyClassName='space-y-5'
      showCloseButton
    >
      {review.result && (
        <section className='space-y-3'>
          <div>
            <h4 className='text-sm font-medium'>{t('Review overview')}</h4>
            <p className='text-muted-foreground mt-1 text-xs leading-5'>
              {review.result.summary}
            </p>
          </div>
          <MessageAuditReviewOverviewGrid review={review} />
          {review.result.findings.length > 0 && (
            <section className='space-y-2'>
              <h4 className='text-xs font-medium'>{t('Findings')}</h4>
              <ol className='divide-y border-y'>
                {review.result.findings.map((finding) => (
                  <li
                    key={`${finding.file_id}-${finding.start_sequence}-${finding.end_sequence}-${finding.category}-${finding.reason}`}
                    className='space-y-1 py-3'
                  >
                    <div className='flex flex-wrap items-center gap-2'>
                      <Badge
                        variant='outline'
                        className={getRiskBadgeClass(finding.severity)}
                      >
                        {t(getMessageAuditRiskLabelKey(finding.severity))}
                      </Badge>
                      <span className='text-xs font-medium'>
                        {t(
                          getMessageAuditReviewCategoryLabelKey(
                            finding.category
                          )
                        )}
                      </span>
                      <span className='text-muted-foreground font-mono text-xs'>
                        {finding.file_id} #{finding.start_sequence}-
                        {finding.end_sequence}
                      </span>
                    </div>
                    <p className='text-muted-foreground text-xs leading-5'>
                      {finding.reason}
                    </p>
                  </li>
                ))}
              </ol>
            </section>
          )}
          {review.result.coverage.length > 0 && (
            <section className='space-y-2'>
              <h4 className='text-xs font-medium'>{t('Read coverage')}</h4>
              <ol className='divide-y border-y'>
                {review.result.coverage.map((coverage) => (
                  <li
                    key={`${coverage.file_id}-${coverage.start_cursor ?? coverage.start_sequence}-${coverage.end_cursor ?? coverage.end_sequence}`}
                    className='space-y-2 py-3'
                  >
                    <div className='font-mono text-xs break-all'>
                      {coverage.file_id}
                    </div>
                    <dl className='grid gap-x-4 gap-y-1 text-xs sm:grid-cols-[auto_1fr]'>
                      <dt className='text-muted-foreground'>
                        {t('Message range')}
                      </dt>
                      <dd>
                        #{coverage.start_sequence}-#{coverage.end_sequence}
                      </dd>
                      {coverage.start_cursor !== undefined &&
                        coverage.end_cursor !== undefined && (
                          <>
                            <dt className='text-muted-foreground'>
                              {t('Virtual chunk range')}
                            </dt>
                            <dd>
                              {coverage.start_cursor}-{coverage.end_cursor}
                            </dd>
                          </>
                        )}
                      <dt className='text-muted-foreground'>
                        {t('Estimated tokens')}
                      </dt>
                      <dd>{coverage.estimated_tokens}</dd>
                    </dl>
                  </li>
                ))}
              </ol>
            </section>
          )}
          {review.result.uncovered.length > 0 && (
            <section className='space-y-2'>
              <h4 className='text-xs font-medium'>{t('Unreviewed sources')}</h4>
              <ul className='space-y-2'>
                {review.result.uncovered.map((source) => (
                  <li key={`${source.file_id}-${source.reason}`}>
                    <div className='font-mono text-xs break-all'>
                      {source.file_id}
                    </div>
                    <p className='text-muted-foreground text-xs'>
                      {t(getMessageAuditReviewUncoveredLabelKey(source.reason))}
                    </p>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </section>
      )}

      {review.diagnostics && (
        <section className='space-y-3 border-t pt-4 text-sm'>
          <h4 className='text-xs font-medium'>{t('Review diagnostics')}</h4>
          <dl className='grid gap-x-4 gap-y-1 text-xs sm:grid-cols-[auto_1fr_auto_1fr]'>
            <dt className='text-muted-foreground'>{t('Review channel')}</dt>
            <dd>#{review.diagnostics.channel_id}</dd>
            <dt className='text-muted-foreground'>{t('Review model')}</dt>
            <dd className='break-all'>{review.diagnostics.model}</dd>
            <dt className='text-muted-foreground'>{t('Model calls')}</dt>
            <dd>{review.diagnostics.model_calls}</dd>
            <dt className='text-muted-foreground'>{t('Tool calls')}</dt>
            <dd>
              {review.diagnostics.tool_calls} /{' '}
              {review.diagnostics.tool_call_limit}
            </dd>
            <dt className='text-muted-foreground'>{t('Tool tokens')}</dt>
            <dd>{review.diagnostics.tool_tokens}</dd>
            <dt className='text-muted-foreground'>{t('Duration')}</dt>
            <dd>{review.diagnostics.duration_ms} ms</dd>
            <dt className='text-muted-foreground'>{t('Text Tool fallback')}</dt>
            <dd>
              {review.diagnostics.text_tool_fallback
                ? t('Enabled')
                : t('Disabled')}
            </dd>
          </dl>
          {review.diagnostics.calls.length > 0 && (
            <ol className='divide-y border-t'>
              {review.diagnostics.calls.map((call) => (
                <li key={call.attempt} className='space-y-2 py-3'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='font-mono text-xs'>#{call.attempt}</span>
                    <Badge variant='outline'>
                      {t(
                        getMessageAuditReviewCallOutcomeLabelKey(call.outcome)
                      )}
                    </Badge>
                    <span className='text-muted-foreground text-xs'>
                      {t(getMessageAuditReviewCallPhaseLabelKey(call.phase))} ·{' '}
                      {t(getMessageAuditReviewProtocolLabelKey(call.protocol))}
                    </span>
                  </div>
                  <dl className='grid gap-x-4 gap-y-1 text-xs sm:grid-cols-[auto_1fr]'>
                    <dt className='text-muted-foreground'>{t('Duration')}</dt>
                    <dd>{call.duration_ms} ms</dd>
                    {call.http_status > 0 && (
                      <>
                        <dt className='text-muted-foreground'>HTTP</dt>
                        <dd>{call.http_status}</dd>
                      </>
                    )}
                    {call.error_stage && (
                      <>
                        <dt className='text-muted-foreground'>
                          {t('Failure stage')}
                        </dt>
                        <dd>
                          {t(
                            getMessageAuditReviewErrorStageLabelKey(
                              call.error_stage
                            )
                          )}{' '}
                          <code className='text-muted-foreground'>
                            {call.error_stage}
                          </code>
                        </dd>
                      </>
                    )}
                    {call.tool_names.length > 0 && (
                      <>
                        <dt className='text-muted-foreground'>
                          {t('Tools used')}
                        </dt>
                        <dd className='font-mono break-all'>
                          {call.tool_names.join(', ')}
                        </dd>
                      </>
                    )}
                  </dl>
                </li>
              ))}
            </ol>
          )}
        </section>
      )}
    </Dialog>
  )
}

function MessageAuditReviewSection(props: { auditSessionId: string }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [detailsOpen, setDetailsOpen] = useState(false)
  const reviewQuery = useQuery({
    queryKey: ['message-audit-review', props.auditSessionId],
    queryFn: () => getMessageAuditReview(props.auditSessionId),
    refetchInterval: (query) =>
      getMessageAuditReviewPollInterval(
        query.state.data as MessageAuditReview | undefined
      ),
  })
  const startReview = useMutation({
    mutationFn: () => startMessageAuditReview(props.auditSessionId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['message-audit-review', props.auditSessionId],
        }),
        queryClient.invalidateQueries({ queryKey: ['message-audits'] }),
      ])
    },
  })
  const review = reviewQuery.data
  const active =
    startReview.isPending ||
    review?.status === 'pending' ||
    review?.status === 'running'
  const hasDetails = Boolean(review?.result || review?.diagnostics)
  const lastFailedCall = getLastFailedReviewCall(review)

  return (
    <section className='space-y-3 border-b pb-5'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div>
          <div className='flex flex-wrap items-center gap-2'>
            <h3 className='text-sm font-medium'>{t('AI review')}</h3>
            {review && (
              <Badge variant='outline'>
                {t(getMessageAuditReviewStatusLabelKey(review.status))}
              </Badge>
            )}
            {review?.risk_level && (
              <Badge
                variant='outline'
                className={getRiskBadgeClass(review.risk_level)}
              >
                {t(getMessageAuditRiskLabelKey(review.risk_level))}
              </Badge>
            )}
            {review?.stale && (
              <Badge variant='outline'>{t('Content changed')}</Badge>
            )}
          </div>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t(
              'The AI reviews the stored inbound conversation context. Model responses are included only when the client submits them again as conversation history. The result is for administrator review and does not trigger automatic enforcement.'
            )}
          </p>
        </div>
        <Button
          type='button'
          size='sm'
          disabled={active || reviewQuery.isLoading}
          onClick={() => startReview.mutate()}
        >
          {active ? (
            <Loader2 className='animate-spin' aria-hidden='true' />
          ) : (
            <ShieldCheck aria-hidden='true' />
          )}
          {review?.result || review?.stale
            ? t('Review again')
            : t('Start AI review')}
        </Button>
      </div>

      {(reviewQuery.isError || startReview.isError) && (
        <Alert variant='destructive'>
          <AlertTitle>{t('AI review unavailable')}</AlertTitle>
          <AlertDescription>
            {getMessageAuditErrorMessage(
              startReview.error ?? reviewQuery.error,
              t('The AI review could not be started.')
            )}
          </AlertDescription>
        </Alert>
      )}

      {review?.status === 'failed' && (
        <Alert variant='destructive'>
          <AlertTitle>{t('AI review failed')}</AlertTitle>
          <AlertDescription>
            {t(getMessageAuditReviewFailureLabelKey(review.failure_code))}
            {review.failure_code && (
              <code className='mt-1 block text-xs'>{review.failure_code}</code>
            )}
            {lastFailedCall && (
              <div className='mt-2 space-y-1 text-xs'>
                <div>
                  {t(getMessageAuditReviewCallOutcomeLabelKey('failed'))}:{' '}
                  {t(getMessageAuditReviewCallPhaseLabelKey(lastFailedCall.phase))}
                  {' · '}
                  {t(
                    getMessageAuditReviewProtocolLabelKey(
                      lastFailedCall.protocol
                    )
                  )}
                </div>
                {lastFailedCall.error_stage && (
                  <div>
                    {t('Failure stage')}:{' '}
                    {t(
                      getMessageAuditReviewErrorStageLabelKey(
                        lastFailedCall.error_stage
                      )
                    )}{' '}
                    <code>{lastFailedCall.error_stage}</code>
                  </div>
                )}
                {lastFailedCall.http_status > 0 && (
                  <div>HTTP: {lastFailedCall.http_status}</div>
                )}
              </div>
            )}
          </AlertDescription>
        </Alert>
      )}

      {review?.diagnostics && (
        <dl className='bg-muted/30 grid gap-x-4 gap-y-1 rounded-md border px-3 py-2 text-xs sm:grid-cols-[auto_1fr_auto_1fr]'>
          <dt className='text-muted-foreground'>{t('Model calls')}</dt>
          <dd>{review.diagnostics.model_calls}</dd>
          <dt className='text-muted-foreground'>{t('Tool calls')}</dt>
          <dd>
            {review.diagnostics.tool_calls} /{' '}
            {review.diagnostics.tool_call_limit}
          </dd>
          <dt className='text-muted-foreground'>{t('Text Tool fallback')}</dt>
          <dd>
            {review.diagnostics.text_tool_fallback
              ? t('Enabled')
              : t('Disabled')}
          </dd>
          <dt className='text-muted-foreground'>{t('Duration')}</dt>
          <dd>{review.diagnostics.duration_ms} ms</dd>
        </dl>
      )}

      {review?.result && (
        <div className='space-y-3 text-sm'>
          <p className='leading-6'>{review.result.summary}</p>
          <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs'>
            <span>
              {t('Review model')}: {review.review_model}
            </span>
            {review.reviewed_at > 0 && (
              <span>
                {t('Reviewed at')}:{' '}
                {dayjs.unix(review.reviewed_at).format('YYYY-MM-DD HH:mm:ss')}
              </span>
            )}
            <span>
              {t('{{count}} source ranges reviewed', {
                count: review.result.coverage.length,
              })}
            </span>
          </div>
          <MessageAuditReviewOverviewGrid review={review} />
          {review.result.categories.length > 0 && (
            <div className='flex flex-wrap gap-2'>
              {review.result.categories.map((category) => (
                <Badge key={category} variant='outline'>
                  {t(getMessageAuditReviewCategoryLabelKey(category))}
                </Badge>
              ))}
            </div>
          )}
        </div>
      )}

      {hasDetails && review && (
        <div className='flex justify-end'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => setDetailsOpen(true)}
          >
            {t('View review details')}
          </Button>
          <MessageAuditReviewDetailsDialog
            review={review}
            open={detailsOpen}
            onOpenChange={setDetailsOpen}
          />
        </div>
      )}
    </section>
  )
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
        const renderedContent =
          typeof message.content === 'string'
            ? message.content
            : JSON.stringify(message.content, null, 2)
        const collapseContent =
          ['tools', 'functions'].includes(message.content_type) ||
          renderedContent.length > 1600
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
                  <pre
                    className={`bg-muted/50 mt-2 max-h-96 overflow-auto rounded-md border p-3 text-xs leading-5 break-words whitespace-pre-wrap ${messageAuditDetailScrollClassName}`}
                  >
                    {renderedContent}
                  </pre>
                </details>
              ) : (
                <pre
                  className={`bg-muted/50 max-h-96 overflow-auto rounded-md border p-3 text-xs leading-5 break-words whitespace-pre-wrap ${messageAuditDetailScrollClassName}`}
                >
                  {renderedContent}
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
        <DropdownMenuGroup>
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
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function DetailBody(props: {
  requestId: string
  onSelectRequest: (requestId: string) => void
}) {
  const { t } = useTranslation()
  const [copiedSequence, setCopiedSequence] = useState<number | null>(null)
  const [hiddenRoles, setHiddenRoles] = useState<string[]>([])
  const [hiddenContentTypes, setHiddenContentTypes] = useState<string[]>([])
  const [sessionPage, setSessionPage] = useState(1)
  const sessionPageSize = 50
  const detailQuery = useQuery({
    queryKey: ['message-audit-detail', props.requestId],
    queryFn: () => getMessageAuditDetail(props.requestId),
  })
  const detail = detailQuery.data
  const auditSessionId = detail?.request.audit_session_id
  const sessionQuery = useQuery({
    queryKey: [
      'message-audit-detail-session',
      auditSessionId,
      sessionPage,
      sessionPageSize,
    ],
    queryFn: () => {
      if (!auditSessionId) {
        throw new Error(t('Inferred session ID is required'))
      }
      return getMessageAuditSessionRequests(
        auditSessionId,
        sessionPage,
        sessionPageSize
      )
    },
    enabled: Boolean(auditSessionId),
  })
  const sessionRequests = useMemo(() => {
    const requests = sessionQuery.data?.items ?? []
    if (
      !detail ||
      requests.some((item) => item.request_id === props.requestId)
    ) {
      return requests
    }
    return [...requests, detail.request]
  }, [detail, props.requestId, sessionQuery.data?.items])
  const sessionPageCount = Math.max(
    1,
    Math.ceil((sessionQuery.data?.total ?? 0) / sessionPageSize)
  )
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
        <dd className='min-w-0 font-mono text-xs break-all'>
          {detail.request.audit_session_id}
        </dd>
        <dt className='text-muted-foreground'>
          {t('Inferred session history')}
        </dt>
        <dd className='flex min-w-0 flex-wrap items-center gap-2'>
          <div className='min-w-0 flex-1'>
            <Select
              value={detail.request.request_id}
              onValueChange={(requestId) => {
                if (requestId && requestId !== detail.request.request_id) {
                  props.onSelectRequest(requestId)
                }
              }}
              disabled={
                !detail.request.audit_session_id || sessionQuery.isLoading
              }
            >
              <SelectTrigger className='w-full min-w-0'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent
                align='start'
                alignItemWithTrigger={false}
                className='max-w-[calc(100vw-2rem)] min-w-80'
              >
                <SelectGroup>
                  {sessionRequests.map((request) => (
                    <SelectItem
                      key={request.request_id}
                      value={request.request_id}
                      className={
                        request.session_match === 'compressed'
                          ? 'text-amber-700 dark:text-amber-300'
                          : undefined
                      }
                    >
                      {request.session_match === 'compressed' && (
                        <Minimize2 aria-hidden='true' />
                      )}
                      {dayjs.unix(request.captured_at).format('MM-DD HH:mm:ss')}{' '}
                      ·{' '}
                      {t(
                        getMessageAuditSessionMatchLabelKey(
                          request.session_match
                        )
                      )}{' '}
                      · {request.message_count} {t('Messages')}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          {sessionPageCount > 1 && (
            <div className='flex shrink-0 items-center gap-1'>
              <Button
                type='button'
                variant='outline'
                size='icon-sm'
                title={t('Previous page')}
                aria-label={t('Previous page')}
                disabled={sessionPage <= 1 || sessionQuery.isFetching}
                onClick={() =>
                  setSessionPage((value) => Math.max(1, value - 1))
                }
              >
                <ChevronLeft aria-hidden='true' />
              </Button>
              <span className='min-w-12 text-center text-xs'>
                {sessionPage}/{sessionPageCount}
              </span>
              <Button
                type='button'
                variant='outline'
                size='icon-sm'
                title={t('Next page')}
                aria-label={t('Next page')}
                disabled={
                  sessionPage >= sessionPageCount || sessionQuery.isFetching
                }
                onClick={() =>
                  setSessionPage((value) =>
                    Math.min(sessionPageCount, value + 1)
                  )
                }
              >
                <ChevronRight aria-hidden='true' />
              </Button>
            </div>
          )}
          {sessionQuery.isError && (
            <span className='text-destructive basis-full text-xs'>
              {t('Failed to load session history')}
            </span>
          )}
        </dd>
        <dt className='text-muted-foreground'>{t('Session continuation')}</dt>
        <dd>
          <Badge
            variant='outline'
            className={
              detail.request.session_match === 'compressed'
                ? 'border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-300'
                : undefined
            }
          >
            {detail.request.session_match === 'compressed' && (
              <Minimize2 aria-hidden='true' />
            )}
            {t(
              getMessageAuditSessionMatchLabelKey(detail.request.session_match)
            )}
          </Badge>
        </dd>
        <dt className='text-muted-foreground'>{t('Session compression')}</dt>
        <dd>
          {detail.request.compressed_request_count > 0 ? (
            <Badge
              variant='outline'
              className='border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-300'
            >
              <Minimize2 aria-hidden='true' />
              {t('Compressed continuation')} ·{' '}
              {detail.request.compressed_request_count}
            </Badge>
          ) : (
            <span className='text-muted-foreground'>{t('None')}</span>
          )}
        </dd>
        <dt className='text-muted-foreground'>{t('Audit storage')}</dt>
        <dd>
          <Badge variant='outline'>
            {t(getMessageAuditStorageModeLabelKey(detail.request.audit_status))}
          </Badge>
        </dd>
        <dt className='text-muted-foreground'>{t('Body size')}</dt>
        <dd className='flex flex-wrap gap-x-4 gap-y-1'>
          <span>
            {t('Original body')}:{' '}
            {formatAuditBytes(detail.request.plaintext_bytes)}
          </span>
          <span>
            {t('Captured body')}:{' '}
            {formatAuditBytes(detail.request.captured_plaintext_bytes)}
          </span>
          <span>
            {t('Stored')}:{' '}
            {formatAuditBytes(detail.request.stored_payload_bytes)}
          </span>
        </dd>
        {detail.request.status === 'failed' && (
          <>
            <dt className='text-muted-foreground'>{t('Failure reason')}</dt>
            <dd className='space-y-1'>
              <div>
                {t('HTTP status')}: {detail.request.http_status || t('Unknown')}
              </div>
              {detail.request.error_code && (
                <>
                  <div>
                    {t('Explanation')}:{' '}
                    {t(
                      getMessageAuditRequestFailureLabelKey(
                        detail.request.error_code
                      )
                    )}
                  </div>
                  <div className='font-mono text-xs break-all'>
                    {t('Error code')}: {detail.request.error_code}
                  </div>
                </>
              )}
              {detail.request.finish_reason && (
                <div>
                  {t('Finish reason')}: {detail.request.finish_reason}
                </div>
              )}
            </dd>
          </>
        )}
      </dl>

      <MessageAuditReviewSection
        auditSessionId={detail.request.audit_session_id}
      />

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
          <div
            className={`min-h-0 flex-1 overflow-y-auto px-4 pr-3 ${messageAuditDetailScrollClassName}`}
          >
            {props.requestId && (
              <DetailBody
                key={props.requestId}
                requestId={props.requestId}
                onSelectRequest={props.onSelectRequest}
              />
            )}
          </div>
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Sheet open={open} onOpenChange={props.onOpenChange}>
      <SheetContent
        side='right'
        className='w-full overflow-hidden sm:max-w-2xl'
      >
        <SheetHeader>
          <SheetTitle>{t('Message audit detail')}</SheetTitle>
          <SheetDescription className='font-mono text-xs break-all'>
            {props.requestId}
          </SheetDescription>
        </SheetHeader>
        <div
          className={`min-h-0 flex-1 overflow-y-auto px-4 pr-3 ${messageAuditDetailScrollClassName}`}
        >
          {props.requestId && (
            <DetailBody
              key={props.requestId}
              requestId={props.requestId}
              onSelectRequest={props.onSelectRequest}
            />
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
