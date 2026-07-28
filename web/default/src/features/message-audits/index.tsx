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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'
import {
  AlertTriangle,
  History,
  Minimize2,
  RefreshCw,
  Settings,
  Trash2,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import dayjs from '@/lib/dayjs'

import {
  getCurrentMessageAuditCleanupTask,
  getMessageAuditCleanupTask,
  getMessageAudits,
  getMessageAuditStatus,
  startMessageAuditCleanup,
} from './api'
import { MessageAuditDetailPanel } from './components/message-audit-detail'
import { MessageAuditSessionDialog } from './components/message-audit-session-dialog'
import {
  getMessageAuditCleanupProgress,
  getMessageAuditCleanupTitleKey,
  getMessageAuditErrorMessage,
  getMessageAuditRequestFailureLabelKey,
  getMessageAuditReviewStatusLabelKey,
  getMessageAuditRiskLabelKey,
  getMessageAuditStorageModeLabelKey,
  isMessageAuditCleanupActive,
  isMessageAuditClearConfirmed,
  MESSAGE_AUDIT_CLEAR_CONFIRMATION,
} from './lib/message-audit-ui'
import type {
  MessageAuditCleanupTask,
  MessageAuditRequest,
  MessageAuditSearch,
} from './types'

const route = getRouteApi('/_authenticated/message-audits/')

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const index = Math.min(
    units.length - 1,
    Math.floor(Math.log(bytes) / Math.log(1024))
  )
  return `${(bytes / Math.pow(1024, index)).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function QueryErrorAlert(props: {
  error: unknown
  fallback: string
  onRetry: () => void
}) {
  const { t } = useTranslation()
  return (
    <Alert variant='destructive'>
      <AlertTriangle aria-hidden='true' />
      <AlertTitle>{t('Failed to load')}</AlertTitle>
      <AlertDescription>
        {getMessageAuditErrorMessage(props.error, props.fallback)}
      </AlertDescription>
      <AlertAction>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={props.onRetry}
        >
          {t('Retry')}
        </Button>
      </AlertAction>
    </Alert>
  )
}

export function MessageAudits() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const [selectedRequestId, setSelectedRequestId] = useState<string | null>(
    null
  )
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(
    null
  )
  const [clearOpen, setClearOpen] = useState(false)
  const [clearConfirmation, setClearConfirmation] = useState('')
  const [isStartingCleanup, setIsStartingCleanup] = useState(false)
  const [cleanupTask, setCleanupTask] =
    useState<MessageAuditCleanupTask | null>(null)
  const [filters, setFilters] = useState({
    username: search.username ?? '',
    token: search.token ?? '',
    model: search.model ?? '',
    requestId: search.requestId ?? '',
    path: search.path ?? '',
    status: search.status ?? '',
    startTime: search.startTime
      ? dayjs.unix(search.startTime).format('YYYY-MM-DDTHH:mm')
      : '',
    endTime: search.endTime
      ? dayjs.unix(search.endTime).format('YYYY-MM-DDTHH:mm')
      : '',
  })

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: { defaultPage: 1, defaultPageSize: isMobile ? 20 : 50 },
      globalFilter: { enabled: false },
      columnFilters: [],
    })

  const auditQuery = useQuery({
    queryKey: ['message-audits', search, pagination],
    queryFn: () =>
      getMessageAudits({
        ...search,
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
      }),
    placeholderData: (previousData) => previousData,
  })
  const statusQuery = useQuery({
    queryKey: ['message-audit-status'],
    queryFn: getMessageAuditStatus,
  })
  const currentCleanupQuery = useQuery({
    queryKey: ['message-audit-cleanup', 'current'],
    queryFn: getCurrentMessageAuditCleanupTask,
  })

  useEffect(() => {
    if (currentCleanupQuery.data) {
      setCleanupTask(currentCleanupQuery.data)
    }
  }, [currentCleanupQuery.data])

  const cleanupTaskId = cleanupTask?.task_id
  const cleanupTaskQuery = useQuery({
    queryKey: ['message-audit-cleanup', cleanupTaskId],
    queryFn: () => {
      if (!cleanupTaskId) {
        throw new Error(t('Failed to clear message audits'))
      }
      return getMessageAuditCleanupTask(cleanupTaskId)
    },
    enabled: Boolean(cleanupTaskId && isMessageAuditCleanupActive(cleanupTask)),
    refetchInterval: isMessageAuditCleanupActive(cleanupTask) ? 1000 : false,
  })

  useEffect(() => {
    const task = cleanupTaskQuery.data
    if (!task) return
    setCleanupTask(task)
    if (!isMessageAuditCleanupActive(task)) {
      void queryClient.invalidateQueries({ queryKey: ['message-audits'] })
      void queryClient.invalidateQueries({ queryKey: ['message-audit-status'] })
      void queryClient.invalidateQueries({
        queryKey: ['message-audit-cleanup', 'current'],
      })
    }
  }, [cleanupTaskQuery.data, queryClient])

  const columns = useMemo<ColumnDef<MessageAuditRequest, unknown>[]>(
    () => [
      {
        accessorKey: 'captured_at',
        header: t('Time'),
        size: 150,
        cell: ({ row }) =>
          dayjs.unix(row.original.captured_at).format('MM-DD HH:mm:ss'),
      },
      {
        accessorKey: 'username',
        header: t('User'),
        cell: ({ row }) => row.original.username || `#${row.original.user_id}`,
      },
      {
        accessorKey: 'token_name',
        header: t('Token'),
        cell: ({ row }) =>
          row.original.token_name || `#${row.original.token_id}`,
      },
      { accessorKey: 'model_name', header: t('Model'), size: 170 },
      { accessorKey: 'request_path', header: t('Request path'), size: 180 },
      {
        id: 'audit_session_id',
        header: t('Inferred session'),
        size: 130,
        cell: ({ row }) => (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='max-w-full'
            title={row.original.audit_session_id}
            disabled={!row.original.audit_session_id}
            onClick={(event) => {
              event.stopPropagation()
              setSelectedSessionId(row.original.audit_session_id)
            }}
          >
            <History aria-hidden='true' />
            <span className='truncate'>
              {t('{{count}} requests', {
                count: row.original.session_request_count || 1,
              })}
            </span>
          </Button>
        ),
      },
      {
        accessorKey: 'message_count',
        header: t('Messages'),
        size: 80,
      },
      {
        accessorKey: 'tool_count',
        header: t('Tools'),
        size: 70,
      },
      {
        accessorKey: 'plaintext_bytes',
        header: t('Body size'),
        cell: ({ row }) => (
          <div className='space-y-0.5'>
            <div>{formatBytes(row.original.plaintext_bytes)}</div>
            {row.original.stored_payload_bytes !== null && (
              <div className='text-muted-foreground text-xs'>
                {t('Stored')}: {formatBytes(row.original.stored_payload_bytes)}
              </div>
            )}
            <Badge variant='outline' className='max-w-32'>
              <span className='truncate'>
                {t(
                  getMessageAuditStorageModeLabelKey(row.original.audit_status)
                )}
              </span>
            </Badge>
          </div>
        ),
      },
      {
        accessorKey: 'compressed_request_count',
        header: t('Context compression'),
        cell: ({ row }) =>
          row.original.compressed_request_count > 0 ? (
            <Badge
              variant='outline'
              className='border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-300'
            >
              <Minimize2 aria-hidden='true' />
              {t('Compressed continuation')} ·{' '}
              {row.original.compressed_request_count}
            </Badge>
          ) : (
            <span className='text-muted-foreground'>{t('None')}</span>
          ),
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        cell: ({ row }) => {
          const request = row.original
          return (
            <div className='space-y-1'>
              <Badge
                variant={
                  request.status === 'failed' ? 'destructive' : 'outline'
                }
              >
                {t(request.status || 'pending')}
              </Badge>
              {request.status === 'failed' && (
                <div className='max-w-56 space-y-0.5 text-xs'>
                  <div
                    className='text-destructive truncate'
                    title={t(
                      getMessageAuditRequestFailureLabelKey(request.error_code)
                    )}
                  >
                    {request.error_code
                      ? t(
                          getMessageAuditRequestFailureLabelKey(
                            request.error_code
                          )
                        )
                      : request.finish_reason || t('Unknown')}
                  </div>
                  <div className='text-muted-foreground truncate font-mono'>
                    HTTP {request.http_status || '?'}
                    {(request.error_code || request.finish_reason) &&
                      ` · ${request.error_code || request.finish_reason}`}
                  </div>
                </div>
              )}
            </div>
          )
        },
      },
      {
        id: 'review',
        header: t('AI review'),
        size: 150,
        cell: ({ row }) => (
          <div className='flex flex-wrap gap-1'>
            <Badge variant='outline'>
              {t(
                getMessageAuditReviewStatusLabelKey(
                  row.original.review_status || 'unreviewed'
                )
              )}
            </Badge>
            {row.original.review_risk_level && (
              <Badge variant='outline'>
                {t(getMessageAuditRiskLabelKey(row.original.review_risk_level))}
              </Badge>
            )}
            {row.original.review_stale && (
              <Badge variant='outline'>{t('Content changed')}</Badge>
            )}
          </div>
        ),
      },
      { accessorKey: 'request_id', header: t('Request ID'), size: 210 },
    ],
    [t]
  )

  const data = auditQuery.data
  const { table } = useDataTable({
    data: data?.items ?? [],
    columns,
    pagination,
    onPaginationChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total ?? 0,
    ensurePageInRange,
    enableRowSelection: false,
  })

  const applyFilters = () => {
    void navigate({
      search: (previous: MessageAuditSearch) => ({
        ...previous,
        ...filters,
        status: filters.status || undefined,
        startTime: filters.startTime
          ? dayjs(filters.startTime).unix()
          : undefined,
        endTime: filters.endTime ? dayjs(filters.endTime).unix() : undefined,
        page: 1,
      }),
    })
  }

  const resetFilters = () => {
    const emptyFilters = {
      username: '',
      token: '',
      model: '',
      requestId: '',
      path: '',
      status: '',
      startTime: '',
      endTime: '',
    }
    setFilters(emptyFilters)
    void navigate({ search: { page: 1, pageSize: search.pageSize } })
  }

  const clearAudits = async () => {
    setIsStartingCleanup(true)
    try {
      const result = await startMessageAuditCleanup()
      setCleanupTask(result.task)
      setClearOpen(false)
      setClearConfirmation('')
      toast.success(t('Message audit cleanup task started.'))
    } catch (error) {
      toast.error(
        getMessageAuditErrorMessage(error, t('Failed to clear message audits'))
      )
    } finally {
      setIsStartingCleanup(false)
    }
  }

  const status = statusQuery.data
  const cleanupProgress = getMessageAuditCleanupProgress(cleanupTask)
  const cleanupActive = isMessageAuditCleanupActive(cleanupTask)

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('Message Audits')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => {
              void auditQuery.refetch()
              void statusQuery.refetch()
              void currentCleanupQuery.refetch()
            }}
          >
            <RefreshCw aria-hidden='true' />
            {t('Refresh')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            render={
              <Link
                to='/system-settings/operations/$section'
                params={{ section: 'logs' }}
              />
            }
          >
            <Settings aria-hidden='true' />
            {t('Settings')}
          </Button>
          <Button
            type='button'
            variant='destructive'
            size='sm'
            disabled={
              cleanupActive ||
              isStartingCleanup ||
              currentCleanupQuery.isLoading ||
              currentCleanupQuery.isError
            }
            onClick={() => setClearOpen(true)}
          >
            <Trash2 aria-hidden='true' />
            {t('Clear audits')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 flex-col gap-3'>
            {statusQuery.isLoading && (
              <Skeleton className='h-16 w-full rounded-none' />
            )}
            {!statusQuery.isLoading && (statusQuery.isError || !status) && (
              <QueryErrorAlert
                error={statusQuery.error}
                fallback={t('Failed to load audit status')}
                onRetry={() => void statusQuery.refetch()}
              />
            )}
            {!statusQuery.isLoading && !statusQuery.isError && status && (
              <div className='bg-muted/30 grid gap-x-6 gap-y-2 border px-3 py-2 text-xs sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-7'>
                <span>
                  {t('Capture')}:{' '}
                  <strong>
                    {status.enabled ? t('Enabled') : t('Disabled')}
                  </strong>
                </span>
                <span>
                  {t('Encryption key')}:{' '}
                  <strong>
                    {status.key_configured ? t('Ready') : t('Missing')}
                  </strong>
                </span>
                <span>
                  {t('Retention')}:{' '}
                  <strong>
                    {status.retention_days} {t('days')}
                  </strong>
                </span>
                <span>
                  {t('Queue')}:{' '}
                  <strong>
                    {status.queue_depth}/{status.queue_capacity}
                  </strong>
                </span>
                <span>
                  {t('Written')}: <strong>{status.succeeded}</strong>
                </span>
                <span>
                  {t('Retries')}: <strong>{status.retries}</strong>
                </span>
                <span>
                  {t('Failed')}: <strong>{status.failed}</strong>
                </span>
                <span>
                  {t('Dropped')}: <strong>{status.dropped}</strong>
                </span>
                <span>
                  {t('Encrypted payload')}:{' '}
                  <strong>{formatBytes(status.payload_bytes ?? 0)}</strong>
                </span>
                <span>
                  {t('Allocated storage')}:{' '}
                  <strong>{formatBytes(status.storage_bytes ?? 0)}</strong>
                  {status.storage_estimated && (
                    <span className='text-muted-foreground ml-1'>
                      ({t('Estimated')})
                    </span>
                  )}
                </span>
                <span>
                  {t('Audited requests')}:{' '}
                  <strong>{status.request_count ?? 0}</strong>
                </span>
                <span>
                  {t('Unique message blocks')}:{' '}
                  <strong>{status.blob_count ?? 0}</strong>
                </span>
                <span>
                  {t('Message references')}:{' '}
                  <strong>{status.item_count ?? 0}</strong>
                </span>
              </div>
            )}

            {currentCleanupQuery.isError && !cleanupTask && (
              <QueryErrorAlert
                error={currentCleanupQuery.error}
                fallback={t('Failed to load cleanup task')}
                onRetry={() => void currentCleanupQuery.refetch()}
              />
            )}

            {cleanupTask && (
              <div className='border px-3 py-2'>
                <div className='flex items-center justify-between gap-3 text-xs'>
                  <span>{t(getMessageAuditCleanupTitleKey(cleanupTask))}</span>
                  <Badge
                    variant={
                      cleanupTask.status === 'failed'
                        ? 'destructive'
                        : 'outline'
                    }
                  >
                    {t(cleanupTask.status)}
                  </Badge>
                </div>
                {cleanupActive && (
                  <Progress className='mt-2' value={cleanupProgress} />
                )}
                <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs'>
                  <span>
                    {t(
                      '{{processed}} of {{total}} audited requests processed.',
                      {
                        processed: cleanupTask.state?.processed ?? 0,
                        total: cleanupTask.state?.total ?? 0,
                      }
                    )}
                  </span>
                  {cleanupTask.status === 'succeeded' && (
                    <>
                      <span>
                        {t('{{count}} audited requests removed.', {
                          count: cleanupTask.result?.deleted_requests ?? 0,
                        })}
                      </span>
                      <span>
                        {t('{{count}} message blocks removed.', {
                          count: cleanupTask.result?.deleted_blobs ?? 0,
                        })}
                      </span>
                      <span className='basis-full'>
                        {t(
                          'Audit remains enabled. Requests received around the clear operation may also be removed. Database allocated space is reusable and may not shrink after records are deleted.'
                        )}
                      </span>
                    </>
                  )}
                  {cleanupTask.status === 'failed' && cleanupTask.error && (
                    <span className='text-destructive'>
                      {cleanupTask.error}
                    </span>
                  )}
                </div>
              </div>
            )}

            {cleanupTaskQuery.isError && (
              <QueryErrorAlert
                error={cleanupTaskQuery.error}
                fallback={t('Failed to load cleanup task')}
                onRetry={() => void cleanupTaskQuery.refetch()}
              />
            )}

            <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-8'>
              <Input
                type='datetime-local'
                aria-label={t('Start time')}
                value={filters.startTime}
                onChange={(event) =>
                  setFilters((value) => ({
                    ...value,
                    startTime: event.target.value,
                  }))
                }
              />
              <Input
                type='datetime-local'
                aria-label={t('End time')}
                value={filters.endTime}
                onChange={(event) =>
                  setFilters((value) => ({
                    ...value,
                    endTime: event.target.value,
                  }))
                }
              />
              <Input
                placeholder={t('Username')}
                value={filters.username}
                onChange={(event) =>
                  setFilters((value) => ({
                    ...value,
                    username: event.target.value,
                  }))
                }
              />
              <Input
                placeholder={t('Token name')}
                value={filters.token}
                onChange={(event) =>
                  setFilters((value) => ({
                    ...value,
                    token: event.target.value,
                  }))
                }
              />
              <Input
                placeholder={t('Model')}
                value={filters.model}
                onChange={(event) =>
                  setFilters((value) => ({
                    ...value,
                    model: event.target.value,
                  }))
                }
              />
              <Input
                placeholder={t('Request ID')}
                value={filters.requestId}
                onChange={(event) =>
                  setFilters((value) => ({
                    ...value,
                    requestId: event.target.value,
                  }))
                }
              />
              <Input
                placeholder={t('Request path')}
                value={filters.path}
                onChange={(event) =>
                  setFilters((value) => ({
                    ...value,
                    path: event.target.value,
                  }))
                }
              />
              <Select
                value={filters.status || 'all'}
                onValueChange={(value) =>
                  setFilters((current) => ({
                    ...current,
                    status: value === 'all' || value === null ? '' : value,
                  }))
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder={t('Status')} />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value='all'>{t('All statuses')}</SelectItem>
                    <SelectItem value='pending'>{t('pending')}</SelectItem>
                    <SelectItem value='succeeded'>{t('succeeded')}</SelectItem>
                    <SelectItem value='failed'>{t('failed')}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <div className='flex gap-2 sm:col-span-2 lg:col-span-4 xl:col-span-8'>
                <Button type='button' size='sm' onClick={applyFilters}>
                  {t('Apply filters')}
                </Button>
                <Button
                  type='button'
                  size='sm'
                  variant='ghost'
                  onClick={resetFilters}
                >
                  {t('Reset')}
                </Button>
              </div>
            </div>

            <div className='min-h-0 flex-1'>
              {auditQuery.isError ? (
                <ErrorState
                  className='h-full min-h-48 border'
                  title={t('Failed to load')}
                  description={getMessageAuditErrorMessage(
                    auditQuery.error,
                    t('Failed to load message audits')
                  )}
                  onRetry={() => void auditQuery.refetch()}
                />
              ) : (
                <DataTablePage
                  table={table}
                  columns={columns}
                  isLoading={auditQuery.isLoading}
                  isFetching={auditQuery.isFetching}
                  emptyTitle={t('No message audits found')}
                  emptyDescription={t(
                    'Captured inbound AI messages will appear here when auditing is enabled.'
                  )}
                  toolbarProps={null}
                  applyHeaderSize
                  renderRow={(row) => (
                    <DataTableRow
                      key={row.id}
                      row={row}
                      className='cursor-pointer'
                      onClick={() =>
                        setSelectedRequestId(row.original.request_id)
                      }
                    />
                  )}
                  mobile={
                    <div className='divide-y border-y'>
                      {table.getRowModel().rows.map((row) => (
                        <div key={row.id} className='px-1 py-3'>
                          <button
                            type='button'
                            className='hover:bg-muted/40 block w-full text-left'
                            onClick={() =>
                              setSelectedRequestId(row.original.request_id)
                            }
                          >
                            <div className='flex items-center justify-between gap-3 text-sm'>
                              <strong className='truncate'>
                                {row.original.model_name}
                              </strong>
                              <Badge
                                variant={
                                  row.original.status === 'failed'
                                    ? 'destructive'
                                    : 'outline'
                                }
                              >
                                {t(row.original.status)}
                              </Badge>
                            </div>
                            {row.original.compressed_request_count > 0 && (
                              <Badge
                                variant='outline'
                                className='mt-2 border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-300'
                              >
                                <Minimize2 aria-hidden='true' />
                                {t('Compressed continuation')} ·{' '}
                                {row.original.compressed_request_count}
                              </Badge>
                            )}
                            <div className='mt-2 flex flex-wrap gap-1'>
                              <Badge variant='outline'>
                                {t(
                                  getMessageAuditReviewStatusLabelKey(
                                    row.original.review_status || 'unreviewed'
                                  )
                                )}
                              </Badge>
                              {row.original.review_risk_level && (
                                <Badge variant='outline'>
                                  {t(
                                    getMessageAuditRiskLabelKey(
                                      row.original.review_risk_level
                                    )
                                  )}
                                </Badge>
                              )}
                              {row.original.review_stale && (
                                <Badge variant='outline'>
                                  {t('Content changed')}
                                </Badge>
                              )}
                            </div>
                            {row.original.status === 'failed' && (
                              <div className='mt-1 space-y-0.5 text-xs'>
                                <div className='text-destructive truncate'>
                                  {row.original.error_code
                                    ? t(
                                        getMessageAuditRequestFailureLabelKey(
                                          row.original.error_code
                                        )
                                      )
                                    : row.original.finish_reason ||
                                      t('Unknown')}
                                </div>
                                <div className='text-muted-foreground truncate font-mono'>
                                  HTTP {row.original.http_status || '?'}
                                  {(row.original.error_code ||
                                    row.original.finish_reason) &&
                                    ` · ${row.original.error_code || row.original.finish_reason}`}
                                </div>
                              </div>
                            )}
                            <div className='text-muted-foreground mt-1 truncate font-mono text-xs'>
                              {row.original.request_id}
                            </div>
                            <div className='text-muted-foreground mt-1 flex justify-between gap-3 text-xs'>
                              <span>
                                {row.original.username ||
                                  `#${row.original.user_id}`}
                              </span>
                              <span>
                                {dayjs
                                  .unix(row.original.captured_at)
                                  .format('MM-DD HH:mm')}
                              </span>
                            </div>
                          </button>
                          <Button
                            type='button'
                            variant='ghost'
                            size='sm'
                            className='mt-2 max-w-full px-0'
                            title={row.original.audit_session_id}
                            disabled={!row.original.audit_session_id}
                            onClick={() => {
                              setSelectedSessionId(
                                row.original.audit_session_id
                              )
                            }}
                          >
                            <History aria-hidden='true' />
                            <span className='truncate'>
                              {t('{{count}} requests', {
                                count: row.original.session_request_count || 1,
                              })}
                            </span>
                          </Button>
                        </div>
                      ))}
                    </div>
                  }
                />
              )}
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <MessageAuditDetailPanel
        requestId={selectedRequestId}
        onOpenChange={(open) => !open && setSelectedRequestId(null)}
        onSelectRequest={setSelectedRequestId}
      />

      <MessageAuditSessionDialog
        sessionId={selectedSessionId}
        onOpenChange={(open) => !open && setSelectedSessionId(null)}
        onSelectRequest={setSelectedRequestId}
      />

      <AlertDialog open={clearOpen} onOpenChange={setClearOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Clear all message audits?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Existing audited messages will be permanently deleted. Requests received around the clear operation may also be removed.'
              )}{' '}
              {t(
                'Database allocated space is reusable and may not shrink after records are deleted.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className='space-y-2'>
            <label
              className='text-sm font-medium'
              htmlFor='message-audit-clear-confirmation'
            >
              {t('Type {{value}} to confirm', {
                value: MESSAGE_AUDIT_CLEAR_CONFIRMATION,
              })}
            </label>
            <Input
              id='message-audit-clear-confirmation'
              value={clearConfirmation}
              onChange={(event) => setClearConfirmation(event.target.value)}
              autoComplete='off'
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={
                !isMessageAuditClearConfirmed(clearConfirmation) ||
                isStartingCleanup
              }
              onClick={clearAudits}
            >
              {t('Clear audits')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
