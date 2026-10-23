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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, type ReactNode } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from '@/components/ui/input-group'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { api } from '@/lib/api'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'

const endpoint = '/api/option/rewarded-ad'
const queryKey = ['rewarded-ad-settings']
export interface RewardedAdConfig {
  enabled: boolean
  reward_quota: number
  daily_limit: number
  cooldown_seconds: number
  daily_budget_quota: number
  dcloud_app_id: string
  adpid: string
  wechat_app_id: string
  s2s_auth_mode: 'sign' | 'connectCode'
  bridge_url: string
  secret_configured: boolean
  wechat_secret_configured: boolean
  s2s_key_configured: boolean
  callback_url: string
  session_ttl_seconds: number
}

const rewardedAdSchema = z
  .object({
    enabled: z.boolean(),
    reward_quota: z.number().int().min(1).max(2147483647),
    daily_limit: z.number().int().min(1).max(100),
    cooldown_seconds: z.number().int().min(0).max(86400),
    daily_budget_quota: z.number().int().min(0).max(Number.MAX_SAFE_INTEGER),
    dcloud_app_id: z.string().trim().max(128),
    adpid: z.string().trim().max(128),
    wechat_app_id: z.string().trim().max(128),
    bridge_url: z
      .string()
      .trim()
      .max(2048)
      .refine((value) => {
        if (!value) return true
        try {
          const url = new URL(value)
          return (
            url.protocol === 'https:' &&
            !!url.hostname &&
            !url.username &&
            !url.password
          )
        } catch {
          return false
        }
      }),
    secret: z.string().trim().max(256),
    wechat_app_secret: z.string().trim().max(256),
    s2s_auth_mode: z.enum(['sign', 'connectCode']),
    s2s_auth_key: z.string().trim().max(256),
  })
  .refine(
    (values) =>
      values.s2s_auth_mode !== 'sign' ||
      !values.s2s_auth_key ||
      new TextEncoder().encode(values.s2s_auth_key).length >= 32,
    { path: ['s2s_auth_key'] }
  )
type RewardedAdValues = z.infer<typeof rewardedAdSchema>

export function RewardedAdSection() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const response = await api.get<{
        success: boolean
        data: RewardedAdConfig
      }>(endpoint)
      if (!response.data.success) throw new Error(t('Failed to load settings'))
      return response.data.data
    },
  })
  let content: ReactNode
  if (query.isPending) {
    content = <p>{t('Loading...')}</p>
  } else if (query.isError) {
    content = (
      <div role='alert'>
        <p>{t('Failed to load settings')}</p>
        <Button onClick={() => query.refetch()}>{t('Retry')}</Button>
      </div>
    )
  } else {
    content = <RewardedAdForm initialValues={query.data} />
  }
  return (
    <Tabs defaultValue='mini-program'>
      <TabsList aria-label={t('Rewarded Ads')}>
        <TabsTrigger value='mini-program'>{t('Mini Program')}</TabsTrigger>
      </TabsList>
      <TabsContent value='mini-program' className='mt-2 space-y-4'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'These settings apply only to rewarded video ads in the WeChat mini program.'
          )}
        </p>
        {content}
      </TabsContent>
    </Tabs>
  )
}

function RewardedAdForm(props: { initialValues: RewardedAdConfig }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<RewardedAdValues>({
    resolver: zodResolver(rewardedAdSchema),
    defaultValues: {
      ...props.initialValues,
      s2s_auth_mode: props.initialValues.s2s_auth_mode ?? 'connectCode',
      secret: '',
      wechat_app_secret: '',
      s2s_auth_key: '',
    },
  })
  const bridgeAuthMode = useWatch({
    control: form.control,
    name: 's2s_auth_mode',
  })
  const currency = useSystemConfigStore((state) => state.config.currency)
  const [rewardQuota, dailyBudgetQuota] = useWatch({
    control: form.control,
    name: ['reward_quota', 'daily_budget_quota'],
  })
  const isDirty = form.formState.isDirty
  useEffect(() => {
    if (!isDirty) {
      form.reset({
        ...props.initialValues,
        s2s_auth_mode: props.initialValues.s2s_auth_mode ?? 'connectCode',
        secret: '',
        wechat_app_secret: '',
        s2s_auth_key: '',
      })
    }
  }, [form, isDirty, props.initialValues])
  const mutation = useMutation({
    mutationFn: async (values: RewardedAdValues) => {
      const response = await api.put<{
        success: boolean
        data: RewardedAdConfig
      }>(endpoint, values, { skipBusinessError: true, skipErrorHandler: true })
      if (!response.data.success) throw new Error(t('Failed to save settings'))
      return response.data.data
    },
    onSuccess: (data) => {
      queryClient.setQueryData(queryKey, data)
      void queryClient.invalidateQueries({ queryKey: ['status'] })
      form.reset({
        ...data,
        s2s_auth_mode: data.s2s_auth_mode ?? 'connectCode',
        secret: '',
        wechat_app_secret: '',
        s2s_auth_key: '',
      })
      toast.success(t('Settings saved successfully'))
    },
    onError: () => toast.error(t('Failed to save settings')),
  })
  const submit = form.handleSubmit((values) => {
    if (
      values.enabled &&
      (!values.adpid ||
        !values.dcloud_app_id ||
        !values.wechat_app_id ||
        !values.bridge_url ||
        values.daily_budget_quota <= 0 ||
        (!values.secret && !props.initialValues.secret_configured) ||
        (!values.wechat_app_secret &&
          !props.initialValues.wechat_secret_configured) ||
        (!values.s2s_auth_key && !props.initialValues.s2s_key_configured))
    ) {
      form.setError('root', {
        message: t(
          'Complete the ad slot, WeChat and bridge credentials and set a positive daily budget before enabling rewards.'
        ),
      })
      return
    }
    mutation.mutate(values)
  })
  const fields: {
    name: Exclude<keyof RewardedAdValues, 'enabled' | 's2s_auth_mode'>
    label: string
    help: string
    type: string
    min?: number
    max?: number
    configured?: boolean
  }[] = [
    {
      name: 'reward_quota',
      label: t('Reward per ad (quota points)'),
      help: t(
        'Added to the user balance after a verified callback. Quota points can pay for model usage; the actual token count depends on model pricing. Range: 1–2147483647.'
      ),
      type: 'number',
      min: 1,
      max: 2147483647,
    },
    {
      name: 'daily_limit',
      label: t('Daily ad reward limit'),
      help: t(
        'Maximum rewarded views per user per day in Asia/Shanghai time. Pending sessions also reserve a slot. Range: 1–100.'
      ),
      type: 'number',
      min: 1,
      max: 100,
    },
    {
      name: 'cooldown_seconds',
      label: t('Ad reward interval (seconds)'),
      help: t(
        'Minimum interval between new viewing sessions for the same user. Zero disables the interval. Range: 0–86400 seconds.'
      ),
      type: 'number',
      min: 0,
      max: 86400,
    },
    {
      name: 'daily_budget_quota',
      label: t('Daily ad reward budget (quota points)'),
      help: t(
        'Site-wide daily reward cap in Asia/Shanghai time, including pending reservations. Set a positive budget before enabling; zero stops new sessions. Maximum: 9007199254740991.'
      ),
      type: 'number',
      min: 0,
      max: Number.MAX_SAFE_INTEGER,
    },
    {
      name: 'dcloud_app_id',
      label: t('DCloud application AppID'),
      help: t(
        'The uni-app application ID, usually starting with __UNI__. Used as the DCloud identity scope in uni-open-bridge; this differs from the WeChat AppID.'
      ),
      type: 'text',
    },
    {
      name: 'adpid',
      label: t('DCloud rewarded ad slot ID'),
      help: t(
        'Enter the adpid from DCloud. The callback must match this ad slot; do not enter an app ID.'
      ),
      type: 'text',
    },
    {
      name: 'secret',
      label: t('DCloud callback secret'),
      help: t(
        'Used to verify sha256(secret:trans_id). Enter the secret for this DCloud ad slot. Leave blank to keep the saved secret; saved secrets are never displayed.'
      ),
      type: 'password',
      configured: props.initialValues.secret_configured,
    },
    {
      name: 'wechat_app_id',
      label: t('WeChat mini program AppID'),
      help: t(
        'Identifies the WeChat mini program that loads ads. Must match the mini program and uni-open-bridge configuration.'
      ),
      type: 'text',
    },
    {
      name: 'wechat_app_secret',
      label: t('WeChat mini program AppSecret'),
      help: t(
        'Used by the server to exchange the login code for the WeChat identity required by ad verification. Leave blank to keep the saved secret.'
      ),
      type: 'password',
      configured: props.initialValues.wechat_secret_configured,
    },
    {
      name: 'bridge_url',
      label: t('uni-open-bridge URL'),
      help: t(
        'The HTTPS root URL of uni-open-bridge HTTP access. The server appends /setAccessToken and /setSessionKey to synchronize WeChat credentials. Configure the bridge and secure network before enabling ads.'
      ),
      type: 'url',
    },
    {
      name: 's2s_auth_key',
      label:
        bridgeAuthMode === 'sign'
          ? t('uniCloud S2S signKey')
          : t('uniCloud S2S connectCode'),
      help:
        bridgeAuthMode === 'sign'
          ? t(
              'Enter signKey (at least 32 bytes). Leave blank to keep the saved S2S credential. Re-enter it when merging legacy settings.'
            )
          : t(
              'Enter connectCode without the CONNECTCODE prefix. Leave blank to keep the saved S2S credential. Re-enter it when merging legacy settings.'
            ),
      type: 'password',
      configured: props.initialValues.s2s_key_configured,
    },
  ]
  return (
    <SettingsSection title={t('Rewarded Ads')}>
      <SettingsForm onSubmit={submit}>
        <SettingsPageFormActions
          onSave={submit}
          isSaving={mutation.isPending}
          isSaveDisabled={mutation.isPending || !isDirty}
        />
        <FieldGroup>
          <p className='text-muted-foreground text-sm'>
            {t(
              'DCloud ad callbacks use the ad slot secret to verify sign. Rewards also require a matching user and viewing session; client playback completion never grants rewards.'
            )}
          </p>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Rewards are credited by verified server callbacks only. Client playback completion does not grant a reward.'
            )}
          </p>
          <Field orientation='horizontal'>
            <FieldLabel htmlFor='rewarded-ad-enabled'>
              {t('Enable ad token rewards')}
            </FieldLabel>
            <Controller
              name='enabled'
              control={form.control}
              render={({ field }) => (
                <Switch
                  id='rewarded-ad-enabled'
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={mutation.isPending}
                  aria-describedby='rewarded-ad-enabled-help'
                />
              )}
            />
          </Field>
          <FieldDescription id='rewarded-ad-enabled-help'>
            {t(
              'Expose the ad entry in the mini program and allow new viewing sessions. Disabling stops new sessions; existing sessions retain their saved reward amount.'
            )}
          </FieldDescription>
          {fields.map((field) => (
            <Field
              key={field.name}
              data-invalid={!!form.formState.errors[field.name]}
            >
              {field.name === 's2s_auth_key' && (
                <div className='space-y-2'>
                  <FieldLabel htmlFor='rewarded-ad-bridge-auth-mode'>
                    {t('uniCloud S2S authentication method')}
                  </FieldLabel>
                  <Controller
                    name='s2s_auth_mode'
                    control={form.control}
                    render={({ field: modeField }) => (
                      <Select
                        value={modeField.value}
                        onValueChange={modeField.onChange}
                        disabled={mutation.isPending}
                        items={[
                          { value: 'sign', label: t('Signature (sign)') },
                          {
                            value: 'connectCode',
                            label: t('Shared code (connectCode)'),
                          },
                        ]}
                      >
                        <SelectTrigger
                          id='rewarded-ad-bridge-auth-mode'
                          aria-describedby='rewarded-ad-bridge-auth-mode-help'
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectItem value='sign'>
                            {t('Signature (sign)')}
                          </SelectItem>
                          <SelectItem value='connectCode'>
                            {t('Shared code (connectCode)')}
                          </SelectItem>
                        </SelectContent>
                      </Select>
                    )}
                  />
                  <FieldDescription id='rewarded-ad-bridge-auth-mode-help'>
                    {t(
                      'S2S credentials authenticate server requests to uni-open-bridge and match uni-cloud-s2s/config.json. Ad callbacks use the separate DCloud callback secret. Keep credentials on servers only.'
                    )}
                  </FieldDescription>
                </div>
              )}
              <FieldLabel htmlFor={`rewarded-ad-${field.name}`}>
                {field.label}
              </FieldLabel>
              <Input
                id={`rewarded-ad-${field.name}`}
                type={field.type}
                min={field.min}
                max={field.max}
                step={field.type === 'number' ? 1 : undefined}
                autoComplete={
                  field.type === 'password' ? 'new-password' : undefined
                }
                disabled={mutation.isPending}
                aria-invalid={!!form.formState.errors[field.name]}
                aria-describedby={`rewarded-ad-${field.name}-help rewarded-ad-${field.name}-error`}
                {...form.register(field.name, {
                  valueAsNumber: field.type === 'number',
                })}
              />
              <FieldDescription id={`rewarded-ad-${field.name}-help`}>
                {field.help}
                {currency.quotaDisplayType !== 'TOKENS' &&
                  (field.name === 'reward_quota' ||
                    field.name === 'daily_budget_quota') && (
                    <span className='block'>
                      {t('Equivalent to {{amount}}', {
                        amount: formatQuotaWithCurrency(
                          field.name === 'reward_quota'
                            ? rewardQuota
                            : dailyBudgetQuota,
                          { digitsLarge: 6, digitsSmall: 6, abbreviate: false }
                        ),
                      })}
                    </span>
                  )}
                {field.configured !== undefined && (
                  <span className='block'>
                    {field.configured
                      ? t('Secret is configured')
                      : t('Secret is not configured')}
                  </span>
                )}
              </FieldDescription>
              {form.formState.errors[field.name] && (
                <p
                  id={`rewarded-ad-${field.name}-error`}
                  role='alert'
                  className='text-destructive text-sm'
                >
                  {t('Enter a valid value within the range described above.')}
                </p>
              )}
            </Field>
          ))}
          <Field>
            <FieldLabel htmlFor='rewarded-ad-callback'>
              {t('Server callback URL')}
            </FieldLabel>
            <InputGroup aria-label={t('Server callback URL')}>
              <InputGroupInput
                id='rewarded-ad-callback'
                className='min-w-0'
                value={props.initialValues.callback_url}
                readOnly
              />
              <InputGroupAddon align='inline-end' className='shrink-0'>
                <InputGroupButton
                  size='xs'
                  onClick={async () => {
                    try {
                      await navigator.clipboard.writeText(
                        props.initialValues.callback_url
                      )
                      toast.success(t('Copied'))
                    } catch {
                      toast.error(t('Failed to copy'))
                    }
                  }}
                >
                  {t('Copy')}
                </InputGroupButton>
              </InputGroupAddon>
            </InputGroup>
            <FieldDescription>
              {t(
                'Select traditional server callback in DCloud and enter this URL. DCloud sends a GET request; do not append parameters manually. For local testing, replace the domain with the current ngrok HTTPS domain.'
              )}
            </FieldDescription>
          </Field>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Viewing sessions expire after {{seconds}} seconds. Their user, ad slot and reward amount are fixed when created.',
              { seconds: props.initialValues.session_ttl_seconds }
            )}
          </p>
          {form.formState.errors.root && (
            <p role='alert' className='text-destructive text-sm'>
              {form.formState.errors.root.message}
            </p>
          )}
        </FieldGroup>
      </SettingsForm>
    </SettingsSection>
  )
}
