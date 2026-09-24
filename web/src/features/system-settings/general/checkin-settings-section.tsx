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
import { t as translate } from 'i18next'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
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
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { formatCurrencyFromUSD, formatQuotaWithCurrency } from '@/lib/currency'
import { useSystemConfigStore } from '@/stores/system-config-store'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const schema = z.object({
  enabled: z.boolean(),
  captchaEnabled: z.boolean(),
  captchaMode: z.enum(['adaptive', 'always']),
  captchaTrustDays: z.coerce
    .number()
    .int({ error: () => translate('Amount must be a whole number') })
    .min(1, {
      error: () =>
        translate('Count must be between {{min}} and {{max}}', {
          min: 1,
          max: 30,
        }),
    })
    .max(30, {
      error: () =>
        translate('Count must be between {{min}} and {{max}}', {
          min: 1,
          max: 30,
        }),
    }),
  captchaIpUserLimit: z.coerce
    .number()
    .int({ error: () => translate('Amount must be a whole number') })
    .min(2, {
      error: () =>
        translate('Count must be between {{min}} and {{max}}', {
          min: 2,
          max: 100,
        }),
    })
    .max(100, {
      error: () =>
        translate('Count must be between {{min}} and {{max}}', {
          min: 2,
          max: 100,
        }),
    }),
  minQuota: z.coerce.number().int().min(0),
  maxQuota: z.coerce.number().int().min(0),
})

type Values = z.infer<typeof schema>

export function CheckinSettingsSection({
  defaultValues,
}: {
  defaultValues: {
    enabled: boolean
    captchaEnabled?: boolean
    captchaMode?: 'adaptive' | 'always'
    captchaTrustDays?: number
    captchaIpUserLimit?: number
    minQuota: number
    maxQuota: number
  }
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const currency = useSystemConfigStore((state) => state.config.currency)
  const currencyDisplay = currency.quotaDisplayType !== 'TOKENS'

  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: {
      enabled: defaultValues.enabled,
      captchaEnabled: defaultValues.captchaEnabled ?? false,
      captchaMode: defaultValues.captchaMode ?? 'adaptive',
      captchaTrustDays: defaultValues.captchaTrustDays ?? 3,
      captchaIpUserLimit: defaultValues.captchaIpUserLimit ?? 5,
      minQuota: defaultValues.minQuota,
      maxQuota: defaultValues.maxQuota,
    },
  })

  const { isDirty, isSubmitting } = form.formState
  const enabled = form.watch('enabled')
  const captchaEnabled = form.watch('captchaEnabled')
  const captchaMode = form.watch('captchaMode')

  async function onSubmit(values: Values) {
    const updates: Array<{ key: string; value: string }> = []

    if (values.captchaEnabled !== (defaultValues.captchaEnabled ?? false)) {
      updates.push({
        key: 'checkin_setting.captcha_enabled',
        value: String(values.captchaEnabled),
      })
    }

    if (values.captchaMode !== (defaultValues.captchaMode ?? 'adaptive')) {
      updates.push({
        key: 'checkin_setting.captcha_mode',
        value: values.captchaMode,
      })
    }
    if (values.captchaTrustDays !== (defaultValues.captchaTrustDays ?? 3)) {
      updates.push({
        key: 'checkin_setting.captcha_trust_days',
        value: String(values.captchaTrustDays),
      })
    }
    if (values.captchaIpUserLimit !== (defaultValues.captchaIpUserLimit ?? 5)) {
      updates.push({
        key: 'checkin_setting.captcha_ip_user_limit',
        value: String(values.captchaIpUserLimit),
      })
    }

    if (values.enabled !== defaultValues.enabled) {
      updates.push({
        key: 'checkin_setting.enabled',
        value: String(values.enabled),
      })
    }

    if (values.minQuota !== defaultValues.minQuota) {
      updates.push({
        key: 'checkin_setting.min_quota',
        value: String(values.minQuota),
      })
    }

    if (values.maxQuota !== defaultValues.maxQuota) {
      updates.push({
        key: 'checkin_setting.max_quota',
        value: String(values.maxQuota),
      })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      const result = await updateOption.mutateAsync(update)
      if (!result.success) return
    }

    form.reset(values)
  }

  return (
    <SettingsSection title={t('Check-in Settings')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save check-in settings'
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable check-in feature')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Allow users to check in daily for random quota rewards'
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

          <Alert>
            <AlertDescription>
              <p>
                {t(
                  'Rewards are entered in quota points, not currency or model tokens. A random integer is awarded between the minimum and maximum (inclusive); equal values give a fixed reward.'
                )}
              </p>
              <p>
                {t('{{points}} quota points = 1 USD.', {
                  points: currency.quotaPerUnit.toLocaleString(),
                })}
                {currencyDisplay && (
                  <>
                    {' '}
                    {t('At the current display rate, 1 USD = {{amount}}.', {
                      amount: formatCurrencyFromUSD(1, {
                        digitsLarge: 6,
                        digitsSmall: 6,
                        abbreviate: false,
                      }),
                    })}
                  </>
                )}
              </p>
            </AlertDescription>
          </Alert>

          <FormField
            control={form.control}
            name='captchaEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Protect check-in with Tencent CAPTCHA')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'When enabled, check-in uses Tencent CAPTCHA instead of Turnstile. When disabled, the global Turnstile setting applies.'
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
          {captchaEnabled && (
            <Alert>
              <AlertDescription>
                {t(
                  'Configure Web/App and mini-program CAPTCHA credentials in SMS settings. Login and check-in share credentials for each client type, with independent protection switches.'
                )}{' '}
                <a href='/system-settings/auth/sms'>{t('SMS Settings')}</a>
              </AlertDescription>
            </Alert>
          )}

          {captchaEnabled && (
            <FormField
              control={form.control}
              name='captchaMode'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Check-in verification mode')}</FormLabel>
                  <Select
                    items={[
                      { value: 'adaptive', label: t('Adaptive verification') },
                      { value: 'always', label: t('Verify every check-in') },
                    ]}
                    value={field.value}
                    onValueChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectItem value='adaptive'>
                        {t('Adaptive verification')}
                      </SelectItem>
                      <SelectItem value='always'>
                        {t('Verify every check-in')}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {t(
                      'Adaptive verification skips CAPTCHA during the trust period unless the same-IP account threshold is reached.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}
          {captchaEnabled && captchaMode === 'adaptive' && (
            <div className='grid gap-6 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='captchaTrustDays'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('CAPTCHA trust period (days)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        max={30}
                        step={1}
                        disabled={updateOption.isPending || isSubmitting}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Set 1–30 days. Only successful Tencent CAPTCHA verification renews trust; ordinary check-ins do not.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='captchaIpUserLimit'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Same-IP account threshold')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={2}
                        max={100}
                        step={1}
                        disabled={updateOption.isPending || isSubmitting}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Set 2–100 accounts. When this many distinct accounts attempt check-in from one IP (IPv6 counts the whole /64 network) within 24 hours, CAPTCHA is required even during the trust period.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          )}

          {enabled && (
            <div className='grid gap-6 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='minQuota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Minimum check-in reward (quota points)')}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        placeholder={t('1000')}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {currencyDisplay
                        ? t('Equivalent to {{amount}}', {
                            amount: formatQuotaWithCurrency(
                              Number(field.value),
                              {
                                digitsLarge: 6,
                                digitsSmall: 6,
                                abbreviate: false,
                              }
                            ),
                          })
                        : t('Minimum quota amount awarded for check-in')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='maxQuota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Maximum check-in reward (quota points)')}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        placeholder={t('10000')}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {currencyDisplay
                        ? t('Equivalent to {{amount}}', {
                            amount: formatQuotaWithCurrency(
                              Number(field.value),
                              {
                                digitsLarge: 6,
                                digitsSmall: 6,
                                abbreviate: false,
                              }
                            ),
                          })
                        : t('Maximum quota amount awarded for check-in')}
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
