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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import {
  channelLimitParentQueryKey,
  getChannelLimitParent,
  getChannelLimitParentOptions,
  saveChannelLimitParent,
} from '../../limit-parent-api'
import type {
  ChannelLimitParentChange,
  ChannelLimitParentOption,
} from '../../limit-parent-types'

type ChannelLimitParentPanelProps = {
  channelId: number
  canOperate: boolean
  blockedByDraft: boolean
  onManageParent: (channelId: number) => void
}

/** @param props 当前渠道、权限与草稿状态。 @returns 父渠道设置及只读继承说明。 */
export function ChannelLimitParentPanel(props: ChannelLimitParentPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<ChannelLimitParentOption | null>(
    null
  )
  const [target, setTarget] = useState<ChannelLimitParentChange | null>(null)
  const relation = useQuery({
    queryKey: channelLimitParentQueryKey(props.channelId),
    queryFn: () => getChannelLimitParent(props.channelId),
  })
  const options = useQuery({
    queryKey: [
      ...channelLimitParentQueryKey(props.channelId),
      'options',
      keyword,
      page,
    ],
    queryFn: () => getChannelLimitParentOptions(props.channelId, keyword, page),
    enabled: props.canOperate,
  })
  const save = useMutation({
    mutationFn: saveChannelLimitParent,
    onSuccess: (value) => {
      queryClient.setQueryData(
        channelLimitParentQueryKey(value.channel_id),
        value
      )
      setSelected(null)
      setTarget(null)
      void queryClient.invalidateQueries({ queryKey: ['channels'] })
      toast.success(t('Parent channel association updated'))
    },
    onError: () => {
      setTarget(null)
      toast.error(t('Failed to update parent channel. Reload and try again.'))
      void relation.refetch()
    },
  })

  if (relation.isPending) {
    return <p role='status'>{t('Loading parent channel')}</p>
  }
  if (!relation.data || relation.isError) {
    return (
      <Alert variant='destructive'>
        <AlertDescription>
          {t('Failed to load parent channel')}
          <Button
            variant='outline'
            size='sm'
            onClick={() => void relation.refetch()}
          >
            {t('Retry')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  }

  const current = relation.data
  const selectedId = selected?.id ?? current.parent_channel_id
  const choices = [...(options.data?.items ?? [])]
  const preserved =
    selected ?? (current.parent ? { ...current.parent, status: 0 } : null)
  if (
    preserved &&
    preserved.id > 0 &&
    !choices.some((item) => item.id === preserved.id)
  ) {
    choices.unshift(preserved)
  }
  const changed = selectedId !== current.parent_channel_id

  return (
    <section
      className='mb-3 space-y-3 rounded-lg border p-3'
      aria-label={t('Parent channel limits')}
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div>
          <p className='text-sm font-medium'>{t('Parent channel limits')}</p>
          {current.read_only && current.parent ? (
            <p className='text-muted-foreground text-sm'>
              {t('Limits inherited from {{name}} (#{{id}})', {
                name: current.parent.name,
                id: current.parent.id,
              })}
            </p>
          ) : (
            <p className='text-muted-foreground text-sm'>
              {t('This channel uses its own limits')}
            </p>
          )}
        </div>
        {current.parent && (
          <Button
            variant='outline'
            size='sm'
            onClick={() => props.onManageParent(current.parent_channel_id)}
          >
            {t('Manage parent limits')}
          </Button>
        )}
      </div>
      {current.read_only && (
        <Alert>
          <AlertDescription>
            {t('Inherited limits are read-only. Edit the parent channel.')}
          </AlertDescription>
        </Alert>
      )}
      {props.canOperate && (
        <div className='space-y-2'>
          <Label htmlFor={`limit-parent-${props.channelId}`}>
            {t('Parent channel')}
          </Label>
          <Input
            aria-label={t('Search parent channels by name or ID')}
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value)
              setPage(1)
            }}
            placeholder={t('Search parent channels by name or ID')}
          />
          <div className='flex flex-wrap items-center gap-2'>
            <NativeSelect
              id={`limit-parent-${props.channelId}`}
              className='min-w-48 flex-1'
              value={String(selectedId)}
              disabled={
                options.isPending ||
                options.isError ||
                save.isPending ||
                props.blockedByDraft
              }
              onChange={(event) => {
                const id = Number(event.target.value)
                setSelected(
                  choices.find((item) => item.id === id) ?? {
                    id: 0,
                    name: '',
                    status: 0,
                  }
                )
              }}
            >
              <NativeSelectOption value='0'>
                {t('No parent channel')}
              </NativeSelectOption>
              {choices.map((item) => (
                <NativeSelectOption key={item.id} value={String(item.id)}>
                  {item.name} (#{item.id})
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <Button
              variant='outline'
              size='sm'
              disabled={!changed || save.isPending || props.blockedByDraft}
              onClick={() =>
                setTarget({
                  channelId: props.channelId,
                  expected_revision: current.revision,
                  parent_channel_id: selectedId,
                })
              }
            >
              {t('Save')}
            </Button>
          </div>
          {options.isError && (
            <p role='alert' className='text-destructive text-sm'>
              {t('Failed to load parent channels')}{' '}
              <Button
                variant='ghost'
                size='sm'
                onClick={() => void options.refetch()}
              >
                {t('Retry')}
              </Button>
            </p>
          )}
          {options.data && options.data.total > 20 && (
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='sm'
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
              >
                {t('Previous')}
              </Button>
              <span>
                {t('Page {{current}} of {{total}}', {
                  current: page,
                  total: Math.ceil(options.data.total / 20),
                })}
              </span>
              <Button
                variant='outline'
                size='sm'
                disabled={page * 20 >= options.data.total}
                onClick={() => setPage(page + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          )}
          {props.blockedByDraft && (
            <p role='status' className='text-muted-foreground text-sm'>
              {t(
                'Save or discard budget changes before changing the parent channel.'
              )}
            </p>
          )}
          <p className='text-muted-foreground text-xs'>
            {t(
              'Pause new requests and wait for active requests to finish before changing the association. Wait at least 5 seconds after saving before resuming requests.'
            )}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t(
              'The parent keeps its current usage. Only new requests share usage after linking; past child usage is not moved.'
            )}
          </p>
        </div>
      )}
      <ConfirmDialog
        open={target !== null}
        onOpenChange={(open) => {
          if (!open && !save.isPending) setTarget(null)
        }}
        title={t('Change parent channel association?')}
        desc={t(
          'Existing limit settings are preserved. Inherited limits can only be edited on the parent channel.'
        )}
        confirmText={t('Confirm')}
        isLoading={save.isPending}
        handleConfirm={() => {
          if (target) save.mutate(target)
        }}
      />
    </section>
  )
}
