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
import {
  AlertTriangle,
  Loader2,
  RefreshCw,
  Search,
  Trash2,
  UserRoundCog,
} from 'lucide-react'
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
import { Switch } from '@/components/ui/switch'
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
  formatTimestampForInput,
  formatTimestampToDate,
  getEditableQuotaStep,
  parseQuotaFromDollars,
  parseTimestampFromInput,
  quotaUnitsToEditableAmount,
} from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import {
  deleteChannelUserLimitOverride,
  getChannelUserConcurrency,
  getChannelUserDailyQuota,
  getChannelUserLimitOverrides,
  getChannelUserLimitStatus,
  getChannelUserWeeklyQuota,
  searchChannelUserLimitUsers,
  setChannelUserDailyQuota,
  setChannelUserLimitOverride,
  setChannelUserWeeklyQuota,
} from '../../api'
import { channelsQueryKeys } from '../../lib'
import type {
  Channel,
  ChannelUserDailyQuotaItem,
  ChannelUserLimitStatus,
  ChannelUserLimitUser,
  ChannelUserWeeklyQuotaItem,
} from '../../types'

const PAGE_SIZE = 20
const MAX_QUOTA = 2147483647

type ChannelUserLimitsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channel: Channel | null
}

type QuotaPeriod = 'daily' | 'weekly'

type QuotaAdjustment = {
  period: QuotaPeriod
  item: ChannelUserDailyQuotaItem | ChannelUserWeeklyQuotaItem
}

function UserIdentity(props: {
  userId: number
  username: string
  displayName: string
}) {
  const primary = props.displayName || props.username || `#${props.userId}`
  const secondary =
    props.username && props.username !== primary
      ? `@${props.username}`
      : `#${props.userId}`

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

function ErrorState(props: { message: string; onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <Empty className='min-h-56'>
      <EmptyHeader>
        <EmptyTitle>{t('Failed to load user limit status')}</EmptyTitle>
        <EmptyDescription>{props.message}</EmptyDescription>
      </EmptyHeader>
      <Button variant='outline' size='sm' onClick={props.onRetry}>
        {t('Retry')}
      </Button>
    </Empty>
  )
}

function Pagination(props: {
  page: number
  total: number
  loading: boolean
  onChange: (page: number) => void
}) {
  const { t } = useTranslation()
  const totalPages = Math.max(1, Math.ceil(props.total / PAGE_SIZE))
  if (totalPages <= 1) return null

  return (
    <div className='flex items-center justify-between gap-3 pt-3'>
      <div className='text-muted-foreground text-sm'>
        {t('Page {{current}} of {{total}}', {
          current: props.page,
          total: totalPages,
        })}
      </div>
      <div className='flex gap-2'>
        <Button
          variant='outline'
          size='sm'
          disabled={props.page <= 1 || props.loading}
          onClick={() => props.onChange(props.page - 1)}
        >
          {t('Previous')}
        </Button>
        <Button
          variant='outline'
          size='sm'
          disabled={props.page >= totalPages || props.loading}
          onClick={() => props.onChange(props.page + 1)}
        >
          {t('Next')}
        </Button>
      </div>
    </div>
  )
}

function RefreshButton(props: { loading: boolean; onClick: () => void }) {
  const { t } = useTranslation()
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant='outline'
            size='icon-sm'
            disabled={props.loading}
            onClick={props.onClick}
            aria-label={t('Refresh')}
          />
        }
      >
        <RefreshCw
          className={props.loading ? 'size-4 animate-spin' : 'size-4'}
        />
      </TooltipTrigger>
      <TooltipContent>{t('Refresh')}</TooltipContent>
    </Tooltip>
  )
}

function OverrideBadge(props: {
  baseLimit: number
  overrideLimit?: number
  effectiveLimit: number
  expiresAt: number
  quota: boolean
}) {
  const { t } = useTranslation()
  const formatLimit = (value: number) => {
    if (value <= 0) return t('Unlimited')
    return props.quota ? formatQuota(value) : String(value)
  }
  return (
    <div className='space-y-1 text-sm'>
      <div>{formatLimit(props.effectiveLimit)}</div>
      <div className='text-muted-foreground text-xs'>
        {t('Default: {{value}}', { value: formatLimit(props.baseLimit) })}
      </div>
      {props.overrideLimit !== undefined ? (
        <div className='text-xs'>
          {t('Personal override: {{value}}', {
            value: formatLimit(props.overrideLimit),
          })}
          {props.expiresAt > 0
            ? ` · ${t('Until {{time}}', {
                time: formatTimestampToDate(props.expiresAt),
              })}`
            : ` · ${t('No expiration')}`}
        </div>
      ) : null}
    </div>
  )
}

/**
 * 展示并管理渠道用户日限、周限、并发与个人覆盖。
 *
 * @param props Dialog 开关、当前渠道和关闭回调。
 * @returns 渠道用户限制状态 Dialog。
 */
export function ChannelUserLimitsDialog(props: ChannelUserLimitsDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const currentUser = useAuthStore((state) => state.auth.user)
  const [activeTab, setActiveTab] = useState('daily-quota')
  const [dailyPage, setDailyPage] = useState(1)
  const [weeklyPage, setWeeklyPage] = useState(1)
  const [concurrencyPage, setConcurrencyPage] = useState(1)
  const [overridePage, setOverridePage] = useState(1)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [quotaAdjustment, setQuotaAdjustment] =
    useState<QuotaAdjustment | null>(null)
  const [adjustedAmount, setAdjustedAmount] = useState('')
  const [overrideUser, setOverrideUser] = useState<ChannelUserLimitUser | null>(
    null
  )
  const [concurrencyOverride, setConcurrencyOverride] = useState('')
  const [dailyOverride, setDailyOverride] = useState('')
  const [weeklyOverride, setWeeklyOverride] = useState('')
  const [hasExpiration, setHasExpiration] = useState(false)
  const [expirationInput, setExpirationInput] = useState('')
  const channelId = props.channel?.id ?? 0
  const canOperate = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.OPERATE
  )

  useEffect(() => {
    if (!props.open) {
      setQuotaAdjustment(null)
      setAdjustedAmount('')
      setOverrideUser(null)
      return
    }
    setActiveTab('daily-quota')
    setDailyPage(1)
    setWeeklyPage(1)
    setConcurrencyPage(1)
    setOverridePage(1)
    setSearchInput('')
    setSearchKeyword('')
    setOverrideUser(null)
  }, [props.open, channelId])

  const dailyQuery = useQuery({
    queryKey: channelsQueryKeys.userDailyQuota(channelId, dailyPage, PAGE_SIZE),
    queryFn: () =>
      getChannelUserDailyQuota(channelId, {
        p: dailyPage,
        page_size: PAGE_SIZE,
      }),
    enabled: props.open && activeTab === 'daily-quota' && channelId > 0,
  })
  const weeklyQuery = useQuery({
    queryKey: channelsQueryKeys.userWeeklyQuota(
      channelId,
      weeklyPage,
      PAGE_SIZE
    ),
    queryFn: () =>
      getChannelUserWeeklyQuota(channelId, {
        p: weeklyPage,
        page_size: PAGE_SIZE,
      }),
    enabled: props.open && activeTab === 'weekly-quota' && channelId > 0,
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
    enabled: props.open && activeTab === 'concurrency' && channelId > 0,
    refetchInterval:
      props.open && activeTab === 'concurrency' && channelId > 0 ? 5000 : false,
  })
  const overridesQuery = useQuery({
    queryKey: channelsQueryKeys.userLimitOverrides(
      channelId,
      overridePage,
      PAGE_SIZE
    ),
    queryFn: () =>
      getChannelUserLimitOverrides(channelId, {
        p: overridePage,
        page_size: PAGE_SIZE,
      }),
    enabled: props.open && activeTab === 'overrides' && channelId > 0,
  })
  const searchQuery = useQuery({
    queryKey: channelsQueryKeys.userLimitSearch(channelId, searchKeyword),
    queryFn: () =>
      searchChannelUserLimitUsers(channelId, {
        keyword: searchKeyword,
        p: 1,
        page_size: 10,
      }),
    enabled:
      props.open &&
      activeTab === 'overrides' &&
      channelId > 0 &&
      searchKeyword.length > 0,
  })
  const overrideStatusQuery = useQuery({
    queryKey: channelsQueryKeys.userLimitStatus(
      channelId,
      overrideUser?.id ?? 0
    ),
    queryFn: () => getChannelUserLimitStatus(channelId, overrideUser?.id ?? 0),
    enabled:
      props.open &&
      activeTab === 'overrides' &&
      overrideUser !== null &&
      channelId > 0,
  })

  useEffect(() => {
    const status = overrideStatusQuery.data
    if (!status) return
    setConcurrencyOverride(
      status.concurrency.base_limit <= 0 ||
        status.concurrency.override_limit === undefined
        ? ''
        : String(status.concurrency.override_limit)
    )
    setDailyOverride(
      status.daily_quota.base_limit <= 0 ||
        status.daily_quota.override_limit === undefined
        ? ''
        : String(quotaUnitsToEditableAmount(status.daily_quota.override_limit))
    )
    setWeeklyOverride(
      status.weekly_quota.base_limit <= 0 ||
        status.weekly_quota.override_limit === undefined
        ? ''
        : String(quotaUnitsToEditableAmount(status.weekly_quota.override_limit))
    )
    setHasExpiration(status.override_expires_at > 0)
    setExpirationInput(
      status.override_expires_at > 0
        ? formatTimestampForInput(status.override_expires_at)
        : ''
    )
  }, [overrideStatusQuery.data, overrideUser?.id])

  useEffect(() => {
    const totals = [
      { page: dailyPage, total: dailyQuery.data?.total, setPage: setDailyPage },
      {
        page: weeklyPage,
        total: weeklyQuery.data?.total,
        setPage: setWeeklyPage,
      },
      {
        page: concurrencyPage,
        total: concurrencyQuery.data?.total,
        setPage: setConcurrencyPage,
      },
      {
        page: overridePage,
        total: overridesQuery.data?.total,
        setPage: setOverridePage,
      },
    ]
    for (const item of totals) {
      if (item.total === undefined) continue
      const lastPage = Math.max(1, Math.ceil(item.total / PAGE_SIZE))
      if (item.page > lastPage) item.setPage(lastPage)
    }
  }, [
    concurrencyPage,
    concurrencyQuery.data?.total,
    dailyPage,
    dailyQuery.data?.total,
    overridePage,
    overridesQuery.data?.total,
    weeklyPage,
    weeklyQuery.data?.total,
  ])

  const adjustedQuota = useMemo(() => {
    if (adjustedAmount.trim() === '') return null
    const amount = Number(adjustedAmount)
    if (!Number.isFinite(amount) || amount < 0) return null
    const quota = parseQuotaFromDollars(amount)
    return quota >= 0 && quota <= MAX_QUOTA ? quota : null
  }, [adjustedAmount])

  const quotaAdjustmentMutation = useMutation({
    mutationFn: async () => {
      if (!quotaAdjustment || adjustedQuota === null) return
      const response =
        quotaAdjustment.period === 'daily'
          ? await setChannelUserDailyQuota(
              channelId,
              quotaAdjustment.item.user_id,
              adjustedQuota
            )
          : await setChannelUserWeeklyQuota(
              channelId,
              quotaAdjustment.item.user_id,
              adjustedQuota
            )
      if (!response.success) {
        throw new Error(response.message || t('Failed to adjust quota usage'))
      }
    },
    onSuccess: async () => {
      toast.success(t('Quota usage updated'))
      setQuotaAdjustment(null)
      setAdjustedAmount('')
      await queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.detail(channelId),
      })
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to adjust quota usage')
      )
    },
  })

  const overridePayload = useMemo(() => {
    const status = overrideStatusQuery.data
    if (!status) return null
    const concurrency = concurrencyOverride.trim()
    const daily = dailyOverride.trim()
    const weekly = weeklyOverride.trim()
    const parsedConcurrency = concurrency === '' ? null : Number(concurrency)
    const parsedDaily =
      daily === '' ? null : parseQuotaFromDollars(Number(daily))
    const parsedWeekly =
      weekly === '' ? null : parseQuotaFromDollars(Number(weekly))
    const expiresAt = hasExpiration
      ? parseTimestampFromInput(expirationInput)
      : 0
    if (
      (parsedConcurrency !== null &&
        (status.concurrency.base_limit <= 0 ||
          !Number.isInteger(parsedConcurrency) ||
          parsedConcurrency <= status.concurrency.base_limit ||
          parsedConcurrency > 1000)) ||
      (parsedDaily !== null &&
        (status.daily_quota.base_limit <= 0 ||
          !Number.isFinite(parsedDaily) ||
          parsedDaily <= status.daily_quota.base_limit ||
          parsedDaily > MAX_QUOTA)) ||
      (parsedWeekly !== null &&
        (status.weekly_quota.base_limit <= 0 ||
          !Number.isFinite(parsedWeekly) ||
          parsedWeekly <= status.weekly_quota.base_limit ||
          parsedWeekly > MAX_QUOTA)) ||
      (hasExpiration && expiresAt <= Math.floor(Date.now() / 1000))
    ) {
      return null
    }
    if (
      parsedConcurrency === null &&
      parsedDaily === null &&
      parsedWeekly === null
    ) {
      return null
    }
    return {
      user_concurrency_limit: parsedConcurrency,
      user_daily_quota_limit: parsedDaily,
      user_weekly_quota_limit: parsedWeekly,
      expires_at: expiresAt,
    }
  }, [
    concurrencyOverride,
    dailyOverride,
    expirationInput,
    hasExpiration,
    overrideStatusQuery.data,
    weeklyOverride,
  ])

  const overrideMutation = useMutation({
    mutationFn: async () => {
      if (!overrideUser || !overridePayload) return
      await setChannelUserLimitOverride(
        channelId,
        overrideUser.id,
        overridePayload
      )
    },
    onSuccess: async () => {
      toast.success(t('Personal override updated'))
      setOverrideUser(null)
      await queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.detail(channelId),
      })
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to update personal override')
      )
    },
  })

  const deleteOverrideMutation = useMutation({
    mutationFn: async () => {
      if (!overrideUser) return
      const response = await deleteChannelUserLimitOverride(
        channelId,
        overrideUser.id
      )
      if (!response.success) {
        throw new Error(response.message || t('Failed to revoke override'))
      }
    },
    onSuccess: async () => {
      toast.success(t('Personal override revoked'))
      setOverrideUser(null)
      await queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.detail(channelId),
      })
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : t('Failed to revoke override')
      )
    },
  })

  const openQuotaAdjustment = (
    period: QuotaPeriod,
    item: ChannelUserDailyQuotaItem | ChannelUserWeeklyQuotaItem
  ) => {
    setQuotaAdjustment({ period, item })
    setAdjustedAmount(String(quotaUnitsToEditableAmount(item.used_quota)))
  }

  const openOverride = (user: ChannelUserLimitUser) => {
    setOverrideUser(user)
    setConcurrencyOverride('')
    setDailyOverride('')
    setWeeklyOverride('')
    setHasExpiration(false)
    setExpirationInput('')
  }

  const renderOperateButton = (user: ChannelUserLimitUser) => (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            className='inline-flex'
            tabIndex={canOperate ? undefined : 0}
            aria-label={
              canOperate ? undefined : t('No permission to perform this action')
            }
          />
        }
      >
        <Button
          variant='outline'
          size='sm'
          disabled={!canOperate}
          onClick={() => openOverride(user)}
        >
          <UserRoundCog className='size-4' />
          {t('Temporarily increase')}
        </Button>
      </TooltipTrigger>
      {!canOperate ? (
        <TooltipContent>
          {t('No permission to perform this action')}
        </TooltipContent>
      ) : null}
    </Tooltip>
  )

  const renderQuotaTab = (
    period: QuotaPeriod,
    query: typeof dailyQuery | typeof weeklyQuery,
    page: number,
    setPage: (page: number) => void
  ) => {
    const periodLabel = period === 'daily' ? t('today') : t('this week')
    return (
      <div className='min-h-0 space-y-3'>
        <div className='flex min-h-8 items-center justify-between gap-3'>
          <div className='text-muted-foreground min-w-0 text-sm'>
            {query.data?.reset_at
              ? t('Resets at {{time}}', {
                  time: formatTimestampToDate(query.data.reset_at),
                })
              : null}
          </div>
          <RefreshButton
            loading={query.isFetching}
            onClick={() => void query.refetch()}
          />
        </div>
        {query.data?.storage_mode === 'memory' ? (
          <Alert>
            <AlertTriangle className='size-4' />
            <AlertDescription>
              {t('Usage data is available for this instance only.')}
            </AlertDescription>
          </Alert>
        ) : null}
        {query.isLoading ? (
          <LoadingState />
        ) : query.isError ? (
          <ErrorState
            message={
              query.error instanceof Error
                ? t(query.error.message)
                : t('Unknown error')
            }
            onRetry={() => void query.refetch()}
          />
        ) : query.data?.items.length ? (
          <>
            <div className='overflow-x-auto'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('User')}</TableHead>
                    <TableHead>{t('Used')}</TableHead>
                    <TableHead>{t('Effective limit')}</TableHead>
                    <TableHead>{t('Remaining')}</TableHead>
                    <TableHead className='text-right'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {query.data.items.map((item) => (
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
                        <OverrideBadge
                          baseLimit={item.base_limit ?? query.data.limit}
                          overrideLimit={item.override_limit}
                          effectiveLimit={item.limit}
                          expiresAt={item.override_expires_at ?? 0}
                          quota
                        />
                      </TableCell>
                      <TableCell>
                        {item.limit > 0
                          ? formatQuota(item.remaining_quota)
                          : t('Unlimited')}
                      </TableCell>
                      <TableCell className='text-right'>
                        <div className='flex justify-end gap-2'>
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
                                onClick={() =>
                                  openQuotaAdjustment(period, item)
                                }
                              >
                                {t('Set usage')}
                              </Button>
                            </TooltipTrigger>
                            {!canOperate ? (
                              <TooltipContent>
                                {t('No permission to perform this action')}
                              </TooltipContent>
                            ) : null}
                          </Tooltip>
                          {renderOperateButton({
                            id: item.user_id,
                            username: item.username,
                            display_name: item.display_name,
                          })}
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            <Pagination
              page={page}
              total={query.data.total}
              loading={query.isFetching}
              onChange={setPage}
            />
          </>
        ) : (
          <Empty className='min-h-56'>
            <EmptyHeader>
              <EmptyTitle>{t('No quota usage')}</EmptyTitle>
              <EmptyDescription>
                {t('No user usage has been recorded for {{period}}.', {
                  period: periodLabel,
                })}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </div>
    )
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={props.onOpenChange}
        title={t('User limit status')}
        description={
          props.channel
            ? `${props.channel.name} · #${props.channel.id}`
            : undefined
        }
        contentHeight='72vh'
        contentClassName='sm:max-w-6xl'
        bodyClassName='h-full'
      >
        <Tabs
          value={activeTab}
          onValueChange={setActiveTab}
          className='h-full gap-3'
        >
          <TabsList className='grid w-full grid-cols-4'>
            <TabsTrigger value='daily-quota'>{t('Daily quota')}</TabsTrigger>
            <TabsTrigger value='weekly-quota'>{t('Weekly quota')}</TabsTrigger>
            <TabsTrigger value='concurrency'>
              {t('Current concurrency')}
            </TabsTrigger>
            <TabsTrigger value='overrides'>
              {t('Personal overrides')}
            </TabsTrigger>
          </TabsList>

          <TabsContent value='daily-quota' className='min-h-0'>
            {renderQuotaTab('daily', dailyQuery, dailyPage, setDailyPage)}
          </TabsContent>
          <TabsContent value='weekly-quota' className='min-h-0'>
            {renderQuotaTab('weekly', weeklyQuery, weeklyPage, setWeeklyPage)}
          </TabsContent>
          <TabsContent value='concurrency' className='min-h-0 space-y-3'>
            <div className='flex min-h-8 items-center justify-end'>
              <RefreshButton
                loading={concurrencyQuery.isFetching}
                onClick={() => void concurrencyQuery.refetch()}
              />
            </div>
            {concurrencyQuery.data?.storage_mode === 'memory' ? (
              <Alert>
                <AlertTriangle className='size-4' />
                <AlertDescription>
                  {t('Concurrency data is available for this instance only.')}
                </AlertDescription>
              </Alert>
            ) : null}
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
                      <TableHead>{t('Effective limit')}</TableHead>
                      <TableHead className='text-right'>
                        {t('Actions')}
                      </TableHead>
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
                          <OverrideBadge
                            baseLimit={
                              item.base_limit ?? concurrencyQuery.data.limit
                            }
                            overrideLimit={item.override_limit}
                            effectiveLimit={item.limit}
                            expiresAt={item.override_expires_at ?? 0}
                            quota={false}
                          />
                        </TableCell>
                        <TableCell className='text-right'>
                          {renderOperateButton({
                            id: item.user_id,
                            username: item.username,
                            display_name: item.display_name,
                          })}
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
          <TabsContent value='overrides' className='min-h-0 space-y-4'>
            <form
              className='flex gap-2'
              onSubmit={(event) => {
                event.preventDefault()
                setSearchKeyword(searchInput.trim())
              }}
            >
              <Input
                value={searchInput}
                onChange={(event) => setSearchInput(event.target.value)}
                placeholder={t('Search by user ID, username, or display name')}
                aria-label={t('Search users')}
              />
              <Button
                type='submit'
                variant='outline'
                disabled={!searchInput.trim()}
              >
                <Search className='size-4' />
                {t('Search')}
              </Button>
            </form>
            {searchKeyword ? (
              <div className='space-y-2'>
                <div className='text-sm font-medium'>{t('Search results')}</div>
                {searchQuery.isLoading ? (
                  <LoadingState />
                ) : searchQuery.data?.items.length ? (
                  <div className='divide-y rounded-md border'>
                    {searchQuery.data.items.map((user) => (
                      <div
                        key={user.id}
                        className='flex items-center justify-between gap-3 p-3'
                      >
                        <UserIdentity
                          userId={user.id}
                          username={user.username}
                          displayName={user.display_name}
                        />
                        {renderOperateButton(user)}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className='text-muted-foreground text-sm'>
                    {t('No users found')}
                  </div>
                )}
              </div>
            ) : null}
            <div className='flex items-center justify-between gap-3'>
              <div className='text-sm font-medium'>{t('Active overrides')}</div>
              <RefreshButton
                loading={overridesQuery.isFetching}
                onClick={() => void overridesQuery.refetch()}
              />
            </div>
            {overridesQuery.isLoading ? (
              <LoadingState />
            ) : overridesQuery.isError ? (
              <ErrorState
                message={
                  overridesQuery.error instanceof Error
                    ? t(overridesQuery.error.message)
                    : t('Unknown error')
                }
                onRetry={() => void overridesQuery.refetch()}
              />
            ) : overridesQuery.data?.items.length ? (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('User')}</TableHead>
                      <TableHead>{t('Concurrency')}</TableHead>
                      <TableHead>{t('Daily quota')}</TableHead>
                      <TableHead>{t('Weekly quota')}</TableHead>
                      <TableHead>{t('Expiration')}</TableHead>
                      <TableHead className='text-right'>
                        {t('Actions')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {overridesQuery.data.items.map((item) => (
                      <TableRow key={item.user.id}>
                        <TableCell>
                          <UserIdentity
                            userId={item.user.id}
                            username={item.user.username}
                            displayName={item.user.display_name}
                          />
                        </TableCell>
                        <TableCell>
                          {item.user_concurrency_limit ?? '-'}
                        </TableCell>
                        <TableCell>
                          {item.user_daily_quota_limit === undefined
                            ? '-'
                            : formatQuota(item.user_daily_quota_limit)}
                        </TableCell>
                        <TableCell>
                          {item.user_weekly_quota_limit === undefined
                            ? '-'
                            : formatQuota(item.user_weekly_quota_limit)}
                        </TableCell>
                        <TableCell>
                          {item.expires_at > 0
                            ? formatTimestampToDate(item.expires_at)
                            : t('No expiration')}
                        </TableCell>
                        <TableCell className='text-right'>
                          {renderOperateButton(item.user)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <Pagination
                  page={overridePage}
                  total={overridesQuery.data.total}
                  loading={overridesQuery.isFetching}
                  onChange={setOverridePage}
                />
              </>
            ) : (
              <Empty className='min-h-40'>
                <EmptyHeader>
                  <EmptyTitle>{t('No active overrides')}</EmptyTitle>
                  <EmptyDescription>
                    {t(
                      'Search for any user to configure an override in advance.'
                    )}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
          </TabsContent>
        </Tabs>
      </Dialog>

      <AlertDialog
        open={quotaAdjustment !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen && !quotaAdjustmentMutation.isPending) {
            setQuotaAdjustment(null)
            setAdjustedAmount('')
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {quotaAdjustment?.period === 'daily'
                ? t('Set daily used quota')
                : t('Set weekly used quota')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This only adjusts the period counter and does not change billing data.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {quotaAdjustment ? (
            <div className='space-y-3 rounded-md border p-3 text-sm'>
              <UserIdentity
                userId={quotaAdjustment.item.user_id}
                username={quotaAdjustment.item.username}
                displayName={quotaAdjustment.item.display_name}
              />
              <div className='font-medium'>
                {quotaAdjustment.period === 'daily'
                  ? t('Adjust daily usage for {{user}} (ID: {{id}}).', {
                      user:
                        quotaAdjustment.item.display_name ||
                        quotaAdjustment.item.username ||
                        `#${quotaAdjustment.item.user_id}`,
                      id: quotaAdjustment.item.user_id,
                    })
                  : t('Adjust weekly usage for {{user}} (ID: {{id}}).', {
                      user:
                        quotaAdjustment.item.display_name ||
                        quotaAdjustment.item.username ||
                        `#${quotaAdjustment.item.user_id}`,
                      id: quotaAdjustment.item.user_id,
                    })}
              </div>
              <div className='grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-2'>
                <span className='text-muted-foreground'>
                  {t('Before adjustment')}
                </span>
                <span className='font-medium'>
                  {formatQuota(quotaAdjustment.item.used_quota)}
                </span>
                <span className='text-muted-foreground'>
                  {t('After adjustment')}
                </span>
                <span className='font-medium'>
                  {adjustedQuota === null ? '-' : formatQuota(adjustedQuota)}
                </span>
              </div>
            </div>
          ) : null}
          <div className='space-y-2 py-2'>
            <Label
              htmlFor={
                quotaAdjustment?.period === 'daily'
                  ? 'channel-user-daily-quota-amount'
                  : 'channel-user-weekly-quota-amount'
              }
            >
              {t('Adjusted used amount ({{unit}})', {
                unit: getCurrencyLabel(),
              })}
            </Label>
            <Input
              id={
                quotaAdjustment?.period === 'daily'
                  ? 'channel-user-daily-quota-amount'
                  : 'channel-user-weekly-quota-amount'
              }
              type='number'
              min={0}
              step={getEditableQuotaStep()}
              value={adjustedAmount}
              onChange={(event) => setAdjustedAmount(event.target.value)}
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={quotaAdjustmentMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={
                adjustedQuota === null || quotaAdjustmentMutation.isPending
              }
              onClick={(event) => {
                event.preventDefault()
                quotaAdjustmentMutation.mutate()
              }}
            >
              {quotaAdjustmentMutation.isPending ? (
                <Loader2 className='size-4 animate-spin' />
              ) : null}
              {t('Confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={overrideUser !== null}
        onOpenChange={(nextOpen) => {
          if (
            !nextOpen &&
            !overrideMutation.isPending &&
            !deleteOverrideMutation.isPending
          ) {
            setOverrideUser(null)
          }
        }}
      >
        <AlertDialogContent className='sm:max-w-2xl'>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Personal limit override')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Overrides take effect immediately and may only increase channel defaults.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {overrideUser ? (
            <UserIdentity
              userId={overrideUser.id}
              username={overrideUser.username}
              displayName={overrideUser.display_name}
            />
          ) : null}
          {overrideStatusQuery.isLoading ? (
            <LoadingState />
          ) : overrideStatusQuery.isError ? (
            <ErrorState
              message={
                overrideStatusQuery.error instanceof Error
                  ? t(overrideStatusQuery.error.message)
                  : t('Unknown error')
              }
              onRetry={() => void overrideStatusQuery.refetch()}
            />
          ) : overrideStatusQuery.data ? (
            <OverrideEditor
              status={overrideStatusQuery.data}
              concurrencyValue={concurrencyOverride}
              dailyValue={dailyOverride}
              weeklyValue={weeklyOverride}
              hasExpiration={hasExpiration}
              expirationValue={expirationInput}
              onConcurrencyChange={setConcurrencyOverride}
              onDailyChange={setDailyOverride}
              onWeeklyChange={setWeeklyOverride}
              onExpirationToggle={setHasExpiration}
              onExpirationChange={setExpirationInput}
            />
          ) : null}
          {overridePayload === null && overrideStatusQuery.data ? (
            <p className='text-destructive text-sm'>
              {t(
                'Each override must be above the channel default and within the allowed range.'
              )}
            </p>
          ) : null}
          <AlertDialogFooter className='sm:justify-between'>
            <Button
              variant='destructive'
              disabled={
                !overrideStatusQuery.data?.override_active ||
                deleteOverrideMutation.isPending ||
                !canOperate
              }
              onClick={() => deleteOverrideMutation.mutate()}
            >
              <Trash2 className='size-4' />
              {t('Revoke override')}
            </Button>
            <div className='flex justify-end gap-2'>
              <AlertDialogCancel
                disabled={
                  overrideMutation.isPending || deleteOverrideMutation.isPending
                }
              >
                {t('Cancel')}
              </AlertDialogCancel>
              <AlertDialogAction
                disabled={
                  !canOperate ||
                  overridePayload === null ||
                  overrideMutation.isPending
                }
                onClick={(event) => {
                  event.preventDefault()
                  overrideMutation.mutate()
                }}
              >
                {overrideMutation.isPending ? (
                  <Loader2 className='size-4 animate-spin' />
                ) : null}
                {t('Save override')}
              </AlertDialogAction>
            </div>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function OverrideEditor(props: {
  status: ChannelUserLimitStatus
  concurrencyValue: string
  dailyValue: string
  weeklyValue: string
  hasExpiration: boolean
  expirationValue: string
  onConcurrencyChange: (value: string) => void
  onDailyChange: (value: string) => void
  onWeeklyChange: (value: string) => void
  onExpirationToggle: (value: boolean) => void
  onExpirationChange: (value: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-4'>
      <div className='grid gap-4 sm:grid-cols-3'>
        <div className='space-y-2'>
          <Label htmlFor='personal-concurrency'>{t('Concurrency')}</Label>
          <Input
            id='personal-concurrency'
            type='number'
            min={props.status.concurrency.base_limit + 1}
            max={1000}
            step={1}
            disabled={props.status.concurrency.base_limit <= 0}
            value={props.concurrencyValue}
            onChange={(event) => props.onConcurrencyChange(event.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Channel default: {{value}}', {
              value:
                props.status.concurrency.base_limit > 0
                  ? props.status.concurrency.base_limit
                  : t('Unlimited'),
            })}
          </p>
        </div>
        <div className='space-y-2'>
          <Label htmlFor='personal-daily'>
            {t('Daily quota ({{unit}})', { unit: getCurrencyLabel() })}
          </Label>
          <Input
            id='personal-daily'
            type='number'
            min={0}
            step={getEditableQuotaStep()}
            disabled={props.status.daily_quota.base_limit <= 0}
            value={props.dailyValue}
            onChange={(event) => props.onDailyChange(event.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Channel default: {{value}}', {
              value:
                props.status.daily_quota.base_limit > 0
                  ? formatQuota(props.status.daily_quota.base_limit)
                  : t('Unlimited'),
            })}
          </p>
        </div>
        <div className='space-y-2'>
          <Label htmlFor='personal-weekly'>
            {t('Weekly quota ({{unit}})', { unit: getCurrencyLabel() })}
          </Label>
          <Input
            id='personal-weekly'
            type='number'
            min={0}
            step={getEditableQuotaStep()}
            disabled={props.status.weekly_quota.base_limit <= 0}
            value={props.weeklyValue}
            onChange={(event) => props.onWeeklyChange(event.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Channel default: {{value}}', {
              value:
                props.status.weekly_quota.base_limit > 0
                  ? formatQuota(props.status.weekly_quota.base_limit)
                  : t('Unlimited'),
            })}
          </p>
        </div>
      </div>
      <div className='flex items-center justify-between gap-3 rounded-md border p-3'>
        <div>
          <Label htmlFor='personal-expiration'>{t('Set expiration')}</Label>
          <p className='text-muted-foreground text-xs'>
            {t('Turn off to keep the override active until it is revoked.')}
          </p>
        </div>
        <Switch
          id='personal-expiration'
          checked={props.hasExpiration}
          onCheckedChange={props.onExpirationToggle}
        />
      </div>
      {props.hasExpiration ? (
        <Input
          type='datetime-local'
          value={props.expirationValue}
          onChange={(event) => props.onExpirationChange(event.target.value)}
        />
      ) : null}
    </div>
  )
}
