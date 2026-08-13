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
import { Download04Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DateTimePicker } from '@/components/datetime-picker'
import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { exportDashboardReport } from '@/features/dashboard/api'
import { useDashboardFilterOptions } from '@/features/dashboard/hooks/use-dashboard-filter-options'
import {
  buildDefaultDashboardFilters,
  buildQueryParams,
  filterDashboardTokenOptionsByGroups,
  filterDashboardTokenValuesByGroups,
  getDefaultDays,
} from '@/features/dashboard/lib'
import type {
  DashboardChartPreferences,
  DashboardFilters,
} from '@/features/dashboard/types'
import { computeTimeRange } from '@/lib/time'

interface DashboardExportDialogProps {
  preferences: DashboardChartPreferences
  currentFilters: DashboardFilters
}

export function DashboardExportDialog(props: DashboardExportDialogProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [filters, setFilters] = useState<DashboardFilters>(
    () =>
      props.currentFilters ?? buildDefaultDashboardFilters(props.preferences)
  )
  const [isExporting, setIsExporting] = useState(false)
  const { groupOptions, tokenOptions, isLoading } =
    useDashboardFilterOptions(open)

  const handleOpenChange = (nextOpen: boolean) => {
    if (isExporting) return
    if (nextOpen) {
      setFilters(
        props.currentFilters ?? buildDefaultDashboardFilters(props.preferences)
      )
    }
    setOpen(nextOpen)
  }

  const handleGroupsChange = (groups: string[]) => {
    setFilters((prev) => ({
      ...prev,
      groups,
      token_names: filterDashboardTokenValuesByGroups(
        prev.token_names,
        groups,
        tokenOptions
      ),
    }))
  }

  const handleExport = async () => {
    setIsExporting(true)
    try {
      const timeRange = computeTimeRange(
        getDefaultDays(filters.time_granularity),
        filters.start_timestamp,
        filters.end_timestamp
      )
      await exportDashboardReport(buildQueryParams(timeRange, filters))
      toast.success(t('Report export started'))
      setOpen(false)
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Failed to export report')
      toast.error(message || t('Failed to export report'))
    } finally {
      setIsExporting(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      trigger={
        <Button variant='outline' size='sm'>
          <HugeiconsIcon
            icon={Download04Icon}
            data-icon='inline-start'
            strokeWidth={2}
          />
          {t('Export')}
        </Button>
      }
      title={t('Export Dashboard Report')}
      description={t(
        'Download an Excel report using the selected time range, groups, and API keys.'
      )}
      contentClassName='max-sm:h-dvh max-sm:w-screen max-sm:max-w-none max-sm:rounded-none max-sm:p-4 sm:max-w-lg'
      contentHeight='min(48vh, 460px)'
      footerClassName='grid grid-cols-2 gap-2 sm:flex'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => handleOpenChange(false)}
            disabled={isExporting}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={handleExport} disabled={isExporting}>
            {isExporting && <Loader2 className='animate-spin' />}
            {t('Export')}
          </Button>
        </>
      }
    >
      <ScrollArea className='h-full pr-3 sm:pr-4'>
        <div className='grid gap-3 py-2'>
          <div className='grid gap-2'>
            <Label htmlFor='dashboard-export-start'>{t('Start Time')}</Label>
            <DateTimePicker
              value={filters.start_timestamp}
              onChange={(date) =>
                setFilters((prev) => ({
                  ...prev,
                  start_timestamp: date || undefined,
                }))
              }
              placeholder={t('Select start time')}
            />
          </div>

          <div className='grid gap-2'>
            <Label htmlFor='dashboard-export-end'>{t('End Time')}</Label>
            <DateTimePicker
              value={filters.end_timestamp}
              onChange={(date) =>
                setFilters((prev) => ({
                  ...prev,
                  end_timestamp: date || undefined,
                }))
              }
              placeholder={t('Select end time')}
            />
          </div>

          <div className='grid gap-2'>
            <Label htmlFor='dashboard-export-groups'>{t('Groups')}</Label>
            <MultiSelect
              id='dashboard-export-groups'
              options={groupOptions}
              selected={filters.groups ?? []}
              onChange={handleGroupsChange}
              placeholder={isLoading ? t('Loading...') : t('All groups')}
              emptyText={t('No matching groups')}
              disabled={isLoading}
              maxVisibleChips={3}
            />
          </div>

          <div className='grid gap-2'>
            <Label htmlFor='dashboard-export-token-names'>
              {t('API Keys')}
            </Label>
            <MultiSelect
              id='dashboard-export-token-names'
              options={filterDashboardTokenOptionsByGroups(
                tokenOptions,
                filters.groups
              )}
              selected={filters.token_names ?? []}
              onChange={(values) =>
                setFilters((prev) => ({ ...prev, token_names: values }))
              }
              placeholder={isLoading ? t('Loading...') : t('All API keys')}
              emptyText={t('No matching API keys')}
              disabled={isLoading}
              maxVisibleChips={3}
            />
          </div>
        </div>
      </ScrollArea>
    </Dialog>
  )
}
