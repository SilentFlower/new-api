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
import { Link } from '@tanstack/react-router'
import type { TFunction } from 'i18next'
import {
  Clock3,
  Loader2,
  RefreshCw,
  ScanSearch,
  Settings,
  ShieldAlert,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import { formatTimestampToDate } from '@/lib/format'

import { getTokenLeakScanStatus, startTokenLeakScan } from './api'
import { TokenLeakFindingsTable } from './components/token-leak-findings-table'
import {
  isTokenLeakScanTaskActive,
  parseTokenLeakTokenID,
} from './lib/token-leak-scan'
import type { TokenLeakScanCoverageStatus, TokenLeakScanTask } from './types'

function getTaskStatusVariant(
  task: TokenLeakScanTask | null
): 'success' | 'danger' | 'warning' | 'neutral' {
  switch (task?.status) {
    case 'succeeded':
      return 'success'
    case 'failed':
      return 'danger'
    case 'pending':
    case 'running':
      return 'warning'
    default:
      return 'neutral'
  }
}

function getTaskStatusLabel(t: TFunction, task: TokenLeakScanTask): string {
  switch (task.status) {
    case 'pending':
      return t('Pending')
    case 'running':
      return t('Running')
    case 'succeeded':
      return t('Success')
    case 'failed':
      return t('Failed')
    default:
      return task.status
  }
}

function getCoverageStatusLabel(
  t: TFunction,
  status: TokenLeakScanCoverageStatus['status']
): string {
  switch (status) {
    case 'enabled':
      return t('Enabled')
    case 'disabled':
      return t('Disabled')
    case 'exhausted':
      return t('Exhausted')
    case 'expired':
      return t('Expired')
    default:
      return t('Other')
  }
}

/**
 * 渲染 root 管理员的 Key 泄露检测与处置工作台。
 *
 * @returns 扫描状态、手动任务控制和泄露位置列表。
 */
export function TokenLeakScan() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const hadActiveTaskRef = useRef(false)
  const [tokenIdInput, setTokenIdInput] = useState('')
  const statusQuery = useQuery({
    queryKey: ['token-leak-scan', 'status'],
    queryFn: getTokenLeakScanStatus,
    refetchInterval: (query) =>
      isTokenLeakScanTaskActive(query.state.data?.current_task ?? null)
        ? 2000
        : false,
  })
  const runMutation = useMutation({
    mutationFn: (tokenId?: number) => startTokenLeakScan(tokenId),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({
        queryKey: ['token-leak-scan', 'status'],
      })
      toast.success(
        result.created
          ? t('Leak scan task started.')
          : t('Existing leak scan task reused.')
      )
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to start leak scan.'))
    },
  })

  const currentTask = statusQuery.data?.current_task ?? null
  const lastTask = statusQuery.data?.last_task ?? null
  const taskActive = isTokenLeakScanTaskActive(currentTask)

  useEffect(() => {
    if (hadActiveTaskRef.current && !taskActive) {
      void queryClient.invalidateQueries({
        queryKey: ['token-leak-scan', 'findings'],
      })
    }
    hadActiveTaskRef.current = taskActive
  }, [queryClient, taskActive])

  const status = statusQuery.data
  const credentialsReady = Boolean(
    status?.credentials.github_token_configured &&
    status.credentials.scan_secret_configured
  )
  const canRun = Boolean(status?.enabled && credentialsReady && !taskActive)
  const progress = currentTask?.state?.progress ?? 0
  const processed = currentTask?.state?.processed ?? 0
  const total = currentTask?.state?.total ?? 0
  const displayLastTask =
    lastTask !== null && lastTask.task_id !== currentTask?.task_id
  const stats = [
    { label: t('Total tokens'), value: status?.total_tokens ?? 0 },
    {
      label: t('High-priority tokens'),
      value: status?.enabled_tokens ?? 0,
    },
    { label: t('Other tokens'), value: status?.other_tokens ?? 0 },
    { label: t('Never scanned'), value: status?.pending_tokens ?? 0 },
    { label: t('Open findings'), value: status?.open_findings ?? 0 },
    {
      label: t('Estimated full scan'),
      value: t('{{count}} min', {
        count: status?.estimated_full_scan_minutes ?? 0,
      }),
    },
  ]
  let scanStateLabel = t('Disabled')
  let scanStateVariant: 'success' | 'danger' | 'warning' | 'neutral' = 'neutral'
  if (currentTask) {
    scanStateLabel = getTaskStatusLabel(t, currentTask)
    scanStateVariant = getTaskStatusVariant(currentTask)
  } else if (status?.enabled) {
    scanStateLabel = t('Ready')
    scanStateVariant = 'success'
  }

  const runSingleTokenScan = () => {
    const tokenId = parseTokenLeakTokenID(tokenIdInput)
    if (tokenId === null) {
      toast.error(t('Enter a valid Token ID.'))
      return
    }
    runMutation.mutate(tokenId)
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Key Leak Detection')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          type='button'
          size='icon-sm'
          variant='ghost'
          aria-label={t('Refresh')}
          title={t('Refresh')}
          disabled={statusQuery.isFetching}
          onClick={() => void statusQuery.refetch()}
        >
          <RefreshCw
            className={statusQuery.isFetching ? 'animate-spin' : undefined}
          />
        </Button>
        <Button
          size='sm'
          variant='outline'
          render={
            <Link
              to='/system-settings/security/$section'
              params={{ section: 'token-leak-scan' }}
            />
          }
        >
          <Settings data-icon='inline-start' />
          {t('Configure')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        {statusQuery.isError ? (
          <ErrorState
            className='min-h-64 border'
            title={t('Failed to load')}
            description={
              statusQuery.error instanceof Error
                ? statusQuery.error.message
                : t('Failed to load leak scan status.')
            }
            onRetry={() => void statusQuery.refetch()}
          />
        ) : (
          <div className='space-y-5'>
            <div className='bg-border grid gap-px overflow-hidden rounded-lg border sm:grid-cols-2 xl:grid-cols-6'>
              {stats.map((stat) => (
                <div key={stat.label} className='bg-background px-3 py-2.5'>
                  <div className='text-muted-foreground text-xs'>
                    {stat.label}
                  </div>
                  <div className='mt-0.5 text-lg font-semibold tabular-nums'>
                    {stat.value}
                  </div>
                </div>
              ))}
            </div>

            <section className='space-y-3'>
              <h3 className='text-sm font-semibold'>
                {t('Token status coverage')}
              </h3>
              <div className='divide-y overflow-hidden rounded-lg border'>
                {(status?.coverage_by_status ?? []).map((coverage) => (
                  <div
                    key={coverage.status}
                    className='grid gap-2 px-3 py-2.5 text-sm sm:grid-cols-[minmax(8rem,1fr)_auto_auto_minmax(11rem,auto)] sm:items-center'
                  >
                    <span className='font-medium'>
                      {getCoverageStatusLabel(t, coverage.status)}
                    </span>
                    <span className='text-muted-foreground'>
                      {t('Total')}: {coverage.total_tokens}
                    </span>
                    <span className='text-muted-foreground'>
                      {t('Pending')}: {coverage.pending_tokens}
                    </span>
                    <span className='text-muted-foreground sm:text-right'>
                      {coverage.last_scan_completed_at > 0
                        ? formatTimestampToDate(coverage.last_scan_completed_at)
                        : t('Never scanned')}
                    </span>
                  </div>
                ))}
              </div>
            </section>

            <section className='space-y-3'>
              <div className='flex flex-wrap items-center justify-between gap-3'>
                <div className='min-w-0'>
                  <h3 className='text-sm font-semibold'>{t('Scan task')}</h3>
                  <p className='text-muted-foreground text-xs'>
                    {taskActive
                      ? t('A scan is currently running or waiting to run.')
                      : t('No scan is running.')}
                  </p>
                </div>
                <StatusBadge
                  label={scanStateLabel}
                  variant={scanStateVariant}
                  copyable={false}
                />
              </div>

              {taskActive ? (
                <div className='space-y-2'>
                  <div className='flex items-center justify-between gap-3 text-sm'>
                    <span className='font-medium'>
                      {t('Processed {{processed}} of {{total}} tokens', {
                        processed,
                        total,
                      })}
                    </span>
                    <span className='text-muted-foreground tabular-nums'>
                      {progress}%
                    </span>
                  </div>
                  <Progress value={progress} />
                </div>
              ) : null}

              <div className='flex flex-wrap items-end gap-2'>
                <Button
                  type='button'
                  size='sm'
                  disabled={!canRun || runMutation.isPending}
                  title={
                    canRun
                      ? undefined
                      : t(
                          'Enable the scan and configure required credentials first.'
                        )
                  }
                  onClick={() => runMutation.mutate(undefined)}
                >
                  {runMutation.isPending ? (
                    <Loader2
                      data-icon='inline-start'
                      className='animate-spin'
                    />
                  ) : (
                    <ScanSearch data-icon='inline-start' />
                  )}
                  {t('Run full scan')}
                </Button>
                <form
                  className='flex min-w-0 flex-1 flex-wrap items-end gap-2 sm:flex-none'
                  onSubmit={(event) => {
                    event.preventDefault()
                    runSingleTokenScan()
                  }}
                >
                  <label className='min-w-36 flex-1 space-y-1 sm:w-48 sm:flex-none'>
                    <span className='text-muted-foreground text-xs'>
                      {t('Token ID')}
                    </span>
                    <Input
                      inputMode='numeric'
                      min={1}
                      type='number'
                      placeholder={t('Enter a Token ID')}
                      value={tokenIdInput}
                      onChange={(event) => setTokenIdInput(event.target.value)}
                    />
                  </label>
                  <Button
                    type='submit'
                    size='sm'
                    variant='outline'
                    disabled={!canRun || runMutation.isPending}
                  >
                    <ScanSearch data-icon='inline-start' />
                    {t('Scan token')}
                  </Button>
                </form>
              </div>

              {lastTask && displayLastTask ? (
                <div className='text-muted-foreground space-y-1 text-xs'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <Clock3 aria-hidden='true' className='size-3.5' />
                    <span>
                      {lastTask.type === 'token_leak_scan_manual'
                        ? t('Last manual scan')
                        : t('Last scheduled scan')}
                      : {formatTimestampToDate(lastTask.updated_at)}
                    </span>
                    <StatusBadge
                      label={getTaskStatusLabel(t, lastTask)}
                      variant={getTaskStatusVariant(lastTask)}
                      copyable={false}
                    />
                  </div>
                  {lastTask.result ? (
                    <p>
                      {t(
                        'Processed {{processed}}, found {{found}}, incomplete {{incomplete}}, failed {{failed}}',
                        {
                          processed: lastTask.result.processed,
                          found: lastTask.result.found,
                          incomplete: lastTask.result.incomplete,
                          failed: lastTask.result.failed,
                        }
                      )}
                    </p>
                  ) : null}
                </div>
              ) : null}
            </section>

            <Alert>
              <ShieldAlert aria-hidden='true' />
              <AlertTitle>{t('Coverage limitations')}</AlertTitle>
              <AlertDescription>
                {t(
                  "No finding does not guarantee that a key has never leaked. This scan only covers GitHub's currently searchable public default branches."
                )}
              </AlertDescription>
            </Alert>

            <section className='space-y-3'>
              <div>
                <h3 className='text-sm font-semibold'>{t('Leak findings')}</h3>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Only candidates confirmed by a full key comparison on this server are listed.'
                  )}
                </p>
              </div>
              <TokenLeakFindingsTable />
            </section>
          </div>
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
