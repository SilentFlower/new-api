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
import { Logout02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import type { AxiosInstance } from 'axios'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

import { getTokenLogChartData, getTokenLogStat } from '../api'
import { DEFAULT_CHART_DATA, DEFAULT_STAT } from '../constants'
import {
  buildDefaultTokenLogFilters,
  buildTokenLogFilterParams,
  isTokenLogUsageStatAvailable,
} from '../lib'
import type { TokenLogFilters } from '../types'
import { TokenLogCharts } from './token-log-charts'
import { TokenLogStatsCards } from './token-log-stats-cards'
import { TokenLogsTable } from './token-logs-table'

export function TokenLogsWorkspace(props: {
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
  let statusError = ''
  if (statsQuery.error instanceof Error) {
    statusError = statsQuery.error.message
  } else if (chartQuery.error instanceof Error) {
    statusError = chartQuery.error.message
  }

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
