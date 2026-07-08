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
import type { AxiosError, AxiosInstance } from 'axios'
import {
  Key02Icon,
  Logout02Icon,
  Search01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
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
import { PublicLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  LOG_TYPE_ENUM,
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
  UsageLogsProvider,
  useUsageLogsContext,
} from '@/features/usage-logs/components/usage-logs-provider'
import { UsageLogsMobileList } from '@/features/usage-logs/components/usage-logs-mobile-card'
import type { UsageLog } from '@/features/usage-logs/data/schema'
import { formatLogQuota, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  createTokenLogClient,
  getTokenLogChartData,
  getTokenLogs,
  getTokenLogStat,
  getTokenUsage,
} from './api'
import {
  buildTokenLogFilterParams,
  buildDefaultTokenLogFilters,
  buildTokenLogQueryParams,
  hasTokenLogFilters,
  isTokenLogUsageStatAvailable,
} from './lib'
import type {
  TokenLogChartData,
  TokenLogFilters,
  TokenLogPageData,
  TokenLogStat,
} from './types'

const DEFAULT_PAGE_DATA: TokenLogPageData = {
  items: [],
  page: 1,
  page_size: 20,
  total: 0,
}

const DEFAULT_STAT: TokenLogStat = {
  count: 0,
  quota: 0,
  prompt_tokens: 0,
  completion_tokens: 0,
  total_tokens: 0,
  rpm: 0,
  tpm: 0,
}

const DEFAULT_CHART_DATA: TokenLogChartData = {
  model_stats: [],
  quota_data: [],
}

const logTypeRowTint: Record<number, string> = {
  [LOG_TYPE_ENUM.ERROR]: 'bg-rose-50/40 dark:bg-rose-950/20',
  [LOG_TYPE_ENUM.REFUND]: 'bg-blue-50/30 dark:bg-blue-950/15',
}

const chartAccentClasses = [
  'bg-sky-500',
  'bg-emerald-500',
  'bg-amber-500',
  'bg-rose-500',
  'bg-violet-500',
  'bg-cyan-500',
]

const maxTrendBars = 48

interface TrendDataPoint {
  created_at: number
  end_at: number
  quota: number
  token_used: number
  count: number
}

function buildTrendDataPoints(
  quotaData: TokenLogChartData['quota_data']
): TrendDataPoint[] {
  const rows = new Map<number, TrendDataPoint>()
  quotaData.forEach((item) => {
    const createdAt = item.created_at || 0
    if (createdAt === 0) return
    const row = rows.get(createdAt) ?? {
      created_at: createdAt,
      end_at: createdAt,
      quota: 0,
      token_used: 0,
      count: 0,
    }
    row.quota += item.quota || 0
    row.token_used += item.token_used || 0
    row.count += item.count || 0
    rows.set(createdAt, row)
  })
  const points = [...rows.values()].sort((a, b) => a.created_at - b.created_at)
  if (points.length <= maxTrendBars) return points

  const chunkSize = Math.ceil(points.length / maxTrendBars)
  const compacted: TrendDataPoint[] = []
  for (let i = 0; i < points.length; i += chunkSize) {
    const chunk = points.slice(i, i + chunkSize)
    compacted.push(
      chunk.reduce<TrendDataPoint>(
        (acc, item) => ({
          created_at: acc.created_at,
          end_at: item.end_at,
          quota: acc.quota + item.quota,
          token_used: acc.token_used + item.token_used,
          count: acc.count + item.count,
        }),
        {
          created_at: chunk[0]?.created_at ?? 0,
          end_at: chunk[0]?.end_at ?? 0,
          quota: 0,
          token_used: 0,
          count: 0,
        }
      )
    )
  }
  return compacted
}

function getTrendLabelStep(count: number): number {
  if (count <= 8) return 1
  if (count <= 16) return 2
  if (count <= 24) return 3
  return Math.ceil(count / 8)
}

function formatTrendTimeRange(item: TrendDataPoint): string {
  const start = formatTimestampToDate(item.created_at)
  if (item.end_at === item.created_at) return start
  return `${start} - ${formatTimestampToDate(item.end_at)}`
}

function resolveApiErrorMessage(
  error: unknown,
  t: (key: string) => string
): string {
  const axiosError = error as AxiosError<{ message?: string }>
  const status = axiosError.response?.status
  if (status === 401) return t('Invalid API key')
  if (status === 403) return t('User has been banned')
  if (status === 429) return t('Too many requests. Please try again later.')
  return (
    axiosError.response?.data?.message ||
    axiosError.message ||
    t('Request failed')
  )
}

function formatCompactNumber(value: number): string {
  return Number(value || 0).toLocaleString()
}

function StatBadgeCard(props: {
  label: string
  value: string
  description?: string
  accent: string
}) {
  return (
    <Card size='sm' className='rounded-lg'>
      <CardContent className='flex items-center gap-3'>
        <span className={cn('h-9 w-1 rounded-full', props.accent)} />
        <div className='min-w-0'>
          <p className='text-muted-foreground text-xs'>{props.label}</p>
          <p className='text-foreground truncate font-mono text-lg font-semibold tabular-nums'>
            {props.value}
          </p>
          {props.description && (
            <p className='text-muted-foreground/70 truncate text-xs'>
              {props.description}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function TokenLogStatsCards(props: {
  stat?: TokenLogStat
  isLoading: boolean
  usageAvailable: boolean
}) {
  const { t } = useTranslation()
  const stat = props.stat ?? DEFAULT_STAT
  const usageDescription = props.usageAvailable
    ? undefined
    : t('Usage statistics are only available for consumption logs')

  if (props.isLoading) {
    return (
      <div className='grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
        {[0, 1, 2, 3].map((item) => (
          <Skeleton key={item} className='h-[88px] rounded-lg' />
        ))}
      </div>
    )
  }

  return (
    <div className='grid gap-2 sm:grid-cols-2 xl:grid-cols-4'>
      <StatBadgeCard
        label={t('Requests')}
        value={formatCompactNumber(stat.count)}
        accent='bg-emerald-500/70'
      />
      <StatBadgeCard
        label={t('Usage')}
        value={formatLogQuota(stat.quota)}
        description={usageDescription}
        accent='bg-sky-500/70'
      />
      <StatBadgeCard
        label={t('Tokens')}
        value={formatCompactNumber(
          stat.total_tokens || stat.prompt_tokens + stat.completion_tokens
        )}
        description={
          props.usageAvailable
            ? `${formatCompactNumber(stat.prompt_tokens)} / ${formatCompactNumber(stat.completion_tokens)}`
            : usageDescription
        }
        accent='bg-amber-500/75'
      />
      <StatBadgeCard
        label='RPM / TPM'
        value={`${formatCompactNumber(stat.rpm)} / ${formatCompactNumber(stat.tpm)}`}
        description={
          props.usageAvailable
            ? undefined
            : t('TPM only applies to consumption logs')
        }
        accent='bg-violet-500/70'
      />
    </div>
  )
}

function TokenLogCharts(props: {
  data?: TokenLogChartData
  isLoading: boolean
  usageAvailable: boolean
  onSelectModel: (modelName: string) => void
}) {
  const { t } = useTranslation()
  const data = props.data ?? DEFAULT_CHART_DATA
  const modelStats = data.model_stats ?? []
  const quotaData = data.quota_data ?? []
  const totalModelCount = modelStats.reduce(
    (sum, item) => sum + (item.count || 0),
    0
  )
  const maxModelCount = Math.max(...modelStats.map((item) => item.count), 1)
  const trendItems = buildTrendDataPoints(quotaData)
  const maxTrendQuota = Math.max(
    ...trendItems.map((item) => item.quota || 0),
    1
  )
  const trendLabelStep = getTrendLabelStep(trendItems.length)
  const yTicks = [1, 0.75, 0.5, 0.25, 0].map((ratio) => maxTrendQuota * ratio)

  if (props.isLoading) {
    return (
      <div className='grid gap-3 lg:grid-cols-2'>
        <Skeleton className='h-[220px] rounded-lg' />
        <Skeleton className='h-[220px] rounded-lg' />
      </div>
    )
  }

  return (
    <div className='grid gap-3 lg:grid-cols-2'>
      <Card className='rounded-lg'>
        <CardHeader>
          <CardTitle>{t('Model Calls')}</CardTitle>
          <CardDescription>
            {t('Total')}: {formatCompactNumber(totalModelCount)}
          </CardDescription>
        </CardHeader>
        <CardContent className='flex flex-col gap-2.5'>
          {modelStats.length === 0 ? (
            <p className='text-muted-foreground text-sm'>{t('No Data')}</p>
          ) : (
            modelStats.slice(0, 8).map((item, index) => {
              const percent = Math.round((item.count / maxModelCount) * 100)
              const modelName = item.model_name?.trim()
              const rowContent = (
                <>
                  <div className='flex items-center justify-between gap-3 text-xs'>
                    <span className='truncate font-medium'>
                      {modelName || t('Unknown')}
                    </span>
                    <span className='text-muted-foreground font-mono tabular-nums'>
                      {formatCompactNumber(item.count)}
                    </span>
                  </div>
                  <div className='bg-muted h-2 overflow-hidden rounded-full'>
                    <div
                      className={cn(
                        'h-full rounded-full',
                        chartAccentClasses[index % chartAccentClasses.length]
                      )}
                      style={{ width: `${Math.max(percent, 4)}%` }}
                    />
                  </div>
                </>
              )
              if (!modelName) {
                return (
                  <div
                    key={`${item.model_name}-${index}`}
                    className='flex flex-col gap-1'
                  >
                    {rowContent}
                  </div>
                )
              }
              return (
                <button
                  key={`${item.model_name}-${index}`}
                  type='button'
                  className='w-full rounded-md px-1 py-0.5 text-left transition-colors hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40'
                  onClick={() => props.onSelectModel(modelName)}
                >
                  <div className='flex flex-col gap-1'>{rowContent}</div>
                </button>
              )
            })
          )}
        </CardContent>
      </Card>

      <Card className='rounded-lg'>
        <CardHeader>
          <CardTitle>{t('Usage Trend')}</CardTitle>
          <CardDescription>{t('Recent usage by time bucket')}</CardDescription>
        </CardHeader>
        <CardContent>
          {trendItems.length === 0 ? (
            <p className='text-muted-foreground text-sm'>
              {props.usageAvailable
                ? t('No Data')
                : t('Usage statistics are only available for consumption logs')}
            </p>
          ) : (
            <div className='grid h-52 grid-cols-[4.5rem_minmax(0,1fr)] grid-rows-[minmax(0,1fr)_2rem] gap-x-2 overflow-hidden'>
              <div className='col-start-1 row-start-1 flex h-full flex-col justify-between py-1 text-right text-[10px] leading-none text-muted-foreground'>
                {yTicks.map((tick, index) => (
                  <span key={`${tick}-${index}`} className='truncate'>
                    {formatLogQuota(tick)}
                  </span>
                ))}
              </div>
              <div className='relative col-start-2 row-start-1 min-w-0 overflow-hidden rounded-sm border-b border-l border-border/70 pl-2 pr-1 pt-1'>
                <div className='pointer-events-none absolute inset-x-2 inset-y-1 flex flex-col justify-between'>
                  {[0, 1, 2, 3, 4].map((line) => (
                    <span
                      key={line}
                      className='border-t border-dashed border-border/70'
                    />
                  ))}
                </div>
                <div className='relative flex h-full items-end gap-1'>
                  {trendItems.map((item, index) => {
                    const percent = Math.round(
                      (item.quota / maxTrendQuota) * 100
                    )
                    return (
                      <Tooltip
                        key={`${item.created_at}-${item.end_at}-${index}`}
                      >
                        <TooltipTrigger
                          render={
                            <div className='flex h-full min-w-0 flex-1 items-end' />
                          }
                        >
                          <div
                            className='min-h-1 w-full rounded-t-sm bg-sky-500/75'
                            style={{ height: `${Math.max(percent, 3)}%` }}
                          />
                        </TooltipTrigger>
                        <TooltipContent side='top' className='max-w-64'>
                          <div className='flex flex-col gap-0.5 text-xs'>
                            <p>{formatTrendTimeRange(item)}</p>
                            <p>
                              {t('Quota')}: {formatLogQuota(item.quota)}
                            </p>
                            <p>
                              {t('Requests')}: {formatCompactNumber(item.count)}
                            </p>
                            <p>
                              {t('Tokens')}:{' '}
                              {formatCompactNumber(item.token_used)}
                            </p>
                          </div>
                        </TooltipContent>
                      </Tooltip>
                    )
                  })}
                </div>
              </div>
              <div className='col-start-2 row-start-2 flex min-w-0 gap-1 pl-2 pr-1 pt-1'>
                {trendItems.map((item, index) => {
                  const showLabel =
                    index === 0 ||
                    index === trendItems.length - 1 ||
                    index % trendLabelStep === 0
                  return (
                    <span
                      key={`${item.created_at}-${index}`}
                      className='min-w-0 flex-1 truncate text-center text-[10px] leading-tight text-muted-foreground'
                    >
                      {showLabel
                        ? formatTimestampToDate(item.created_at).slice(5, 16)
                        : ''}
                    </span>
                  )
                })}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

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

function TokenLogsTable(props: {
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

function AuthPanel(props: {
  apiKey: string
  setApiKey: (value: string) => void
  authError: string
  isLoading: boolean
  onSubmit: () => void
}) {
  const { t } = useTranslation()

  return (
    <Card className='mx-auto mt-16 max-w-lg rounded-lg'>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          <HugeiconsIcon icon={Key02Icon} strokeWidth={2} />
          {t('API Key Logs')}
        </CardTitle>
      </CardHeader>
      <CardContent className='flex flex-col gap-4'>
        <div className='grid gap-2'>
          <Label htmlFor='public-log-api-key'>{t('API Key')}</Label>
          <Input
            id='public-log-api-key'
            type='password'
            autoComplete='off'
            placeholder={t('Enter API Key')}
            value={props.apiKey}
            onChange={(event) => props.setApiKey(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') props.onSubmit()
            }}
          />
        </div>
        {props.authError && (
          <Alert variant='destructive'>
            <AlertTitle>{t('Authentication failed')}</AlertTitle>
            <AlertDescription>{props.authError}</AlertDescription>
          </Alert>
        )}
        <Button
          type='button'
          className='w-full'
          onClick={props.onSubmit}
          disabled={props.isLoading}
        >
          {props.isLoading ? (
            <LoadingState inline size='sm' message={t('Verifying...')} />
          ) : (
            <>
              <HugeiconsIcon
                icon={Search01Icon}
                data-icon='inline-start'
                strokeWidth={2}
              />
              {t('View Logs')}
            </>
          )}
        </Button>
      </CardContent>
    </Card>
  )
}

function TokenLogsWorkspace(props: {
  client: AxiosInstance
  onSwitchKey: () => void
}) {
  const { t } = useTranslation()
  const [draftFilters, setDraftFilters] = useState<TokenLogFilters>(() =>
    buildDefaultTokenLogFilters()
  )
  const [appliedFilters, setAppliedFilters] = useState<TokenLogFilters>(() =>
    buildDefaultTokenLogFilters()
  )
  const filterParams = useMemo(
    () => buildTokenLogFilterParams(appliedFilters),
    [appliedFilters]
  )
  const usageAvailable = isTokenLogUsageStatAvailable(appliedFilters)

  const statsQuery = useQuery({
    queryKey: ['token-logs', 'stat', filterParams],
    queryFn: async () => {
      const result = await getTokenLogStat(props.client, filterParams)
      if (!result.success) {
        throw new Error(result.message || t('Failed to load statistics'))
      }
      return result.data ?? DEFAULT_STAT
    },
    placeholderData: (previousData) => previousData,
  })
  const chartQuery = useQuery({
    queryKey: ['token-logs', 'chart', filterParams],
    queryFn: async () => {
      const result = await getTokenLogChartData(props.client, filterParams)
      if (!result.success) {
        throw new Error(result.message || t('Failed to load chart data'))
      }
      return result.data ?? DEFAULT_CHART_DATA
    },
    placeholderData: (previousData) => previousData,
  })
  const statusError =
    statsQuery.error instanceof Error
      ? statsQuery.error.message
      : chartQuery.error instanceof Error
        ? chartQuery.error.message
        : ''

  return (
    <div className='flex min-h-0 flex-col gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='min-w-0'>
          <h1 className='text-lg font-semibold tracking-tight'>
            {t('API Key Logs')}
          </h1>
        </div>
        <Button variant='outline' size='sm' onClick={props.onSwitchKey}>
          <HugeiconsIcon
            icon={Logout02Icon}
            data-icon='inline-start'
            strokeWidth={2}
          />
          {t('Switch Key')}
        </Button>
      </div>

      {statusError && (
        <Alert variant='destructive'>
          <AlertTitle>{t('Failed to load statistics')}</AlertTitle>
          <AlertDescription>{statusError}</AlertDescription>
        </Alert>
      )}

      <TokenLogStatsCards
        stat={statsQuery.data}
        isLoading={statsQuery.isLoading}
        usageAvailable={usageAvailable}
      />
      <TokenLogCharts
        data={chartQuery.data}
        isLoading={chartQuery.isLoading}
        usageAvailable={usageAvailable}
        onSelectModel={(modelName) => {
          const nextModel = modelName.trim()
          if (!nextModel) return
          setDraftFilters((previous) =>
            previous.model?.trim() === nextModel
              ? previous
              : { ...previous, model: nextModel }
          )
          setAppliedFilters((previous) =>
            previous.model?.trim() === nextModel
              ? previous
              : { ...previous, model: nextModel }
          )
        }}
      />
      <TokenLogsTable
        client={props.client}
        appliedFilters={appliedFilters}
        draftFilters={draftFilters}
        setDraftFilters={setDraftFilters}
        onApplyFilters={() => setAppliedFilters(draftFilters)}
        onResetFilters={() => {
          const nextFilters = buildDefaultTokenLogFilters()
          setDraftFilters(nextFilters)
          setAppliedFilters(nextFilters)
        }}
      />
    </div>
  )
}

function PublicTokenLogsContent() {
  const { t } = useTranslation()
  const [apiKey, setApiKey] = useState('')
  const [authError, setAuthError] = useState('')
  const [isAuthenticating, setIsAuthenticating] = useState(false)
  const [authenticatedKey, setAuthenticatedKey] = useState('')
  const client = useMemo(
    () => (authenticatedKey ? createTokenLogClient(authenticatedKey) : null),
    [authenticatedKey]
  )

  const handleSubmit = useCallback(async () => {
    const normalizedKey = apiKey.trim()
    if (!normalizedKey) {
      setAuthError(t('Please enter an API key'))
      return
    }
    setIsAuthenticating(true)
    setAuthError('')
    try {
      const nextClient = createTokenLogClient(normalizedKey)
      const result = await getTokenUsage(nextClient)
      if (result.code !== true) {
        setAuthError(result.message || t('Invalid API key'))
        return
      }
      setAuthenticatedKey(normalizedKey)
      setApiKey('')
    } catch (error) {
      setAuthError(resolveApiErrorMessage(error, t))
    } finally {
      setIsAuthenticating(false)
    }
  }, [apiKey, t])

  const handleSwitchKey = useCallback(() => {
    setAuthenticatedKey('')
    setApiKey('')
    setAuthError('')
  }, [])

  if (!client) {
    return (
      <AuthPanel
        apiKey={apiKey}
        setApiKey={setApiKey}
        authError={authError}
        isLoading={isAuthenticating}
        onSubmit={handleSubmit}
      />
    )
  }

  return <TokenLogsWorkspace client={client} onSwitchKey={handleSwitchKey} />
}

export function PublicTokenLogs() {
  return (
    <PublicLayout showAuthButtons showThemeSwitch>
      <UsageLogsProvider>
        <PublicTokenLogsContent />
      </UsageLogsProvider>
    </PublicLayout>
  )
}
