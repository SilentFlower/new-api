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
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { formatQuota } from '@/lib/format'

import {
  getChannelUserLimitStatus,
  searchChannelUserLimitUsers,
} from '../../../api'
import { channelsQueryKeys } from '../../../lib'
import type { ChannelUserLimitUser } from '../../../types'
import { ChannelPeriodOverrideEditor } from '../channel-period-override-editor'

/** @param props 渠道、可选的固定预算行与预选用户。 @returns 先选用户再提额的抽屉。 */
export function BudgetOverrideSheet(props: {
  channelId: number
  open: boolean
  budget: { id: string; name: string; limit: number } | null
  user?: ChannelUserLimitUser | null
  canOperate: boolean
  onClose: () => void
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const [keyword, setKeyword] = useState('')
  const [searchKeyword, setSearchKeyword] = useState('')
  const [user, setUser] = useState<ChannelUserLimitUser | null>(null)
  const presetUser = props.user ?? null
  useEffect(() => {
    if (props.open) {
      setUser(presetUser)
      setKeyword('')
      setSearchKeyword('')
    }
  }, [props.open, presetUser])
  const search = useQuery({
    queryKey: channelsQueryKeys.userLimitSearch(props.channelId, searchKeyword),
    queryFn: () =>
      searchChannelUserLimitUsers(props.channelId, {
        keyword: searchKeyword,
        p: 1,
        page_size: 10,
      }),
    enabled: props.open && !!searchKeyword && user === null,
  })
  const status = useQuery({
    queryKey: channelsQueryKeys.userLimitStatus(props.channelId, user?.id ?? 0),
    queryFn: () => getChannelUserLimitStatus(props.channelId, user?.id ?? 0),
    enabled: props.open && user !== null,
  })
  return (
    <Sheet
      open={props.open}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <SheetContent
        className='visible-scrollbar w-full overflow-y-auto sm:max-w-xl'
        aria-label={t('Personal budget override')}
      >
        <SheetHeader>
          <SheetTitle>{t('Personal budget override')}</SheetTitle>
          <SheetDescription>
            {props.budget
              ? `${props.budget.name} · ${t('Base limit')} ${props.budget.limit > 0 ? formatQuota(props.budget.limit) : t('Unlimited')}`
              : t(
                  'This override only raises the selected budget for this user. Pool and other budgets still apply.'
                )}
          </SheetDescription>
        </SheetHeader>
        <div className='space-y-4 px-4 pb-4'>
          {user === null ? (
            <form
              className='space-y-3'
              onSubmit={(event) => {
                event.preventDefault()
                setSearchKeyword(keyword.trim())
              }}
            >
              <div className='flex gap-2'>
                <Input
                  value={keyword}
                  onChange={(event) => setKeyword(event.target.value)}
                  placeholder={t(
                    'Search by user ID, username, or display name'
                  )}
                  aria-label={t('Search users')}
                />
                <Button
                  type='submit'
                  variant='outline'
                  disabled={!keyword.trim()}
                >
                  {t('Search')}
                </Button>
              </div>
              {search.isLoading && <LoadingState />}
              {search.data && search.data.items.length === 0 && (
                <p className='text-muted-foreground text-sm'>
                  {t('No users matched.')}
                </p>
              )}
              {search.data && search.data.items.length > 0 && (
                <ul className='divide-y rounded-md border'>
                  {search.data.items.map((item) => (
                    <li
                      key={item.id}
                      className='flex items-center justify-between gap-2 px-3 py-2'
                    >
                      <span className='text-sm'>
                        <span className='font-medium'>
                          {item.display_name || item.username || `#${item.id}`}
                        </span>
                        <span className='text-muted-foreground'>
                          {' '}
                          · #{item.id}
                        </span>
                      </span>
                      <Button
                        type='button'
                        size='sm'
                        variant='outline'
                        onClick={() => setUser(item)}
                      >
                        {t('Select')}
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </form>
          ) : (
            <div className='space-y-3'>
              <div className='flex items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm'>
                <span>
                  <span className='font-medium'>
                    {user.display_name || user.username || `#${user.id}`}
                  </span>
                  <span className='text-muted-foreground'> · #{user.id}</span>
                </span>
                {presetUser === null && (
                  <Button
                    type='button'
                    size='sm'
                    variant='ghost'
                    onClick={() => setUser(null)}
                  >
                    {t('Change user')}
                  </Button>
                )}
              </div>
              {status.isLoading && <LoadingState />}
              {status.isError && (
                <p role='alert' className='text-destructive text-sm'>
                  {status.error instanceof Error
                    ? t(status.error.message)
                    : t('Unknown error')}
                </p>
              )}
              {status.data && (
                <ChannelPeriodOverrideEditor
                  key={`${props.channelId}:${user.id}:${props.budget?.id ?? ''}`}
                  status={status.data}
                  canOperate={props.canOperate}
                  defaultBudgetId={props.budget?.id}
                  onChanged={() => {
                    void status.refetch()
                    props.onChanged()
                  }}
                />
              )}
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
