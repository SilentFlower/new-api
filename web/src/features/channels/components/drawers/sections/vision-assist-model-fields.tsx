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
import { RefreshCw } from 'lucide-react'
import { useMemo } from 'react'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'

import { getChannelModelOptions } from '../../../api'
import type { ChannelFormValues } from '../../../lib'

/**
 * 渲染视觉辅助渠道与模型的联动选择控件。
 *
 * @returns 与当前渠道表单绑定的视觉辅助选择字段。
 */
export function VisionAssistModelFields() {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const selectedChannelID = useWatch({
    control: form.control,
    name: 'vision_assist_channel_id',
  })
  const selectedModel = useWatch({
    control: form.control,
    name: 'vision_assist_model',
  })
  const channelOptionsQuery = useQuery({
    queryKey: ['channel-model-options'],
    queryFn: getChannelModelOptions,
    staleTime: 60_000,
  })
  const selectedChannel = channelOptionsQuery.data?.find(
    (channel) => channel.id === selectedChannelID
  )
  const channelItems = useMemo(() => {
    const items = (channelOptionsQuery.data ?? []).map((channel) => ({
      value: String(channel.id),
      label: `${channel.name} (#${channel.id})`,
    }))
    if (
      selectedChannelID &&
      !items.some((item) => item.value === String(selectedChannelID))
    ) {
      items.push({
        value: String(selectedChannelID),
        label: t('Unavailable channel (#{{id}})', { id: selectedChannelID }),
      })
    }
    return items
  }, [channelOptionsQuery.data, selectedChannelID, t])
  const modelItems = useMemo(() => {
    const items = (selectedChannel?.models ?? []).map((model) => ({
      value: model,
      label: model,
    }))
    if (selectedModel && !items.some((item) => item.value === selectedModel)) {
      items.push({
        value: selectedModel,
        label: `${selectedModel} (${t('Unavailable')})`,
      })
    }
    return items
  }, [selectedChannel?.models, selectedModel, t])

  return (
    <div className='grid gap-4 sm:grid-cols-2'>
      <FormField
        control={form.control}
        name='vision_assist_channel_id'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Assist channel')}</FormLabel>
            <FormControl>
              <Combobox
                options={channelItems}
                value={field.value ? String(field.value) : ''}
                onValueChange={(value) => {
                  const nextChannelID = value ? Number(value) : 0
                  if (!Number.isInteger(nextChannelID)) return
                  if (nextChannelID !== field.value) {
                    form.setValue('vision_assist_model', '', {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                  field.onChange(nextChannelID)
                }}
                placeholder={
                  channelOptionsQuery.isLoading
                    ? t('Loading channels...')
                    : t('Select an assist channel')
                }
                searchPlaceholder={t('Search channels by name or ID')}
                emptyText={t('No enabled channels')}
                disabled={channelOptionsQuery.isLoading}
                openOnFocus
              />
            </FormControl>
            <FormDescription>
              {t(
                'Select the enabled channel used to call the vision assist model'
              )}
            </FormDescription>
            {channelOptionsQuery.isError && (
              <div className='text-destructive flex items-center gap-2 text-xs'>
                <span>{t('Failed to load channel options')}</span>
                <Button
                  type='button'
                  variant='ghost'
                  size='xs'
                  onClick={() => void channelOptionsQuery.refetch()}
                >
                  <RefreshCw aria-hidden='true' />
                  {t('Retry')}
                </Button>
              </div>
            )}
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name='vision_assist_model'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Assist model')}</FormLabel>
            <FormControl>
              <Combobox
                options={modelItems}
                value={field.value ?? ''}
                onValueChange={(value) => field.onChange(value ?? '')}
                placeholder={
                  selectedChannelID
                    ? t('Search or enter a model')
                    : t('Select an assist channel first')
                }
                searchPlaceholder={t('Search or enter a model')}
                emptyText={t('No models configured; enter a custom model')}
                disabled={!selectedChannelID}
                allowCustomValue
                openOnFocus
              />
            </FormControl>
            <FormDescription>
              {t('Model used to read image content')}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  )
}
