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
import { zodResolver } from '@hookform/resolvers/zod'
import type { TFunction } from 'i18next'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const createSchema = (t: TFunction) =>
  z
    .object({
      enabled: z.boolean(),
      type: z.enum(['fixed', 'percent']),
      value: z.coerce.number().min(0),
      topupCountLimit: z.coerce.number().int().min(0),
    })
    .superRefine((data, ctx) => {
      if (data.type === 'percent' && data.value > 100) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['value'],
          message: t(
            'When commission type is percentage, the value must be between 0 and 100'
          ),
        })
      }
    })

type Values = z.infer<ReturnType<typeof createSchema>>

export function CommissionSettingsSection({
  defaultValues,
}: {
  defaultValues: {
    enabled: boolean
    type: 'fixed' | 'percent'
    value: number
    topupCountLimit: number
  }
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<Values>({
    resolver: zodResolver(createSchema(t)) as unknown as Resolver<Values>,
    defaultValues: {
      enabled: defaultValues.enabled,
      type: defaultValues.type,
      value: defaultValues.value,
      topupCountLimit: defaultValues.topupCountLimit,
    },
  })

  const { isDirty, isSubmitting } = form.formState
  const enabled = form.watch('enabled')
  const commissionType = form.watch('type')

  async function onSubmit(values: Values) {
    const updates: Array<{ key: string; value: string }> = []

    if (values.enabled !== defaultValues.enabled) {
      updates.push({
        key: 'commission_setting.enabled',
        value: String(values.enabled),
      })
    }

    if (values.type !== defaultValues.type) {
      updates.push({ key: 'commission_setting.type', value: values.type })
    }

    if (values.value !== defaultValues.value) {
      updates.push({
        key: 'commission_setting.value',
        value: String(values.value),
      })
    }

    if (values.topupCountLimit !== defaultValues.topupCountLimit) {
      updates.push({
        key: 'commission_setting.topup_count_limit',
        value: String(values.topupCountLimit),
      })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }

    form.reset(values)
  }

  return (
    <SettingsSection title={t('Referral Commission')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save referral commission settings'
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Recharge Commission')}</FormLabel>
                  <FormDescription>
                    {t(
                      'When enabled, inviters automatically earn commission when invited users make paid top-ups'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          {enabled && (
            <div className='grid gap-6 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='type'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Commission Type')}</FormLabel>
                    <Select
                      items={[
                        { value: 'percent', label: t('Percentage') },
                        { value: 'fixed', label: t('Fixed Amount') },
                      ]}
                      value={field.value}
                      onValueChange={field.onChange}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='percent'>
                            {t('Percentage')}
                          </SelectItem>
                          <SelectItem value='fixed'>
                            {t('Fixed Amount')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {commissionType === 'percent'
                        ? t(
                            'Commission = credited quota of the order × value ÷ 100'
                          )
                        : t(
                            'Each qualifying order grants a fixed quota amount'
                          )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='value'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Commission Value')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} {...field} />
                    </FormControl>
                    <FormDescription>
                      {commissionType === 'percent'
                        ? t('e.g. 10 means 10%')
                        : t('Quota amount granted per qualifying order')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='topupCountLimit'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Commission Count Limit')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        "0 means every paid top-up earns commission; N means only the invitee's first N paid orders"
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          )}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
