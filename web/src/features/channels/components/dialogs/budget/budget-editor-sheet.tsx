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
import { useEffect, useState } from 'react'
import type { Path, PathValue, UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import type {
  ChannelPeriodConfig,
  ChannelPeriodTarget,
} from '../../../period-types'
import { ChannelPeriodAmount } from '../channel-period-amount'
import { isNewId } from './budget-row-state'

/** @param props 表单、正在编辑的行下标与降级候选。 @returns 分组字段、行内校验的预算编辑抽屉；关闭保留草稿。 */
export function BudgetEditorSheet(props: {
  form: UseFormReturn<ChannelPeriodConfig>
  formId: string
  index: number | null
  config: ChannelPeriodConfig
  targets: ChannelPeriodTarget[]
  selfTarget?: ChannelPeriodTarget
  otherTargets: ChannelPeriodTarget[]
  disabled: boolean
  fieldKey: string
  /** 后端上次拒绝保存时对本行给出的原因；为空表示没有。 */
  rowError: string
  onClose: () => void
  onRemove: (index: number) => void
  onDuplicate: (index: number) => void
}) {
  const { t } = useTranslation()
  const index = props.index
  const row = index === null ? null : props.config.budgets[index]
  const scopeLabels = { user: t('Per user'), pool: t('Pool') }
  const scopeHelp = {
    user: t(
      'Each user gets their own allowance and can be raised individually.'
    ),
    pool: t('All users share one allowance for this channel.'),
  }
  const windowLabels = {
    daily: t('Daily'),
    weekly: t('Weekly'),
    occurrence: t('Whole period'),
  }
  const windowHelp = {
    daily: t('Resets every day at 00:00 server time.'),
    weekly: t('Resets every Monday at 00:00 server time.'),
    occurrence: t(
      'Accumulates across each occurrence of the schedule and resets when it ends.'
    ),
  }
  const modeLabels = {
    inherit: t('Use policy default'),
    reject: t('Reject'),
    fallback: t('Fallback'),
  }
  const targetModels = (channelId: number) =>
    props.targets.find(
      (item) => item.id === channelId || (channelId === 0 && item.self)
    )?.models ?? []
  const nameMissing = !!row && !row.name.trim()
  const scheduleMissing =
    !!row && row.window === 'occurrence' && !row.schedule_id
  const sameChannelNeedsModels =
    !!row &&
    row.on_exceed.mode === 'fallback' &&
    row.on_exceed.channel_id === 0 &&
    row.models.length === 0
  const targetModelMissing =
    !!row && row.on_exceed.mode === 'fallback' && !row.on_exceed.model
  const valid =
    !nameMissing &&
    !scheduleMissing &&
    !sameChannelNeedsModels &&
    !targetModelMissing
  // 文本框保留原始输入（含末尾逗号），只在切换行或点击模型按钮后与草稿同步。
  const [modelsText, setModelsText] = useState(row?.models.join(', ') ?? '')
  const modelsKey = row ? row.models.join(',') : ''
  useEffect(() => {
    setModelsText((current) => {
      const parsed = current
        .split(/[,\n]/)
        .map((item) => item.trim())
        .filter(Boolean)
        .join(',')
      return parsed === modelsKey
        ? current
        : modelsKey.split(',').filter(Boolean).join(', ')
    })
  }, [modelsKey, props.fieldKey])
  // 编辑抽屉里的分段按钮不是原生表单控件，显式标记脏值以驱动保存栏；路径与值类型由 RHF 泛型约束。
  const set = <P extends Path<ChannelPeriodConfig>>(
    name: P,
    value: PathValue<ChannelPeriodConfig, P>
  ) => props.form.setValue(name, value, { shouldDirty: true })
  return (
    <Sheet
      open={row !== null}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <SheetContent
        className='visible-scrollbar w-full overflow-y-auto sm:max-w-2xl'
        showCloseButton={false}
        aria-label={t('Budget editor')}
      >
        <SheetHeader>
          <SheetTitle>{t('Budget editor')}</SheetTitle>
          <SheetDescription>
            {t('Close to keep your draft, then preview and save the policy.')}
          </SheetDescription>
        </SheetHeader>
        {row !== null && index !== null && (
          <fieldset
            key={props.fieldKey}
            disabled={props.disabled}
            className='min-h-0 flex-1 space-y-5 px-4 pb-4'
          >
            {props.rowError && (
              <p role='alert' className='text-destructive text-sm'>
                {props.rowError}
              </p>
            )}
            <section className='space-y-3'>
              <Field>
                <FieldLabel className='grid gap-1 text-sm'>
                  {t('Budget name')}
                  <Input
                    form={props.formId}
                    required
                    maxLength={80}
                    aria-invalid={nameMissing}
                    placeholder={t('e.g. astra pool daily')}
                    {...props.form.register(`budgets.${index}.name`)}
                  />
                </FieldLabel>
                {nameMissing ? (
                  <p role='alert' className='text-destructive text-xs'>
                    {t('Budget name is required.')}
                  </p>
                ) : (
                  <p className='text-muted-foreground text-xs'>
                    {t('Display only; renaming keeps usage.')}
                  </p>
                )}
              </Field>
            </section>
            <section className='space-y-3 border-t pt-4'>
              <h4 className='text-muted-foreground text-xs font-semibold tracking-wide uppercase'>
                {t('Scope and window')}
              </h4>
              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='space-y-1.5'>
                  <span className='text-sm font-medium'>{t('Scope')}</span>
                  <div
                    role='group'
                    aria-label={t('Scope')}
                    className='flex gap-1'
                  >
                    {(['user', 'pool'] as const).map((value) => (
                      <Button
                        key={value}
                        type='button'
                        size='sm'
                        variant={row.scope === value ? 'default' : 'outline'}
                        aria-pressed={row.scope === value}
                        onClick={() => set(`budgets.${index}.scope`, value)}
                      >
                        {scopeLabels[value]}
                      </Button>
                    ))}
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {scopeHelp[row.scope]}
                  </p>
                </div>
                <div className='space-y-1.5'>
                  <span className='text-sm font-medium'>{t('Window')}</span>
                  <div
                    role='group'
                    aria-label={t('Window')}
                    className='flex gap-1'
                  >
                    {(['daily', 'weekly', 'occurrence'] as const).map(
                      (value) => (
                        <Button
                          key={value}
                          type='button'
                          size='sm'
                          variant={row.window === value ? 'default' : 'outline'}
                          aria-pressed={row.window === value}
                          onClick={() => set(`budgets.${index}.window`, value)}
                        >
                          {windowLabels[value]}
                        </Button>
                      )
                    )}
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {windowHelp[row.window]}
                  </p>
                </div>
              </div>
              <Field>
                <FieldLabel className='grid gap-1 text-sm'>
                  {t('Schedule')}
                  <NativeSelect
                    form={props.formId}
                    required={row.window === 'occurrence'}
                    aria-invalid={scheduleMissing}
                    {...props.form.register(`budgets.${index}.schedule_id`)}
                  >
                    <NativeSelectOption
                      value=''
                      disabled={row.window === 'occurrence'}
                    >
                      {row.window === 'occurrence'
                        ? t('Select a schedule')
                        : t('No schedule, always applies')}
                    </NativeSelectOption>
                    {props.config.schedules.map((item) => (
                      <NativeSelectOption key={item.id} value={item.id}>
                        {item.name || t('Untitled schedule')}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </FieldLabel>
                {scheduleMissing && (
                  <p role='alert' className='text-destructive text-xs'>
                    {t('Whole-period budgets must reference a schedule.')}
                  </p>
                )}
                {!scheduleMissing &&
                  row.schedule_id &&
                  row.window !== 'occurrence' && (
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'Daily and weekly counters ignore schedules; this budget shares its counter with the unscheduled budget of the same scope and models.'
                      )}
                    </p>
                  )}
                {!scheduleMissing &&
                  (!row.schedule_id || row.window === 'occurrence') && (
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'With a schedule, the budget only applies while the schedule is active.'
                      )}
                    </p>
                  )}
              </Field>
            </section>
            <section className='space-y-2 border-t pt-4'>
              <h4 className='text-muted-foreground text-xs font-semibold tracking-wide uppercase'>
                {t('Models')}
              </h4>
              <div
                role='group'
                aria-label={t('Models')}
                className='flex flex-wrap gap-1.5'
              >
                {(props.selfTarget?.models ?? []).map((model) => {
                  const selected = row.models.includes(model)
                  return (
                    <Button
                      key={model}
                      type='button'
                      size='sm'
                      variant={selected ? 'default' : 'outline'}
                      aria-pressed={selected}
                      className='font-mono text-xs'
                      onClick={() =>
                        set(
                          `budgets.${index}.models`,
                          selected
                            ? row.models.filter((item) => item !== model)
                            : [...row.models, model]
                        )
                      }
                    >
                      {model}
                    </Button>
                  )
                })}
              </div>
              <Input
                form={props.formId}
                value={modelsText}
                placeholder={t('Blank applies to all models')}
                aria-label={t('Models')}
                onChange={(event) => {
                  setModelsText(event.target.value)
                  set(
                    `budgets.${index}.models`,
                    event.target.value
                      .split(/[,\n]/)
                      .map((item) => item.trim())
                      .filter(Boolean)
                  )
                }}
              />
              <p className='text-muted-foreground text-xs'>
                {row.models.length
                  ? t('Matched exactly against the client model name.')
                  : t(
                      'No models selected: applies to every model on this channel.'
                    )}
              </p>
            </section>
            <section className='space-y-2 border-t pt-4'>
              <h4 className='text-muted-foreground text-xs font-semibold tracking-wide uppercase'>
                {t('Limit')}
              </h4>
              <ChannelPeriodAmount
                label={t('Limit')}
                value={row.limit}
                onChange={(value) => set(`budgets.${index}.limit`, value ?? 0)}
              />
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Zero means unlimited. Changing models uses the selected model history when available; renaming or changing the limit keeps usage.'
                )}
              </p>
            </section>
            <section className='space-y-3 border-t pt-4'>
              <h4 className='text-muted-foreground text-xs font-semibold tracking-wide uppercase'>
                {t('When exceeded')}
              </h4>
              <div
                role='group'
                aria-label={t('When exceeded')}
                className='flex flex-wrap gap-1'
              >
                {(['inherit', 'reject', 'fallback'] as const).map((value) => (
                  <Button
                    key={value}
                    type='button'
                    size='sm'
                    variant={
                      row.on_exceed.mode === value ? 'default' : 'outline'
                    }
                    aria-pressed={row.on_exceed.mode === value}
                    onClick={() =>
                      set(`budgets.${index}.on_exceed`, {
                        mode: value,
                        channel_id: 0,
                        model: '',
                      })
                    }
                  >
                    {modeLabels[value]}
                  </Button>
                ))}
              </div>
              {row.on_exceed.mode === 'fallback' ? (
                <div className='grid gap-3 sm:grid-cols-2'>
                  <Field>
                    <FieldLabel className='grid gap-1 text-sm'>
                      {t('Target channel')}
                      <NativeSelect
                        form={props.formId}
                        value={row.on_exceed.channel_id}
                        onChange={(event) => {
                          set(
                            `budgets.${index}.on_exceed.channel_id`,
                            Number(event.target.value)
                          )
                          set(`budgets.${index}.on_exceed.model`, '')
                        }}
                      >
                        <NativeSelectOption
                          value={0}
                          disabled={
                            !props.selfTarget || row.models.length === 0
                          }
                        >
                          {t('This channel (switch model)')}
                        </NativeSelectOption>
                        {row.on_exceed.channel_id > 0 &&
                          !props.targets.some(
                            (item) => item.id === row.on_exceed.channel_id
                          ) && (
                            <NativeSelectOption
                              value={row.on_exceed.channel_id}
                            >
                              #{row.on_exceed.channel_id} · {t('Unavailable')}
                            </NativeSelectOption>
                          )}
                        {props.otherTargets.map((item) => (
                          <NativeSelectOption key={item.id} value={item.id}>
                            #{item.id} · {item.name}
                          </NativeSelectOption>
                        ))}
                      </NativeSelect>
                    </FieldLabel>
                    {sameChannelNeedsModels && (
                      <p role='alert' className='text-destructive text-xs'>
                        {t(
                          'Switching models on this channel requires specific models on this budget.'
                        )}
                      </p>
                    )}
                  </Field>
                  <Field>
                    <FieldLabel className='grid gap-1 text-sm'>
                      {t('Target model')}
                      <NativeSelect
                        form={props.formId}
                        required
                        aria-invalid={targetModelMissing}
                        {...props.form.register(
                          `budgets.${index}.on_exceed.model`
                        )}
                      >
                        <NativeSelectOption value='' disabled>
                          {t('Select model')}
                        </NativeSelectOption>
                        {!!row.on_exceed.model &&
                          !targetModels(row.on_exceed.channel_id).includes(
                            row.on_exceed.model
                          ) && (
                            <NativeSelectOption value={row.on_exceed.model}>
                              {row.on_exceed.model} · {t('Unavailable')}
                            </NativeSelectOption>
                          )}
                        {targetModels(row.on_exceed.channel_id)
                          .filter(
                            (model) =>
                              row.on_exceed.channel_id !== 0 ||
                              !row.models.includes(model)
                          )
                          .map((model) => (
                            <NativeSelectOption key={model} value={model}>
                              {model}
                            </NativeSelectOption>
                          ))}
                      </NativeSelect>
                    </FieldLabel>
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'Billed at the target model price. If the target is also exhausted the request is rejected; no second hop.'
                      )}
                    </p>
                  </Field>
                </div>
              ) : (
                <p className='text-muted-foreground text-xs'>
                  {row.on_exceed.mode === 'inherit'
                    ? t('Uses the default action configured for this policy.')
                    : t('Requests over the limit return 429.')}
                </p>
              )}
            </section>
            <div className='flex flex-wrap items-center gap-2 border-t pt-4'>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => set(`budgets.${index}.enabled`, !row.enabled)}
              >
                {row.enabled ? t('Disable budget') : t('Enable budget')}
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => props.onDuplicate(index)}
              >
                {t('Duplicate')}
              </Button>
              {isNewId(row.id) && (
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  className='text-destructive'
                  onClick={() => props.onRemove(index)}
                >
                  {t('Remove')}
                </Button>
              )}
              <span className='flex-1' />
              <Button type='button' disabled={!valid} onClick={props.onClose}>
                {t('Done')}
              </Button>
            </div>
          </fieldset>
        )}
      </SheetContent>
    </Sheet>
  )
}
