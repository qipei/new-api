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
import { useEffect, useMemo, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { PasswordInput } from '@/components/password-input'
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
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

/**
 * Nested objects keep the dotted FormField `name` props aligned with
 * react-hook-form's path semantics; flat dotted keys would silently diverge
 * from what zod validates on submit.
 */
const smsSchema = z.object({
  sms: z.object({
    local_only: z.boolean(),
    debug_code: z.string(),
    endpoint: z.string(),
    access_key_id: z.string(),
    access_key_secret: z.string(),
    sign_name: z.string(),
    template_code: z.string(),
    code_length: z.number().int().min(4).max(8),
    code_expire_minutes: z.number().int().min(1).max(60),
    resend_interval_sec: z.number().int().min(0).max(600),
    daily_limit: z.number().int().min(0).max(1000000),
    phone_daily_limit: z.number().int().min(0).max(1000000),
    ip_daily_limit: z.number().int().min(0).max(1000000),
  }),
  sms_captcha: z.object({
    enabled: z.boolean(),
    mini_app_id: z.string(),
    mini_app_secret_key: z.string(),
    captcha_app_id: z.string(),
    app_secret_key: z.string(),
    secret_id: z.string(),
    secret_key: z.string(),
    phone_trigger_count: z.number().int().min(0).max(1000),
    ip_trigger_count: z.number().int().min(0).max(10000),
    window_seconds: z.number().int().min(1).max(86400),
  }),
})

type SMSFormValues = z.infer<typeof smsSchema>

export type FlatSMSDefaults = {
  'sms.local_only': boolean
  'sms.debug_code': string
  'sms.endpoint': string
  'sms.access_key_id': string
  'sms.access_key_secret': string
  'sms.sign_name': string
  'sms.template_code': string
  'sms.code_length': number
  'sms.code_expire_minutes': number
  'sms.resend_interval_sec': number
  'sms.daily_limit': number
  'sms.phone_daily_limit': number
  'sms.ip_daily_limit': number
  'sms_captcha.enabled': boolean
  'sms_captcha.captcha_app_id': string
  'sms_captcha.app_secret_key': string
  'sms_captcha.mini_app_id': string
  'sms_captcha.mini_app_secret_key': string
  'sms_captcha.secret_id': string
  'sms_captcha.secret_key': string
  'sms_captcha.phone_trigger_count': number
  'sms_captcha.ip_trigger_count': number
  'sms_captcha.window_seconds': number
}

const buildFormDefaults = (defaults: FlatSMSDefaults): SMSFormValues => ({
  sms: {
    local_only: defaults['sms.local_only'],
    debug_code: defaults['sms.debug_code'] ?? '',
    endpoint: defaults['sms.endpoint'] ?? '',
    access_key_id: defaults['sms.access_key_id'] ?? '',
    access_key_secret: defaults['sms.access_key_secret'] ?? '',
    sign_name: defaults['sms.sign_name'] ?? '',
    template_code: defaults['sms.template_code'] ?? '',
    code_length: defaults['sms.code_length'] || 6,
    code_expire_minutes: defaults['sms.code_expire_minutes'] || 10,
    resend_interval_sec: defaults['sms.resend_interval_sec'] ?? 60,
    daily_limit: defaults['sms.daily_limit'] ?? 1000,
    phone_daily_limit: defaults['sms.phone_daily_limit'] ?? 10,
    ip_daily_limit: defaults['sms.ip_daily_limit'] ?? 50,
  },
  sms_captcha: {
    enabled: defaults['sms_captcha.enabled'],
    mini_app_id: defaults['sms_captcha.mini_app_id'] ?? '',
    mini_app_secret_key: '',
    captcha_app_id: defaults['sms_captcha.captcha_app_id'] ?? '',
    app_secret_key: defaults['sms_captcha.app_secret_key'] ?? '',
    secret_id: defaults['sms_captcha.secret_id'] ?? '',
    secret_key: defaults['sms_captcha.secret_key'] ?? '',
    phone_trigger_count: defaults['sms_captcha.phone_trigger_count'] ?? 3,
    ip_trigger_count: defaults['sms_captcha.ip_trigger_count'] ?? 8,
    window_seconds: defaults['sms_captcha.window_seconds'] || 600,
  },
})

const flattenFormValues = (values: SMSFormValues): FlatSMSDefaults => ({
  'sms.local_only': values.sms.local_only,
  'sms.debug_code': values.sms.debug_code,
  'sms.endpoint': values.sms.endpoint,
  'sms.access_key_id': values.sms.access_key_id,
  'sms.access_key_secret': values.sms.access_key_secret,
  'sms.sign_name': values.sms.sign_name,
  'sms.template_code': values.sms.template_code,
  'sms.code_length': values.sms.code_length,
  'sms.code_expire_minutes': values.sms.code_expire_minutes,
  'sms.resend_interval_sec': values.sms.resend_interval_sec,
  'sms.daily_limit': values.sms.daily_limit,
  'sms.phone_daily_limit': values.sms.phone_daily_limit,
  'sms.ip_daily_limit': values.sms.ip_daily_limit,
  'sms_captcha.enabled': values.sms_captcha.enabled,
  'sms_captcha.mini_app_id': values.sms_captcha.mini_app_id.trim(),
  'sms_captcha.mini_app_secret_key':
    values.sms_captcha.mini_app_secret_key.trim(),
  'sms_captcha.captcha_app_id': values.sms_captcha.captcha_app_id,
  'sms_captcha.app_secret_key': values.sms_captcha.app_secret_key,
  'sms_captcha.secret_id': values.sms_captcha.secret_id,
  'sms_captcha.secret_key': values.sms_captcha.secret_key,
  'sms_captcha.phone_trigger_count': values.sms_captcha.phone_trigger_count,
  'sms_captcha.ip_trigger_count': values.sms_captcha.ip_trigger_count,
  'sms_captcha.window_seconds': values.sms_captcha.window_seconds,
})

interface SMSSectionProps {
  defaultValues: FlatSMSDefaults
}

export function SMSSection(props: SMSSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<SMSFormValues>({
    resolver: zodResolver(smsSchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatSMSDefaults>(props.defaultValues)
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(props.defaultValues)
  )

  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = props.defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(props.defaultValues))
  }, [props.defaultValues, form])

  const localOnly = form.watch('sms.local_only')

  const onSubmit = async (values: SMSFormValues) => {
    const flattened = flattenFormValues(values)
    const changedKeys = (
      Object.keys(flattened) as Array<keyof FlatSMSDefaults>
    ).filter((key) => {
      if (key === 'sms_captcha.mini_app_secret_key' && !flattened[key]) {
        return false
      }
      return flattened[key] !== baselineRef.current[key]
    })

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    // Credentials must be persisted before the enable switch is validated.
    changedKeys.sort(
      (a, b) =>
        Number(a === 'sms_captcha.enabled') -
        Number(b === 'sms_captcha.enabled')
    )
    for (const key of changedKeys) {
      const response = await updateOption.mutateAsync({
        key,
        value: flattened[key],
      })
      if (!response.success) return
    }

    baselineRef.current = flattened
    baselineSerializedRef.current = JSON.stringify(flattened)
    form.reset(buildFormDefaults(flattened))
  }

  return (
    <SettingsSection title={t('SMS Service')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <FormField
            control={form.control}
            name='sms.local_only'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Debug Mode')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Skip the SMS provider and return a fixed code to the login flow. For local development only.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          {localOnly ? (
            <FormField
              control={form.control}
              name='sms.debug_code'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Debug Verification Code')}</FormLabel>
                  <FormControl>
                    <Input placeholder='123456' {...field} />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Code returned to the login flow while debug mode is on'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}

          <FormField
            control={form.control}
            name='sms.endpoint'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('SMS Endpoint')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder='https://dysmsapi.aliyuncs.com/'
                    {...field}
                  />
                </FormControl>
                <FormDescription>{t('Aliyun SMS API address')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='sms.access_key_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('AccessKey ID')}</FormLabel>
                <FormControl>
                  <Input placeholder='LTAI...' {...field} />
                </FormControl>
                <FormDescription>{t('Aliyun AccessKey ID')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='sms.access_key_secret'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('AccessKey Secret')}</FormLabel>
                <FormControl>
                  <PasswordInput autoComplete='new-password' {...field} />
                </FormControl>
                <FormDescription>
                  {t('Aliyun AccessKey Secret')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='sms.sign_name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('SMS Signature')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormDescription>
                  {t('The SMS signature approved by Aliyun')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='sms.template_code'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Template Code')}</FormLabel>
                <FormControl>
                  <Input placeholder='SMS_000000000' {...field} />
                </FormControl>
                <FormDescription>
                  {t('Login verification code template, variable name is code')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='grid gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='sms.code_length'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Code Length')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={4}
                      max={8}
                      step={1}
                      {...field}
                      onChange={(e) =>
                        field.onChange(Number.parseInt(e.target.value, 10) || 0)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Number of digits in the verification code')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='sms.code_expire_minutes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Code Validity')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={1}
                        max={60}
                        step={1}
                        {...field}
                        onChange={(e) =>
                          field.onChange(
                            Number.parseInt(e.target.value, 10) || 0
                          )
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('minutes')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t('How long a verification code stays valid')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='sms.resend_interval_sec'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Resend Interval')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={0}
                        max={600}
                        step={1}
                        {...field}
                        onChange={(e) =>
                          field.onChange(
                            Number.parseInt(e.target.value, 10) || 0
                          )
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('seconds')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t('Minimum gap between two code requests for one number')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='sms.daily_limit'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Daily Total Limit')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...field}
                      onChange={(e) =>
                        field.onChange(Number.parseInt(e.target.value, 10) || 0)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Maximum SMS codes the whole site sends per day. 0 means unlimited'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='sms.phone_daily_limit'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Per-number Daily Limit')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...field}
                      onChange={(e) =>
                        field.onChange(Number.parseInt(e.target.value, 10) || 0)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Maximum SMS codes one number can receive per day. 0 means unlimited'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='sms.ip_daily_limit'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Per-IP Daily Limit')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...field}
                      onChange={(e) =>
                        field.onChange(Number.parseInt(e.target.value, 10) || 0)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Maximum SMS codes one IP can request per day. 0 means unlimited'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='sms_captcha.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('SMS Anti-abuse Captcha')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Require Tencent Cloud Captcha before sending a code once the thresholds below are reached'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='sms_captcha.captcha_app_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>Web/App {t('CaptchaAppId')}</FormLabel>
                <FormControl>
                  <Input inputMode='numeric' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Tencent Cloud Captcha application ID, sent to the browser to open the captcha'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='sms_captcha.app_secret_key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>Web/App {t('AppSecretKey')}</FormLabel>
                <FormControl>
                  <PasswordInput autoComplete='new-password' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Tencent Cloud Captcha AppSecretKey, used for server-side ticket verification only and never sent to the browser'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <p className='text-muted-foreground text-sm'>
            {t(
              'Configure Web/App and mini-program CAPTCHA credentials in SMS settings. Login and check-in share credentials for each client type, with independent protection switches.'
            )}
          </p>
          <FormField
            control={form.control}
            name='sms_captcha.mini_app_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Mini-program CAPTCHA application ID')}
                </FormLabel>
                <FormControl>
                  <Input {...field} autoComplete='off' />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='sms_captcha.mini_app_secret_key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Mini-program CAPTCHA application secret')}
                </FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    type='password'
                    autoComplete='new-password'
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Leave blank to keep the saved secret. Never send this secret to the mini-program.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='sms_captcha.secret_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Tencent Cloud SecretId')}</FormLabel>
                <FormControl>
                  <PasswordInput autoComplete='new-password' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Tencent Cloud account OpenAPI SecretId used to sign DescribeCaptchaResult; this is not the captcha AppSecretKey'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='sms_captcha.secret_key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Tencent Cloud SecretKey')}</FormLabel>
                <FormControl>
                  <PasswordInput autoComplete='new-password' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Tencent Cloud account OpenAPI SecretKey used to sign DescribeCaptchaResult; this is not the captcha AppSecretKey'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='grid gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='sms_captcha.phone_trigger_count'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Per-number Trigger Count')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...field}
                      onChange={(e) =>
                        field.onChange(Number.parseInt(e.target.value, 10) || 0)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Once one number has been sent this many codes inside the window, the next request must pass the captcha first'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='sms_captcha.ip_trigger_count'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Per-IP Trigger Count')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...field}
                      onChange={(e) =>
                        field.onChange(Number.parseInt(e.target.value, 10) || 0)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Once one IP has been sent this many codes inside the window, the next request must pass the captcha first'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='sms_captcha.window_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Window Duration')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        {...field}
                        onChange={(e) =>
                          field.onChange(
                            Number.parseInt(e.target.value, 10) || 0
                          )
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('seconds')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t(
                      'How long the counters are kept. Example: window 600 with a per-number count of 3 means the 4th request within 10 minutes needs the captcha'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
