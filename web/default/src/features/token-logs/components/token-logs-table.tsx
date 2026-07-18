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
import type { AxiosInstance } from 'axios'
import { useQuery } from '@tanstack/react-query'
import type {
  ColumnDef,
  OnChangeFn,
  PaginationState,
  Table as TanstackTable,
} from '@tanstack/react-table'
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type KeyboardEvent,
  type SetStateAction,
} from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  LOG_TYPE_FILTERS,
} from '@/features/usage-logs/constants'
import { useCommonLogsColumns } from '@/features/usage-logs/components/columns/common-logs-columns'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '@/features/usage-logs/components/logs-filter-toolbar'
import {
  useUsageLogsContext,
} from '@/features/usage-logs/components/usage-logs-provider'
import { UsageLogsMobileList } from '@/features/usage-logs/components/usage-logs-mobile-card'
import type { UsageLog } from '@/features/usage-logs/data/schema'
import { cn } from '@/lib/utils'

import { getTokenLogs } from '../api'
import { DEFAULT_PAGE_DATA, logTypeRowTint } from '../constants'
import {
  buildTokenLogQueryParams,
  hasTokenLogFilters,
} from '../lib'
import type { TokenLogFilters } from '../types'

function TokenLogsToolbar(props: {
  table: TanstackTable<UsageLog>
  draftFilters: TokenLogFilters
  setDraftFilters: Dispatch<SetStateAction<TokenLogFilters>>
  hasActiveFilters: boolean
  isFetching: boolean
  onApply: () => void
  onReset: () => void
}) {
  const { t } = useTranslation()
  const { sensitiveVisible, setSensitiveVisible } = useUsageLogsContext()
  const logTypeItems = useMemo(
    () =>
      LOG_TYPE_FILTERS.map((type) => ({
        value: type.value,
        label: t(type.label),
      })),
    [t]
  )
  const logTypeLabel =
    logTypeItems.find((type) => type.value === props.draftFilters.type)
      ?.label ?? t('All Types')

  const handleChange = useCallback(
    (field: keyof TokenLogFilters, value: Date | string | undefined) => {
      props.setDraftFilters((prev) => ({ ...prev, [field]: value }))
    },
    [props]
  )
  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      if (event.key === 'Enter') props.onApply()
    },
    [props]
  )

  const dateRangeFilter = (
    <LogsFilterField wide>
      <CompactDateTimeRangePicker
        start={props.draftFilters.startTime}
        end={props.draftFilters.endTime}
        onChange={({ start, end }) => {
          handleChange('startTime', start)
          handleChange('endTime', end)
        }}
      />
    </LogsFilterField>
  )
  const modelFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Model Name')}
        value={props.draftFilters.model || ''}
        onChange={(event) => handleChange('model', event.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const typeFilter = (
    <LogsFilterField>
      <Select
        items={logTypeItems}
        value={props.draftFilters.type}
        onValueChange={(value) =>
          handleChange('type', value == null ? '0' : String(value))
        }
      >
        <SelectTrigger>
          <SelectValue>{logTypeLabel}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {LOG_TYPE_FILTERS.map((type) => (
              <SelectItem key={type.value} value={type.value}>
                {t(type.label)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </LogsFilterField>
  )
  const requestIdFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Request ID')}
        value={props.draftFilters.requestId || ''}
        onChange={(event) => handleChange('requestId', event.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )

  return (
    <LogsFilterToolbar
      table={props.table}
      primaryFilters={
        <>
          {dateRangeFilter}
          {modelFilter}
          {typeFilter}
        </>
      }
      advancedFilters={requestIdFilter}
      mobilePinnedFilters={dateRangeFilter}
      mobileFilters={
        <>
          {modelFilter}
          {typeFilter}
          {requestIdFilter}
        </>
      }
      actionStart={
        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={() => setSensitiveVisible(!sensitiveVisible)}
        >
          {sensitiveVisible ? t('Hide') : t('Show')}
        </Button>
      }
      hasActiveFilters={props.hasActiveFilters}
      hasAdvancedActiveFilters={Boolean(props.draftFilters.requestId?.trim())}
      advancedFilterCount={props.draftFilters.requestId?.trim() ? 1 : 0}
      mobileFilterCount={
        [
          props.draftFilters.model,
          props.draftFilters.type !== '0',
          props.draftFilters.requestId,
        ].filter(Boolean).length
      }
      searchLoading={props.isFetching}
      onSearch={props.onApply}
      onReset={props.onReset}
    />
  )
}

export function TokenLogsTable(props: {
  client: AxiosInstance
  appliedFilters: TokenLogFilters
  draftFilters: TokenLogFilters
  setDraftFilters: Dispatch<SetStateAction<TokenLogFilters>>
  onApplyFilters: () => void
  onResetFilters: () => void
}) {
  const { t } = useTranslation()
  const baseColumns = useCommonLogsColumns(false)
  const columns = useMemo<ColumnDef<UsageLog>[]>(
    () => [
      ...baseColumns,
      {
        accessorKey: 'request_id',
        header: t('Request ID'),
        cell: ({ row }) => {
          const requestId = row.original.request_id
          if (!requestId) {
            return <span className='text-muted-foreground/50 text-xs'>-</span>
          }
          return (
            <StatusBadge
              label={requestId}
              copyText={requestId}
              showDot={false}
              size='sm'
              className='max-w-[180px] font-mono'
            />
          )
        },
        size: 200,
      },
    ],
    [baseColumns, t]
  )
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })
  const handlePaginationChange = useCallback<OnChangeFn<PaginationState>>(
    (updater) => {
      setPagination((previous) =>
        typeof updater === 'function' ? updater(previous) : updater
      )
    },
    []
  )

  useEffect(() => {
    setPagination((previous) =>
      previous.pageIndex === 0 ? previous : { ...previous, pageIndex: 0 }
    )
  }, [props.appliedFilters])

  const logsQuery = useQuery({
    queryKey: [
      'token-logs',
      pagination.pageIndex,
      pagination.pageSize,
      props.appliedFilters,
    ],
    queryFn: async () => {
      const result = await getTokenLogs(
        props.client,
        buildTokenLogQueryParams(
          props.appliedFilters,
          pagination.pageIndex + 1,
          pagination.pageSize
        )
      )
      if (!result.success) {
        throw new Error(result.message || t('Failed to load logs'))
      }
      return result.data ?? DEFAULT_PAGE_DATA
    },
    placeholderData: (previousData) => previousData,
  })

  const tableData = logsQuery.data ?? DEFAULT_PAGE_DATA
  const logs = tableData.items ?? []
  const { table } = useDataTable({
    data: logs,
    columns,
    pagination,
    onPaginationChange: handlePaginationChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: tableData.total || 0,
    enableRowSelection: false,
    columnVisibilityStorageKey: 'token-logs:public:column-visibility',
  })
  const isLoading = logsQuery.isLoading
  const isFetching = logsQuery.isFetching

  return (
    <div className='min-h-[520px]'>
      {logsQuery.isError && (
        <Alert variant='destructive' className='mb-3'>
          <AlertTitle>{t('Failed to load logs')}</AlertTitle>
          <AlertDescription>
            {logsQuery.error instanceof Error
              ? logsQuery.error.message
              : t('Request failed')}
          </AlertDescription>
        </Alert>
      )}
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={isLoading}
        isFetching={isFetching}
        emptyTitle={t('No Logs Found')}
        emptyDescription={t(
          'No usage logs available. Logs will appear here once API calls are made.'
        )}
        skeletonKeyPrefix='token-log-skeleton'
        applyHeaderSize
        tableClassName='[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
        toolbar={
          <TokenLogsToolbar
            table={table}
            draftFilters={props.draftFilters}
            setDraftFilters={props.setDraftFilters}
            hasActiveFilters={hasTokenLogFilters(props.draftFilters)}
            isFetching={isFetching}
            onApply={() => {
              setPagination((prev) => ({ ...prev, pageIndex: 0 }))
              props.onApplyFilters()
            }}
            onReset={() => {
              setPagination((prev) => ({ ...prev, pageIndex: 0 }))
              props.onResetFilters()
            }}
          />
        }
        mobile={
          <UsageLogsMobileList
            table={table}
            isLoading={isLoading}
            logCategory='common'
          />
        }
        renderRow={(row) => {
          const logType = row.original.type
          const tintClass = logTypeRowTint[logType] ?? ''

          return (
            <DataTableRow
              key={row.id}
              row={row}
              className={cn('transition-colors', tintClass)}
              getColumnClassName={() => 'py-2'}
            />
          )
        }}
      />
    </div>
  )
}
