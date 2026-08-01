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
import { getRouteApi } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'
import { ExternalLink, Loader2, ShieldOff } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { StatusBadge, StatusBadgeList } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
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
import { Button } from '@/components/ui/button'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { formatTimestampToDate } from '@/lib/format'

import { disableTokenLeakFinding, getTokenLeakFindings } from '../api'
import {
  canSubmitTokenLeakDisable,
  selectLatestTokenLeakNotificationsByChannel,
} from '../lib/token-leak-scan'
import type { TokenLeakFinding } from '../types'

const route = getRouteApi('/_authenticated/security-alerts/token-leaks/')

function getNotificationChannelLabel(t: TFunction, channel: string): string {
  switch (channel) {
    case 'root':
      return t('Root')
    case 'user':
      return t('User')
    case 'dingtalk':
      return t('DingTalk')
    default:
      return channel
  }
}

function getNotificationVariant(
  status: string
): 'success' | 'danger' | 'warning' | 'neutral' {
  switch (status) {
    case 'succeeded':
      return 'success'
    case 'failed':
      return 'danger'
    case 'pending':
      return 'warning'
    default:
      return 'neutral'
  }
}

function getNotificationStatusLabel(t: TFunction, status: string): string {
  switch (status) {
    case 'succeeded':
      return t('Success')
    case 'failed':
      return t('Failed')
    case 'pending':
      return t('Pending')
    default:
      return status
  }
}

/**
 * 渲染公开泄露位置、通知审计和禁用处置操作。
 *
 * @returns 支持 URL 分页与状态筛选的泄露结果表格。
 */
export function TokenLeakFindingsTable() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const [disableTarget, setDisableTarget] = useState<TokenLeakFinding | null>(
    null
  )
  const {
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search,
    navigate,
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 10 : 20 },
    globalFilter: { enabled: false },
    columnFilters: [{ columnId: 'status', searchKey: 'status', type: 'array' }],
  })
  const statusFilter =
    (columnFilters.find((filter) => filter.id === 'status')?.value as
      | string[]
      | undefined) ?? []
  const activeStatus = statusFilter[0]
  const findingsQuery = useQuery({
    queryKey: [
      'token-leak-scan',
      'findings',
      pagination.pageIndex,
      pagination.pageSize,
      activeStatus,
    ],
    queryFn: () =>
      getTokenLeakFindings(
        pagination.pageIndex + 1,
        pagination.pageSize,
        activeStatus
      ),
    placeholderData: (previous) => previous,
  })
  const disableMutation = useMutation({
    mutationFn: (findingId: number) => disableTokenLeakFinding(findingId),
    onSuccess: async () => {
      setDisableTarget(null)
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['token-leak-scan', 'findings'],
        }),
        queryClient.invalidateQueries({
          queryKey: ['token-leak-scan', 'status'],
        }),
      ])
      toast.success(t('Token disabled.'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to disable token.'))
    },
  })

  const columns = useMemo<ColumnDef<TokenLeakFinding, unknown>[]>(
    () => [
      {
        accessorKey: 'token_id',
        header: t('Token'),
        size: 120,
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <div className='space-y-0.5'>
            <div className='flex items-center gap-1.5'>
              <span className='max-w-28 truncate text-sm font-medium'>
                {row.original.token_name || t('Token')}
              </span>
              <TableId value={row.original.token_id} />
            </div>
            <div className='text-muted-foreground text-xs'>
              {t('User')} #{row.original.user_id}
            </div>
          </div>
        ),
      },
      {
        id: 'location',
        header: t('Repository / file'),
        minSize: 280,
        cell: ({ row }) => (
          <a
            href={row.original.html_url}
            target='_blank'
            rel='noreferrer'
            className='group/link block min-w-0 hover:underline'
            title={`${row.original.repository_name}/${row.original.file_path}`}
          >
            <span className='inline-flex max-w-full items-center gap-1.5 font-medium'>
              <span className='truncate'>{row.original.repository_name}</span>
              <ExternalLink className='size-3.5 shrink-0' aria-hidden='true' />
            </span>
            <span className='text-muted-foreground block truncate text-xs'>
              {row.original.file_path}
            </span>
          </a>
        ),
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        size: 120,
        meta: { mobileBadge: true },
        cell: ({ row }) => (
          <StatusBadge
            label={row.original.status === 'open' ? t('Open') : t('Mitigated')}
            variant={row.original.status === 'open' ? 'danger' : 'success'}
            copyable={false}
          />
        ),
        filterFn: (row, id, value) => {
          if (!Array.isArray(value) || value.length === 0) return true
          return value.includes(row.getValue(id))
        },
      },
      {
        accessorKey: 'first_found_at',
        header: t('First found'),
        size: 165,
        meta: { mobileHidden: true },
        cell: ({ row }) => formatTimestampToDate(row.original.first_found_at),
      },
      {
        accessorKey: 'last_found_at',
        header: t('Last confirmed'),
        size: 165,
        cell: ({ row }) => formatTimestampToDate(row.original.last_found_at),
      },
      {
        id: 'notifications',
        header: t('Notifications'),
        minSize: 220,
        cell: ({ row }) => {
          const notifications = selectLatestTokenLeakNotificationsByChannel(
            row.original.notifications
          )
          return (
            <StatusBadgeList
              items={notifications}
              max={3}
              getKey={(notification) => notification.channel}
              renderItem={(notification) => (
                <StatusBadge
                  label={`${getNotificationChannelLabel(t, notification.channel)}: ${getNotificationStatusLabel(t, notification.status)}`}
                  variant={getNotificationVariant(notification.status)}
                  copyable={false}
                />
              )}
            />
          )
        },
      },
      {
        id: 'actions',
        header: t('Actions'),
        size: 120,
        cell: ({ row }) =>
          row.original.status === 'open' ? (
            <Button
              type='button'
              size='sm'
              variant='destructive'
              onClick={() => setDisableTarget(row.original)}
            >
              <ShieldOff data-icon='inline-start' />
              {t('Disable token')}
            </Button>
          ) : (
            <span className='text-muted-foreground text-xs'>-</span>
          ),
      },
    ],
    [t]
  )
  const data = findingsQuery.data
  const { table } = useDataTable({
    data: data?.items ?? [],
    columns,
    totalCount: data?.total ?? 0,
    pagination,
    onPaginationChange,
    columnFilters,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    withSortedRowModel: false,
    ensurePageInRange,
  })

  if (findingsQuery.isError) {
    return (
      <ErrorState
        className='h-full min-h-48 border'
        title={t('Failed to load')}
        description={
          findingsQuery.error instanceof Error
            ? findingsQuery.error.message
            : t('Failed to load leak findings.')
        }
        onRetry={() => void findingsQuery.refetch()}
      />
    )
  }

  return (
    <>
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={findingsQuery.isLoading}
        isFetching={findingsQuery.isFetching}
        emptyTitle={t('No leak findings')}
        emptyDescription={t(
          'Exact matches from GitHub public repositories will appear here.'
        )}
        skeletonKeyPrefix='token-leak-finding-skeleton'
        applyHeaderSize
        toolbarProps={{
          customSearch: null,
          hideViewOptions: true,
          filters: [
            {
              columnId: 'status',
              title: t('Status'),
              singleSelect: true,
              options: [
                { label: t('Open'), value: 'open' },
                { label: t('Mitigated'), value: 'mitigated' },
              ],
            },
          ],
        }}
      />

      <AlertDialog
        open={disableTarget !== null}
        onOpenChange={(open) => {
          if (!open && !disableMutation.isPending) setDisableTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Confirm token disable')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Token #{{tokenId}} will stop working immediately. The leak finding and notification audit will be retained.',
                { tokenId: disableTarget?.token_id ?? '' }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disableMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={
                !canSubmitTokenLeakDisable(
                  disableTarget,
                  disableMutation.isPending
                )
              }
              onClick={() => {
                if (
                  canSubmitTokenLeakDisable(
                    disableTarget,
                    disableMutation.isPending
                  ) &&
                  disableTarget
                ) {
                  disableMutation.mutate(disableTarget.id)
                }
              }}
            >
              {disableMutation.isPending ? (
                <Loader2 data-icon='inline-start' className='animate-spin' />
              ) : (
                <ShieldOff data-icon='inline-start' />
              )}
              {t('Disable token')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
