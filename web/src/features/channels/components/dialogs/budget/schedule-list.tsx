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
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { formatTimestampToDate } from '@/lib/format'

import type { ChannelScheduleState } from '../../../lib/channel-schedule-state'
import type {
  ChannelBudgetRow,
  ChannelBudgetSchedule,
  ChannelPeriodConfig,
} from '../../../period-types'
import { isNewId } from './budget-row-state'

/** @param props 表单、时段、各时段的进行中/下次开始状态与引用它们的预算行。 @returns 折叠的时段列表，展开后编辑。 */
export function ScheduleList(props: {
  form: UseFormReturn<ChannelPeriodConfig>
  fields: { formKey: string }[]
  schedules: ChannelBudgetSchedule[]
  budgets: ChannelBudgetRow[]
  states: Map<string, ChannelScheduleState>
  now: number
  disabled: boolean
  onAdd: () => void
  onRemove: (index: number) => void
}) {
  const { t } = useTranslation()
  const weekdays = [
    t('Monday'),
    t('Tuesday'),
    t('Wednesday'),
    t('Thursday'),
    t('Friday'),
    t('Saturday'),
    t('Sunday'),
  ]
  const kinds = {
    date_range: t('Specific dates'),
    weekly: t('Weekly schedule'),
  }
  return (
    <section className='space-y-3' aria-label={t('Schedules')}>
      <div className='flex items-center justify-between gap-2'>
        <div>
          <h4 className='font-medium'>{t('Schedules')}</h4>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Schedules only decide when a budget applies; limits live on the budgets that reference them. Started schedules keep their time boundaries.'
            )}
          </p>
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.disabled || props.fields.length >= 64}
          onClick={props.onAdd}
        >
          {t('Add schedule')}
        </Button>
      </div>
      {props.fields.length === 0 ? (
        <p className='text-muted-foreground rounded-lg border px-4 py-6 text-center text-sm'>
          {t(
            'No schedules yet. Budgets without a schedule apply all the time.'
          )}
        </p>
      ) : (
        <Accordion className='rounded-lg border'>
          {props.fields.map((field, index) => {
            const schedule = props.schedules[index]
            if (!schedule) return null
            const isNew = isNewId(schedule.id)
            const refs = props.budgets.filter(
              (row) => row.schedule_id === schedule.id
            )
            const state = props.states.get(schedule.id)
            const started =
              !isNew &&
              schedule.kind === 'date_range' &&
              schedule.start_at > 0 &&
              schedule.start_at <= props.now
            const range =
              schedule.kind === 'date_range'
                ? `${schedule.start_local || '—'} → ${schedule.end_local || '—'}`
                : `${weekdays[schedule.start_weekday] ?? ''} ${schedule.start_time} → ${weekdays[schedule.end_weekday] ?? ''} ${schedule.end_time}`
            return (
              <AccordionItem
                key={field.formKey}
                value={schedule.id || field.formKey}
              >
                <AccordionTrigger className='px-3 py-2.5 hover:no-underline'>
                  <div className='flex w-full flex-wrap items-center gap-x-4 gap-y-1 text-left text-sm'>
                    <span className='font-medium'>
                      {schedule.name || t('Untitled schedule')}
                      {isNew && (
                        <Badge variant='outline' className='ml-2'>
                          {t('New')}
                        </Badge>
                      )}
                    </span>
                    <span className='text-muted-foreground text-xs'>
                      {kinds[schedule.kind]}
                    </span>
                    <span className='text-muted-foreground text-xs tabular-nums'>
                      {range}
                    </span>
                    <span className='flex flex-wrap gap-1'>
                      {refs.length === 0 ? (
                        <span className='text-muted-foreground text-xs'>
                          {t('Not referenced')}
                        </span>
                      ) : (
                        refs.map((row) => (
                          <Badge key={row.id} variant='secondary'>
                            {row.name || t('Untitled budget')}
                          </Badge>
                        ))
                      )}
                    </span>
                    <span className='ml-auto'>
                      {!schedule.enabled && (
                        <Badge variant='ghost'>{t('Disabled')}</Badge>
                      )}
                      {schedule.enabled && state?.active === true && (
                        <Badge variant='secondary'>{t('In progress')}</Badge>
                      )}
                      {schedule.enabled && state?.ended && (
                        <Badge variant='outline'>{t('Ended')}</Badge>
                      )}
                      {schedule.enabled &&
                        state?.active === false &&
                        !state.ended && (
                          <Badge variant='outline'>
                            {state.nextStartAt > 0
                              ? t('Not started · {{time}}', {
                                  time: formatTimestampToDate(
                                    state.nextStartAt
                                  ),
                                })
                              : t('Not started')}
                          </Badge>
                        )}
                    </span>
                  </div>
                </AccordionTrigger>
                <AccordionContent className='space-y-3 px-3 pb-4'>
                  <div className='flex items-center justify-between gap-2'>
                    <label className='flex items-center gap-2 text-sm'>
                      <Switch
                        checked={schedule.enabled}
                        disabled={props.disabled}
                        onCheckedChange={(checked) =>
                          props.form.setValue(
                            `schedules.${index}.enabled`,
                            checked,
                            {
                              shouldDirty: true,
                            }
                          )
                        }
                      />
                      {t('Enabled')}
                    </label>
                    {isNew && (
                      <Button
                        type='button'
                        variant='ghost'
                        size='sm'
                        onClick={() => props.onRemove(index)}
                      >
                        {t('Remove')}
                      </Button>
                    )}
                  </div>
                  <div className='grid gap-3 sm:grid-cols-2'>
                    <Field>
                      <FieldLabel className='grid gap-1 text-sm'>
                        {t('Schedule name')}
                        <Input
                          required
                          maxLength={80}
                          {...props.form.register(`schedules.${index}.name`)}
                        />
                      </FieldLabel>
                    </Field>
                    <Field>
                      <FieldLabel className='grid gap-1 text-sm'>
                        {t('Schedule type')}
                        <NativeSelect
                          disabled={!isNew}
                          {...props.form.register(`schedules.${index}.kind`)}
                        >
                          <NativeSelectOption value='date_range'>
                            {kinds.date_range}
                          </NativeSelectOption>
                          <NativeSelectOption value='weekly'>
                            {kinds.weekly}
                          </NativeSelectOption>
                        </NativeSelect>
                      </FieldLabel>
                    </Field>
                    {schedule.kind === 'date_range' ? (
                      <>
                        <Field>
                          <FieldLabel className='grid gap-1 text-sm'>
                            {t('Start time')}
                            <Input
                              type='datetime-local'
                              required
                              disabled={started}
                              {...props.form.register(
                                `schedules.${index}.start_local`
                              )}
                            />
                          </FieldLabel>
                        </Field>
                        <Field>
                          <FieldLabel className='grid gap-1 text-sm'>
                            {t('End time')}
                            <Input
                              type='datetime-local'
                              required
                              disabled={started}
                              {...props.form.register(
                                `schedules.${index}.end_local`
                              )}
                            />
                          </FieldLabel>
                        </Field>
                      </>
                    ) : (
                      <>
                        <Field>
                          <FieldLabel className='grid gap-1 text-sm'>
                            {t('Start weekday')}
                            <NativeSelect
                              {...props.form.register(
                                `schedules.${index}.start_weekday`,
                                {
                                  valueAsNumber: true,
                                }
                              )}
                            >
                              {weekdays.map((day, i) => (
                                <NativeSelectOption key={day} value={i}>
                                  {day}
                                </NativeSelectOption>
                              ))}
                            </NativeSelect>
                          </FieldLabel>
                          <Input
                            aria-label={t('Start time')}
                            type='time'
                            required
                            {...props.form.register(
                              `schedules.${index}.start_time`
                            )}
                          />
                        </Field>
                        <Field>
                          <FieldLabel className='grid gap-1 text-sm'>
                            {t('End weekday')}
                            <NativeSelect
                              {...props.form.register(
                                `schedules.${index}.end_weekday`,
                                {
                                  valueAsNumber: true,
                                }
                              )}
                            >
                              {weekdays.map((day, i) => (
                                <NativeSelectOption key={day} value={i}>
                                  {day}
                                </NativeSelectOption>
                              ))}
                            </NativeSelect>
                          </FieldLabel>
                          <Input
                            aria-label={t('End time')}
                            type='time'
                            required
                            {...props.form.register(
                              `schedules.${index}.end_time`
                            )}
                          />
                        </Field>
                      </>
                    )}
                  </div>
                  {started && (
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'This schedule started on {{time}}. Boundaries cannot change; disable it and create a new one instead.',
                        {
                          time: formatTimestampToDate(schedule.start_at),
                        }
                      )}
                    </p>
                  )}
                  {refs.length > 0 && (
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'Disabling this schedule pauses {{count}} budgets that reference it.',
                        {
                          count: refs.length,
                        }
                      )}
                    </p>
                  )}
                </AccordionContent>
              </AccordionItem>
            )
          })}
        </Accordion>
      )}
    </section>
  )
}
