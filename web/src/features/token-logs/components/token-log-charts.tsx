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

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatLogQuota, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { chartAccentClasses, DEFAULT_CHART_DATA } from '../constants'
import {
  buildTrendDataPoints,
  formatCompactNumber,
  formatTrendTimeRange,
  getTrendLabelStep,
} from '../lib/chart'
import type { TokenLogChartData } from '../types'

export function TokenLogCharts(props: {
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
                    key={modelName || `unknown-model-${item.count}`}
                    className='flex flex-col gap-1'
                  >
                    {rowContent}
                  </div>
                )
              }
              return (
                <button
                  key={modelName}
                  type='button'
                  className='hover:bg-muted/60 focus-visible:ring-ring/40 w-full rounded-md px-1 py-0.5 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none'
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
              <div className='text-muted-foreground col-start-1 row-start-1 flex h-full flex-col justify-between py-1 text-right text-[10px] leading-none'>
                {yTicks.map((tick) => (
                  <span key={tick} className='truncate'>
                    {formatLogQuota(tick)}
                  </span>
                ))}
              </div>
              <div className='border-border/70 relative col-start-2 row-start-1 min-w-0 overflow-hidden rounded-sm border-b border-l pt-1 pr-1 pl-2'>
                <div className='pointer-events-none absolute inset-x-2 inset-y-1 flex flex-col justify-between'>
                  {[0, 1, 2, 3, 4].map((line) => (
                    <span
                      key={line}
                      className='border-border/70 border-t border-dashed'
                    />
                  ))}
                </div>
                <div className='relative flex h-full items-end gap-1'>
                  {trendItems.map((item) => {
                    const percent = Math.round(
                      (item.quota / maxTrendQuota) * 100
                    )
                    return (
                      <Tooltip key={`${item.created_at}-${item.end_at}`}>
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
              <div className='col-start-2 row-start-2 flex min-w-0 gap-1 pt-1 pr-1 pl-2'>
                {trendItems.map((item, index) => {
                  const showLabel =
                    index === 0 ||
                    index === trendItems.length - 1 ||
                    index % trendLabelStep === 0
                  return (
                    <span
                      key={`${item.created_at}-${item.end_at}`}
                      className='text-muted-foreground min-w-0 flex-1 truncate text-center text-[10px] leading-tight'
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
