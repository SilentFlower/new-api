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
/* oxlint-disable eslint/no-nested-ternary -- 查询状态按加载、错误、数据和空态依次呈现。 */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Loader2, RefreshCw } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { getCurrencyLabel } from '@/lib/currency'
import {
  formatQuota,
  formatTimestampToDate,
  getEditableQuotaStep,
  parseQuotaFromDollars,
  quotaUnitsToEditableAmount,
} from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import {
  getChannelUserConcurrency,
  getChannelUserDailyQuota,
  setChannelUserDailyQuota,
} from '../../api'
import { channelsQueryKeys } from '../../lib'
import type { Channel, ChannelUserDailyQuotaItem } from '../../types'

const PAGE_SIZE = 20
const MAX_QUOTA = 2147483647

type ChannelUserLimitsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channel: Channel | null
}

function UserIdentity({
  userId,
  username,
  displayName,
}: {
  userId: number
  username: string
  displayName: string
}) {
  const primary = displayName || username || `#${userId}`
  const secondary =
    username && username !== primary ? `@${username}` : `#${userId}`

  return (
    <div className='min-w-36'>
      <div className='max-w-52 truncate font-medium'>{primary}</div>
      <div className='text-muted-foreground max-w-52 truncate text-xs'>
        {secondary}
      </div>
    </div>
  )
}

function LoadingState() {
  return (
    <div className='flex min-h-56 items-center justify-center'>
      <Loader2 className='text-muted-foreground size-5 animate-spin' />
    </div>
  )
}

function ErrorState({
  message,
  onRetry,
}: {
  message: string
  onRetry: () => void
}) {
  const { t } = useTranslation()
  return (
    <Empty className='min-h-56'>
      <EmptyHeader>
        <EmptyTitle>{t('Failed to load user limit status')}</EmptyTitle>
        <EmptyDescription>{message}</EmptyDescription>
      </EmptyHeader>
      <Button variant='outline' size='sm' onClick={onRetry}>
        {t('Retry')}
      </Button>
    </Empty>
  )
}

function Pagination({
  page,
  total,
  loading,
  onChange,
}: {
  page: number
  total: number
  loading: boolean
  onChange: (page: number) => void
}) {
  const { t } = useTranslation()
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  if (totalPages <= 1) return null

  return (
    <div className='flex items-center justify-between gap-3 pt-3'>
      <div className='text-muted-foreground text-sm'>
        {t('Page {{current}} of {{total}}', {
          current: page,
          total: totalPages,
        })}
      </div>
      <div className='flex gap-2'>
        <Button
          variant='outline'
          size='sm'
          disabled={page <= 1 || loading}
          onClick={() => onChange(page - 1)}
        >
          {t('Previous')}
        </Button>
        <Button
          variant='outline'
          size='sm'
          disabled={page >= totalPages || loading}
          onClick={() => onChange(page + 1)}
        >
          {t('Next')}
        </Button>
      </div>
    </div>
  )
}

function RefreshButton({
  loading,
  onClick,
}: {
  loading: boolean
  onClick: () => void
}) {
  const { t } = useTranslation()
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant='outline'
            size='icon-sm'
            disabled={loading}
            onClick={onClick}
            aria-label={t('Refresh')}
          />
        }
      >
        <RefreshCw className={loading ? 'size-4 animate-spin' : 'size-4'} />
      </TooltipTrigger>
      <TooltipContent>{t('Refresh')}</TooltipContent>
    </Tooltip>
  )
}

/**
 * 展示渠道用户当日额度和当前并发状态。
 *
 * @param props Dialog 开关、当前渠道和关闭回调。
 * @returns 渠道用户限制状态 Dialog。
 */
export function ChannelUserLimitsDialog({
  open,
  onOpenChange,
  channel,
}: ChannelUserLimitsDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const currentUser = useAuthStore((state) => state.auth.user)
  const [activeTab, setActiveTab] = useState('daily-quota')
  const [dailyPage, setDailyPage] = useState(1)
  const [concurrencyPage, setConcurrencyPage] = useState(1)
  const [adjustingUser, setAdjustingUser] =
    useState<ChannelUserDailyQuotaItem | null>(null)
  const [adjustedAmount, setAdjustedAmount] = useState('')
  const channelId = channel?.id ?? 0
  const canOperate = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.OPERATE
  )

  useEffect(() => {
    if (open) {
      setActiveTab('daily-quota')
      setDailyPage(1)
      setConcurrencyPage(1)
    }
  }, [open, channelId])

  const dailyQuery = useQuery({
    queryKey: channelsQueryKeys.userDailyQuota(channelId, dailyPage, PAGE_SIZE),
    queryFn: () =>
      getChannelUserDailyQuota(channelId, {
        p: dailyPage,
        page_size: PAGE_SIZE,
      }),
    enabled: open && activeTab === 'daily-quota' && channelId > 0,
  })

  const concurrencyQuery = useQuery({
    queryKey: channelsQueryKeys.userConcurrency(
      channelId,
      concurrencyPage,
      PAGE_SIZE
    ),
    queryFn: () =>
      getChannelUserConcurrency(channelId, {
        p: concurrencyPage,
        page_size: PAGE_SIZE,
      }),
    enabled: open && activeTab === 'concurrency' && channelId > 0,
    refetchInterval:
      open && activeTab === 'concurrency' && channelId > 0 ? 5000 : false,
  })

  useEffect(() => {
    const total = dailyQuery.data?.total
    if (total === undefined) return
    const lastPage = Math.max(1, Math.ceil(total / PAGE_SIZE))
    if (dailyPage > lastPage) {
      setDailyPage(lastPage)
    }
  }, [dailyPage, dailyQuery.data?.total])

  useEffect(() => {
    const total = concurrencyQuery.data?.total
    if (total === undefined) return
    const lastPage = Math.max(1, Math.ceil(total / PAGE_SIZE))
    if (concurrencyPage > lastPage) {
      setConcurrencyPage(lastPage)
    }
  }, [concurrencyPage, concurrencyQuery.data?.total])

  const adjustMutation = useMutation({
    mutationFn: async ({
      userId,
      usedQuota,
    }: {
      userId: number
      usedQuota: number
    }) => {
      const response = await setChannelUserDailyQuota(
        channelId,
        userId,
        usedQuota
      )
      if (!response.success) {
        throw new Error(response.message || t('Failed to adjust daily usage'))
      }
    },
    onSuccess: async () => {
      toast.success(t('Daily usage updated'))
      setAdjustingUser(null)
      setAdjustedAmount('')
      await queryClient.invalidateQueries({
        queryKey: [...channelsQueryKeys.detail(channelId), 'user-daily-quota'],
      })
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to adjust daily usage')
      )
    },
  })

  const adjustedQuota = useMemo(() => {
    if (adjustedAmount.trim() === '') return null
    const amount = Number(adjustedAmount)
    if (!Number.isFinite(amount) || amount < 0) return null
    const quota = parseQuotaFromDollars(amount)
    if (quota < 0 || quota > MAX_QUOTA) return null
    return quota
  }, [adjustedAmount])

  const openAdjustment = (item: ChannelUserDailyQuotaItem) => {
    setAdjustingUser(item)
    setAdjustedAmount(String(quotaUnitsToEditableAmount(item.used_quota)))
  }

  const confirmAdjustment = () => {
    if (!adjustingUser || adjustedQuota === null) return
    adjustMutation.mutate({
      userId: adjustingUser.user_id,
      usedQuota: adjustedQuota,
    })
  }

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={onOpenChange}
        title={t('User limit status')}
        description={channel ? `${channel.name} · #${channel.id}` : undefined}
        contentHeight='65vh'
        contentClassName='sm:max-w-5xl'
        bodyClassName='h-full'
      >
        <Tabs
          value={activeTab}
          onValueChange={setActiveTab}
          className='h-full gap-3'
        >
          <TabsList className='grid w-full grid-cols-2'>
            <TabsTrigger value='daily-quota'>{t('Daily quota')}</TabsTrigger>
            <TabsTrigger value='concurrency'>
              {t('Current concurrency')}
            </TabsTrigger>
          </TabsList>

          <TabsContent value='daily-quota' className='min-h-0 space-y-3'>
            <div className='flex min-h-8 items-center justify-between gap-3'>
              <div className='text-muted-foreground min-w-0 text-sm'>
                {dailyQuery.data?.reset_at
                  ? t('Resets at {{time}}', {
                      time: formatTimestampToDate(dailyQuery.data.reset_at),
                    })
                  : null}
              </div>
              <RefreshButton
                loading={dailyQuery.isFetching}
                onClick={() => void dailyQuery.refetch()}
              />
            </div>

            {dailyQuery.data?.storage_mode === 'memory' && (
              <Alert>
                <AlertTriangle className='size-4' />
                <AlertDescription>
                  {t('Usage data is available for this instance only.')}
                </AlertDescription>
              </Alert>
            )}

            {dailyQuery.isLoading ? (
              <LoadingState />
            ) : dailyQuery.isError ? (
              <ErrorState
                message={
                  dailyQuery.error instanceof Error
                    ? t(dailyQuery.error.message)
                    : t('Unknown error')
                }
                onRetry={() => void dailyQuery.refetch()}
              />
            ) : dailyQuery.data?.items.length ? (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('User')}</TableHead>
                      <TableHead>{t('Used today')}</TableHead>
                      <TableHead>{t('Daily limit')}</TableHead>
                      <TableHead>{t('Remaining')}</TableHead>
                      <TableHead className='text-right'>
                        {t('Actions')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {dailyQuery.data.items.map((item) => (
                      <TableRow key={item.user_id}>
                        <TableCell>
                          <UserIdentity
                            userId={item.user_id}
                            username={item.username}
                            displayName={item.display_name}
                          />
                        </TableCell>
                        <TableCell>{formatQuota(item.used_quota)}</TableCell>
                        <TableCell>
                          {item.limit > 0
                            ? formatQuota(item.limit)
                            : t('Unlimited')}
                        </TableCell>
                        <TableCell>
                          {item.limit > 0
                            ? formatQuota(item.remaining_quota)
                            : t('Unlimited')}
                        </TableCell>
                        <TableCell className='text-right'>
                          <Tooltip>
                            <TooltipTrigger
                              render={
                                <span
                                  className='inline-flex'
                                  tabIndex={canOperate ? undefined : 0}
                                  aria-label={
                                    canOperate
                                      ? undefined
                                      : t(
                                          'No permission to perform this action'
                                        )
                                  }
                                />
                              }
                            >
                              <Button
                                variant='outline'
                                size='sm'
                                disabled={!canOperate}
                                onClick={() => openAdjustment(item)}
                              >
                                {t('Set usage')}
                              </Button>
                            </TooltipTrigger>
                            {!canOperate && (
                              <TooltipContent>
                                {t('No permission to perform this action')}
                              </TooltipContent>
                            )}
                          </Tooltip>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <Pagination
                  page={dailyPage}
                  total={dailyQuery.data.total}
                  loading={dailyQuery.isFetching}
                  onChange={setDailyPage}
                />
              </>
            ) : (
              <Empty className='min-h-56'>
                <EmptyHeader>
                  <EmptyTitle>{t('No daily usage')}</EmptyTitle>
                  <EmptyDescription>
                    {t(
                      'No user usage has been recorded for this channel today.'
                    )}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
          </TabsContent>

          <TabsContent value='concurrency' className='min-h-0 space-y-3'>
            <div className='flex min-h-8 items-center justify-end'>
              <RefreshButton
                loading={concurrencyQuery.isFetching}
                onClick={() => void concurrencyQuery.refetch()}
              />
            </div>

            {concurrencyQuery.data?.storage_mode === 'memory' && (
              <Alert>
                <AlertTriangle className='size-4' />
                <AlertDescription>
                  {t('Concurrency data is available for this instance only.')}
                </AlertDescription>
              </Alert>
            )}

            {concurrencyQuery.isLoading ? (
              <LoadingState />
            ) : concurrencyQuery.isError ? (
              <ErrorState
                message={
                  concurrencyQuery.error instanceof Error
                    ? t(concurrencyQuery.error.message)
                    : t('Unknown error')
                }
                onRetry={() => void concurrencyQuery.refetch()}
              />
            ) : concurrencyQuery.data?.items.length ? (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('User')}</TableHead>
                      <TableHead>{t('Current concurrency')}</TableHead>
                      <TableHead>{t('Concurrency limit')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {concurrencyQuery.data.items.map((item) => (
                      <TableRow key={item.user_id}>
                        <TableCell>
                          <UserIdentity
                            userId={item.user_id}
                            username={item.username}
                            displayName={item.display_name}
                          />
                        </TableCell>
                        <TableCell>{item.current_concurrency}</TableCell>
                        <TableCell>
                          {item.limit > 0 ? item.limit : t('Unlimited')}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <Pagination
                  page={concurrencyPage}
                  total={concurrencyQuery.data.total}
                  loading={concurrencyQuery.isFetching}
                  onChange={setConcurrencyPage}
                />
              </>
            ) : (
              <Empty className='min-h-56'>
                <EmptyHeader>
                  <EmptyTitle>{t('No active requests')}</EmptyTitle>
                  <EmptyDescription>
                    {t('This channel has no active user requests.')}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
          </TabsContent>
        </Tabs>
      </Dialog>

      <AlertDialog
        open={adjustingUser !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen && !adjustMutation.isPending) {
            setAdjustingUser(null)
            setAdjustedAmount('')
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Set daily used quota')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                "Enter the adjusted used amount. Enter 0 to clear today's usage."
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {adjustingUser && (
            <div className='space-y-3 rounded-md border p-3 text-sm'>
              <div className='font-medium'>
                {t('Adjust daily usage for {{user}} (ID: {{id}}).', {
                  user:
                    adjustingUser.display_name ||
                    adjustingUser.username ||
                    `#${adjustingUser.user_id}`,
                  id: adjustingUser.user_id,
                })}
              </div>
              <div className='grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-2'>
                <span className='text-muted-foreground'>
                  {t('Before adjustment')}
                </span>
                <span className='font-medium'>
                  {formatQuota(adjustingUser.used_quota)}
                </span>
                <span className='text-muted-foreground'>
                  {t('After adjustment')}
                </span>
                <span className='font-medium'>
                  {adjustedQuota === null ? '-' : formatQuota(adjustedQuota)}
                </span>
              </div>
            </div>
          )}
          <div className='space-y-2 py-2'>
            <Label htmlFor='channel-user-daily-quota-amount'>
              {t('Adjusted used amount ({{unit}})', {
                unit: getCurrencyLabel(),
              })}
            </Label>
            <Input
              id='channel-user-daily-quota-amount'
              type='number'
              min={0}
              step={getEditableQuotaStep()}
              value={adjustedAmount}
              onChange={(event) => setAdjustedAmount(event.target.value)}
            />
            {adjustedAmount !== '' && adjustedQuota === null && (
              <p className='text-destructive text-sm'>
                {t('Amount must be within the allowed quota range.')}
              </p>
            )}
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={adjustMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={adjustedQuota === null || adjustMutation.isPending}
              onClick={(event) => {
                event.preventDefault()
                confirmAdjustment()
              }}
            >
              {adjustMutation.isPending && (
                <Loader2 className='size-4 animate-spin' />
              )}
              {t('Confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
