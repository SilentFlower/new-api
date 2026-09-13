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

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
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
import {
  formatQuota,
  formatTimestampForInput,
  formatTimestampToDate,
  parseTimestampFromInput,
} from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import {
  deleteChannelUserLimitOverride,
  getChannelUserConcurrency,
  getChannelUserLimitOverrides,
  getChannelUserLimitStatus,
  searchChannelUserLimitUsers,
  setChannelUserLimitOverride,
} from '../../api'
import { channelsQueryKeys } from '../../lib'
import {
  channelPeriodErrorKey,
  getChannelBudgetUserOverrides,
  saveChannelBudgetOverride,
} from '../../period-api'
import type {
  Channel,
  ChannelUserLimitStatus,
  ChannelUserLimitUser,
} from '../../types'
import { BudgetOverrideSheet } from './budget/budget-override-sheet'
import { ChannelBudgetUsageTab } from './budget/budget-usage-tab'
import { ChannelPeriodPolicyPanel } from './channel-period-policy-panel'

const PAGE_SIZE = 20

type ChannelUserLimitsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channel: Channel | null
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
}) {
  const { t } = useTranslation()
  const formatLimit = (value: number) =>
    value <= 0 ? t('Unlimited') : String(value)
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
 * 展示并管理渠道预算策略、按行用量、并发与个人覆盖。
 *
 * @param props Dialog 开关、当前渠道和关闭回调。
 * @returns 渠道用户限制状态 Dialog。
 */
export function ChannelUserLimitsDialog(props: ChannelUserLimitsDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const currentUser = useAuthStore((state) => state.auth.user)
  const [activeTab, setActiveTab] = useState('budgets')
  const [policyDirty, setPolicyDirty] = useState(false)
  const [confirmClose, setConfirmClose] = useState(false)
  const [confirmRevokeConcurrency, setConfirmRevokeConcurrency] =
    useState(false)
  const [budgetOverrideTarget, setBudgetOverrideTarget] = useState<{
    user: ChannelUserLimitUser | null
    budget: { id: string; name: string; limit: number } | null
  } | null>(null)
  const [revokeTarget, setRevokeTarget] = useState<{
    user: ChannelUserLimitUser
    budget_id: string
    budget_name: string
  } | null>(null)
  const [concurrencyPage, setConcurrencyPage] = useState(1)
  const [overridePage, setOverridePage] = useState(1)
  const [budgetOverridePage, setBudgetOverridePage] = useState(1)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [overrideUser, setOverrideUser] = useState<ChannelUserLimitUser | null>(
    null
  )
  const [concurrencyOverride, setConcurrencyOverride] = useState('')
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
      setOverrideUser(null)
      return
    }
    setActiveTab('budgets')
    setPolicyDirty(false)
    setConfirmRevokeConcurrency(false)
    setBudgetOverrideTarget(null)
    setRevokeTarget(null)
    setConcurrencyPage(1)
    setOverridePage(1)
    setBudgetOverridePage(1)
    setSearchInput('')
    setSearchKeyword('')
    setOverrideUser(null)
  }, [props.open, channelId])

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
  const budgetOverridesQuery = useQuery({
    queryKey: channelsQueryKeys.budgetUserOverrides(
      channelId,
      budgetOverridePage,
      PAGE_SIZE
    ),
    queryFn: () =>
      getChannelBudgetUserOverrides(channelId, {
        p: budgetOverridePage,
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
    setHasExpiration(status.override_expires_at > 0)
    setExpirationInput(
      status.override_expires_at > 0
        ? formatTimestampForInput(status.override_expires_at)
        : ''
    )
  }, [overrideStatusQuery.data, overrideUser?.id])

  useEffect(() => {
    const totals = [
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
      {
        page: budgetOverridePage,
        total: budgetOverridesQuery.data?.total,
        setPage: setBudgetOverridePage,
      },
    ]
    for (const item of totals) {
      if (item.total === undefined) continue
      const lastPage = Math.max(1, Math.ceil(item.total / PAGE_SIZE))
      if (item.page > lastPage) item.setPage(lastPage)
    }
  }, [
    budgetOverridePage,
    budgetOverridesQuery.data?.total,
    concurrencyPage,
    concurrencyQuery.data?.total,
    overridePage,
    overridesQuery.data?.total,
  ])

  const overridePayload = useMemo(() => {
    const status = overrideStatusQuery.data
    if (!status) return null
    const concurrency = concurrencyOverride.trim()
    const parsedConcurrency = concurrency === '' ? null : Number(concurrency)
    const expiresAt = hasExpiration
      ? parseTimestampFromInput(expirationInput)
      : 0
    if (
      parsedConcurrency === null ||
      status.concurrency.base_limit <= 0 ||
      !Number.isInteger(parsedConcurrency) ||
      parsedConcurrency <= status.concurrency.base_limit ||
      parsedConcurrency > 1000 ||
      (hasExpiration && expiresAt <= Math.floor(Date.now() / 1000))
    ) {
      return null
    }
    return {
      user_concurrency_limit: parsedConcurrency,
      expires_at: expiresAt,
    }
  }, [
    concurrencyOverride,
    expirationInput,
    hasExpiration,
    overrideStatusQuery.data,
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

  const revokeBudgetOverride = useMutation({
    mutationFn: (item: { user: ChannelUserLimitUser; budget_id: string }) =>
      saveChannelBudgetOverride(channelId, item.budget_id, item.user.id, null),
    onSuccess: () => {
      toast.success(t('Budget override revoked'))
      void budgetOverridesQuery.refetch()
      void queryClient.invalidateQueries({ queryKey: ['channels', channelId] })
    },
    onError: (reason) => toast.error(t(channelPeriodErrorKey(reason))),
  })

  const openOverride = (user: ChannelUserLimitUser) => {
    setOverrideUser(user)
    setConcurrencyOverride('')
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
          {t('Concurrency override')}
        </Button>
      </TooltipTrigger>
      {!canOperate ? (
        <TooltipContent>
          {t('No permission to perform this action')}
        </TooltipContent>
      ) : null}
    </Tooltip>
  )

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={(open) => {
          if (!open && policyDirty) {
            setConfirmClose(true)
            return
          }
          props.onOpenChange(open)
        }}
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
          <TabsList className='grid h-auto w-full grid-cols-2 gap-1 sm:grid-cols-5'>
            <TabsTrigger value='budgets'>{t('Budgets')}</TabsTrigger>
            <TabsTrigger value='schedules'>{t('Schedules')}</TabsTrigger>
            <TabsTrigger value='budget-usage'>{t('Usage')}</TabsTrigger>
            <TabsTrigger value='overrides'>
              {t('Personal overrides')}
            </TabsTrigger>
            <TabsTrigger value='concurrency'>
              {t('Current concurrency')}
            </TabsTrigger>
          </TabsList>

          {/* 预算与时段共用同一份草稿：策略面板只挂载一次，按页签切换展示区块并保留未保存改动。 */}
          <div
            role='tabpanel'
            aria-label={t('Period policy')}
            hidden={activeTab !== 'budgets' && activeTab !== 'schedules'}
            className='min-h-0 flex-1 text-sm outline-none'
          >
            {props.open && channelId > 0 && (
              <ChannelPeriodPolicyPanel
                key={channelId}
                channelId={channelId}
                section={activeTab === 'schedules' ? 'schedules' : 'budgets'}
                canOperate={canOperate}
                onDirtyChange={setPolicyDirty}
              />
            )}
          </div>
          <TabsContent value='budget-usage' className='min-h-0'>
            {props.open && activeTab === 'budget-usage' && channelId > 0 && (
              <ChannelBudgetUsageTab
                key={channelId}
                channelId={channelId}
                canOperate={canOperate}
              />
            )}
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
                        <div className='flex flex-wrap justify-end gap-2'>
                          <Button
                            variant='outline'
                            size='sm'
                            disabled={!canOperate}
                            onClick={() =>
                              setBudgetOverrideTarget({ user, budget: null })
                            }
                          >
                            {t('Budget override')}
                          </Button>
                          {renderOperateButton(user)}
                        </div>
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
            <section className='space-y-3' aria-label={t('Budget overrides')}>
              <div className='flex items-center justify-between gap-3'>
                <div>
                  <div className='text-sm font-medium'>
                    {t('Budget overrides')}
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Only per-user budgets can be raised; the amount must exceed the base limit.'
                    )}
                  </p>
                </div>
                <div className='flex items-center gap-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={!canOperate}
                    onClick={() =>
                      setBudgetOverrideTarget({ user: null, budget: null })
                    }
                  >
                    {t('Add budget override')}
                  </Button>
                  <RefreshButton
                    loading={budgetOverridesQuery.isFetching}
                    onClick={() => void budgetOverridesQuery.refetch()}
                  />
                </div>
              </div>
              {budgetOverridesQuery.isLoading ? (
                <LoadingState />
              ) : budgetOverridesQuery.isError ? (
                <ErrorState
                  message={t(channelPeriodErrorKey(budgetOverridesQuery.error))}
                  onRetry={() => void budgetOverridesQuery.refetch()}
                />
              ) : budgetOverridesQuery.data?.items.length ? (
                <>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('User')}</TableHead>
                        <TableHead>{t('Budget')}</TableHead>
                        <TableHead>{t('Default')}</TableHead>
                        <TableHead>{t('Override amount')}</TableHead>
                        <TableHead>{t('Expiration')}</TableHead>
                        <TableHead className='text-right'>
                          {t('Actions')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {budgetOverridesQuery.data.items.map((item) => (
                        <TableRow key={`${item.user.id}:${item.budget_id}`}>
                          <TableCell>
                            <UserIdentity
                              userId={item.user.id}
                              username={item.user.username}
                              displayName={item.user.display_name}
                            />
                          </TableCell>
                          <TableCell>
                            {item.budget_name || item.budget_id}
                          </TableCell>
                          <TableCell>{formatQuota(item.base_limit)}</TableCell>
                          <TableCell>{formatQuota(item.limit)}</TableCell>
                          <TableCell>
                            {item.expires_at > 0
                              ? formatTimestampToDate(item.expires_at)
                              : t('No expiration')}
                          </TableCell>
                          <TableCell className='text-right'>
                            <div className='flex justify-end gap-2'>
                              <Button
                                variant='outline'
                                size='sm'
                                disabled={!canOperate}
                                onClick={() =>
                                  setBudgetOverrideTarget({
                                    user: item.user,
                                    budget: {
                                      id: item.budget_id,
                                      name: item.budget_name,
                                      limit: item.base_limit,
                                    },
                                  })
                                }
                              >
                                {t('Edit')}
                              </Button>
                              <Button
                                variant='outline'
                                size='sm'
                                className='text-destructive'
                                disabled={
                                  !canOperate || revokeBudgetOverride.isPending
                                }
                                onClick={() =>
                                  setRevokeTarget({
                                    user: item.user,
                                    budget_id: item.budget_id,
                                    budget_name: item.budget_name,
                                  })
                                }
                              >
                                {t('Revoke')}
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                  <Pagination
                    page={budgetOverridePage}
                    total={budgetOverridesQuery.data.total}
                    loading={budgetOverridesQuery.isFetching}
                    onChange={setBudgetOverridePage}
                  />
                </>
              ) : (
                <p className='text-muted-foreground text-sm'>
                  {t('No budget overrides are active.')}
                </p>
              )}
            </section>
            <section
              className='space-y-3'
              aria-label={t('Concurrency overrides')}
            >
              <div className='flex items-center justify-between gap-3'>
                <div>
                  <div className='text-sm font-medium'>
                    {t('Concurrency overrides')}
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Raises the per-user concurrency limit of this channel; unrelated to budgets.'
                    )}
                  </p>
                </div>
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
            </section>
          </TabsContent>
        </Tabs>
      </Dialog>

      <Sheet
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
        <SheetContent
          className='visible-scrollbar w-full overflow-y-auto sm:max-w-xl'
          aria-label={t('Concurrency override')}
        >
          <SheetHeader>
            <SheetTitle>{t('Concurrency override')}</SheetTitle>
            <SheetDescription>
              {t(
                'Overrides take effect immediately and may only increase channel defaults.'
              )}
            </SheetDescription>
          </SheetHeader>
          <div className='space-y-4 px-4'>
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
                hasExpiration={hasExpiration}
                expirationValue={expirationInput}
                onConcurrencyChange={setConcurrencyOverride}
                onExpirationToggle={setHasExpiration}
                onExpirationChange={setExpirationInput}
              />
            ) : null}
            {overridePayload === null && overrideStatusQuery.data ? (
              <p className='text-destructive text-sm'>
                {t(
                  'The concurrency override must be above the channel default and within the allowed range.'
                )}
              </p>
            ) : null}
          </div>
          <SheetFooter className='flex-row flex-wrap items-center gap-2'>
            <Button
              type='button'
              variant='destructive'
              disabled={
                !overrideStatusQuery.data?.override_active ||
                deleteOverrideMutation.isPending ||
                !canOperate
              }
              onClick={() => setConfirmRevokeConcurrency(true)}
            >
              <Trash2 className='size-4' />
              {t('Revoke override')}
            </Button>
            <span className='flex-1' />
            <Button
              type='button'
              variant='outline'
              disabled={
                overrideMutation.isPending || deleteOverrideMutation.isPending
              }
              onClick={() => setOverrideUser(null)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              disabled={
                !canOperate ||
                overridePayload === null ||
                overrideMutation.isPending
              }
              onClick={() => overrideMutation.mutate()}
            >
              {overrideMutation.isPending ? (
                <Loader2 className='size-4 animate-spin' />
              ) : null}
              {t('Save override')}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
      <ConfirmDialog
        open={confirmRevokeConcurrency}
        onOpenChange={setConfirmRevokeConcurrency}
        title={t('Revoke this concurrency override?')}
        desc={
          overrideUser
            ? t(
                '{{user}} returns to the channel default concurrency immediately.',
                {
                  user:
                    overrideUser.display_name ||
                    overrideUser.username ||
                    `#${overrideUser.id}`,
                }
              )
            : ''
        }
        confirmText={t('Revoke')}
        destructive
        isLoading={deleteOverrideMutation.isPending}
        handleConfirm={() => {
          setConfirmRevokeConcurrency(false)
          deleteOverrideMutation.mutate()
        }}
      />
      <BudgetOverrideSheet
        channelId={channelId}
        open={budgetOverrideTarget !== null}
        budget={budgetOverrideTarget?.budget ?? null}
        user={budgetOverrideTarget?.user ?? null}
        canOperate={canOperate}
        onClose={() => setBudgetOverrideTarget(null)}
        onChanged={() => {
          void budgetOverridesQuery.refetch()
          void queryClient.invalidateQueries({
            queryKey: ['channels', channelId],
          })
        }}
      />
      <ConfirmDialog
        open={revokeTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRevokeTarget(null)
        }}
        title={t('Revoke this override?')}
        desc={
          revokeTarget
            ? t(
                '{{user}} returns to the base limit of {{budget}} immediately; usage already counted is kept.',
                {
                  user:
                    revokeTarget.user.display_name ||
                    revokeTarget.user.username ||
                    `#${revokeTarget.user.id}`,
                  budget: revokeTarget.budget_name || revokeTarget.budget_id,
                }
              )
            : ''
        }
        confirmText={t('Revoke')}
        destructive
        isLoading={revokeBudgetOverride.isPending}
        handleConfirm={() => {
          if (!revokeTarget) return
          const target = revokeTarget
          setRevokeTarget(null)
          revokeBudgetOverride.mutate(target)
        }}
      />
      <ConfirmDialog
        open={confirmClose}
        onOpenChange={setConfirmClose}
        title={t('Discard unsaved changes?')}
        desc={t(
          'The period policy draft has unsaved changes. Closing will discard them.'
        )}
        confirmText={t('Discard')}
        destructive
        handleConfirm={() => {
          setConfirmClose(false)
          setPolicyDirty(false)
          props.onOpenChange(false)
        }}
      />
    </>
  )
}

function OverrideEditor(props: {
  status: ChannelUserLimitStatus
  concurrencyValue: string
  hasExpiration: boolean
  expirationValue: string
  onConcurrencyChange: (value: string) => void
  onExpirationToggle: (value: boolean) => void
  onExpirationChange: (value: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-4'>
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
