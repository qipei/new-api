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
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { storageSchema, parseStorageJSON } from './lib/storage-schema'

type Values = z.infer<typeof storageSchema>
type Config = Omit<Values, 'access_key_id' | 'access_key_secret'> & {
  credentials_configured: boolean
}
interface Response {
  success: boolean
  message: string
  data: Config
}
const endpoint = '/api/option/object-storage'
const queryKey = ['object-storage-settings']
const defaults: Values = {
  enabled: false,
  endpoint: '',
  region: '',
  bucket: '',
  access_key_id: '',
  access_key_secret: '',
  public_endpoint: '',
  prefix: 'uploads/images/',
  url_ttl_hours: 24,
  max_file_mb: 20,
  temp_retention_hours: 1,
  temp_cleanup_interval_minutes: 30,
  max_concurrent_uploads: 2,
  requests_per_minute: 10,
  daily_limit_mb: 1024,
}
function editable(config: Config): Values {
  return Object.fromEntries(
    Object.entries(defaults).map(([key, value]) => [
      key,
      key.startsWith('access_key_')
        ? ''
        : (config[key as keyof Config] ?? value),
    ])
  ) as Values
}
export function StorageSection() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const { data } = await api.get<Response>(endpoint)
      if (!data.success) throw new Error(data.message)
      return data.data
    },
  })
  if (query.isPending) return <p>{t('Loading...')}</p>
  if (query.isError) {
    return (
      <div role='alert'>
        {t('Failed to load settings')}
        <Button onClick={() => query.refetch()}>{t('Retry')}</Button>
      </div>
    )
  }
  return <StorageForm config={query.data} />
}
function StorageForm(props: { config: Config }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<Values>({
    defaultValues: editable(props.config),
    resolver: zodResolver(storageSchema),
  })
  const [mode, setMode] = useState<'visual' | 'json'>('visual')
  const [jsonDraft, setJsonDraft] = useState('')
  const [jsonError, setJsonError] = useState(false)
  const enabled = form.watch('enabled')
  const save = useMutation({
    mutationFn: async (values: Values) => {
      const { data } = await api.put<Response>(endpoint, values)
      if (!data.success) throw new Error(data.message)
      return data.data
    },
    onSuccess: (data) => {
      form.reset(editable(data))
      setJsonDraft(JSON.stringify(editable(data), null, 2))
      setJsonError(false)
      client.setQueryData(queryKey, data)
      toast.success(t('Settings saved successfully'))
    },
    onError: (error) =>
      toast.error(error.message || t('Failed to save settings')),
  })
  const check = useMutation({
    mutationFn: async (action: 'test' | 'lifecycle') => {
      const { data } = await api.post<Response>(`${endpoint}/${action}`)
      if (!data.success) throw new Error(data.message)
      return data.message
    },
    onSuccess: (message) => toast.success(message || t('Success')),
    onError: (error) => toast.error(error.message || t('Operation failed')),
  })
  const cleanup = useMutation({
    mutationFn: async () => {
      const { data } = await api.post<{
        success: boolean
        message: string
        data: {
          deleted_files: number
          deleted_bytes: number
          active_files: number
        }
      }>(`${endpoint}/cleanup`)
      if (!data.success) throw new Error(data.message)
      return data.data
    },
    onSuccess: (data) =>
      toast.success(
        t('Deleted {{count}} expired temporary files.', {
          count: data.deleted_files,
        })
      ),
    onError: (error) => toast.error(error.message || t('Operation failed')),
  })
  const busy = save.isPending || check.isPending || cleanup.isPending
  const dirty =
    form.formState.isDirty ||
    (mode === 'json' && jsonDraft !== JSON.stringify(form.getValues(), null, 2))
  const submit = async (event?: { preventDefault(): void }) => {
    event?.preventDefault()
    if (mode === 'json') {
      try {
        const values = parseStorageJSON(
          jsonDraft,
          props.config.credentials_configured
        )
        setJsonError(false)
        save.mutate(values)
      } catch {
        setJsonError(true)
      }
      return
    }
    await form.handleSubmit((values) => {
      if (values.enabled && !props.config.credentials_configured) {
        let missing = false
        for (const name of ['access_key_id', 'access_key_secret'] as const) {
          if (!values[name].trim()) {
            form.setError(name, { type: 'required' })
            missing = true
          }
        }
        if (missing) return
      }
      save.mutate(values)
    })()
  }
  const changeMode = (next: 'visual' | 'json') => {
    if (next === mode) return
    if (next === 'json') {
      setJsonDraft(JSON.stringify(form.getValues(), null, 2))
      setJsonError(false)
      setMode(next)
      return
    }
    try {
      const values = parseStorageJSON(
        jsonDraft,
        props.config.credentials_configured
      )
      for (const name of Object.keys(defaults) as (keyof Values)[]) {
        form.setValue(name, values[name], { shouldDirty: true })
      }
      form.clearErrors()
      setJsonError(false)
      setMode(next)
    } catch {
      setJsonError(true)
    }
  }
  const fields = [
    ['endpoint', t('OSS endpoint')],
    ['region', t('OSS region')],
    ['bucket', t('OSS bucket')],
    ['access_key_id', t('Access key ID')],
    ['access_key_secret', t('Access key secret')],
    ['public_endpoint', t('Custom HTTPS domain')],
    ['prefix', t('Object prefix')],
    ['url_ttl_hours', t('URL validity (hours)')],
    ['max_file_mb', t('Maximum file size (MB)')],
    ['max_concurrent_uploads', t('Maximum concurrent uploads per instance')],
    ['requests_per_minute', t('Upload requests per minute')],
    ['daily_limit_mb', t('Daily upload limit (MB)')],
    ['temp_retention_hours', t('Local temporary file retention (hours)')],
    [
      'temp_cleanup_interval_minutes',
      t('Temporary file cleanup interval (minutes)'),
    ],
  ] as const
  return (
    <SettingsSection title={t('File Storage')}>
      <SettingsForm onSubmit={submit} autoComplete='off'>
        <SettingsPageFormActions
          onSave={submit}
          isSaving={save.isPending}
          isSaveDisabled={busy || !dirty}
        />
        <fieldset disabled={busy} className='space-y-4'>
          <p>{t('Storage provider')}: OSS</p>
          <div
            className='flex gap-2'
            role='group'
            aria-label={t('Storage editor mode')}
          >
            <Button
              type='button'
              aria-pressed={mode === 'visual'}
              onClick={() => changeMode('visual')}
            >
              {t('Visual configuration')}
            </Button>
            <Button
              type='button'
              aria-pressed={mode === 'json'}
              onClick={() => changeMode('json')}
            >
              {t('JSON configuration')}
            </Button>
          </div>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Get credentials from Alibaba Cloud RAM: Identity Management > Users > select a user > Credentials > Create AccessKey. Use a RAM user scoped to the upload bucket and prefix. The Secret is shown only when created.'
            )}
          </p>
          <a
            className='text-primary underline'
            href='https://help.aliyun.com/zh/ram/user-guide/create-an-accesskey-pair'
            target='_blank'
            rel='noopener noreferrer'
          >
            {t('AccessKey creation guide')}
          </a>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Grant PutObject, GetObject and DeleteObject on the upload prefix, and GetBucketLifecycle on the bucket for lifecycle checks.'
            )}
          </p>
          {mode === 'json' && (
            <div className='space-y-2'>
              <label htmlFor='storage-json'>{t('JSON configuration')}</label>
              <Textarea
                id='storage-json'
                className='min-h-80 font-mono'
                value={jsonDraft}
                maxLength={16000}
                aria-invalid={jsonError}
                onChange={(event) => {
                  setJsonDraft(event.target.value)
                  setJsonError(false)
                }}
              />
              <p className='text-muted-foreground text-sm'>
                {t(
                  'JSON edits the same settings as the visual form. Saved credentials are never returned; omit them or leave them blank to keep existing values. Newly entered credentials are visible in JSON.'
                )}
              </p>
            </div>
          )}
          {jsonError && (
            <p role='alert'>
              {t(
                'Invalid JSON or storage settings. Check required fields and value ranges before saving or switching modes.'
              )}
            </p>
          )}
          {mode === 'visual' && (
            <>
              <label className='flex items-center gap-2'>
                <input type='checkbox' {...form.register('enabled')} />
                {t('Enable file storage')}
              </label>
              <p role='status'>
                {props.config.credentials_configured
                  ? t('Credentials configured')
                  : t('Credentials not configured')}
              </p>
              <p className='text-muted-foreground text-sm'>
                {t('Leave credentials blank to keep existing credentials.')}
              </p>
              {fields.map(([name, label]) => {
                const numeric = typeof defaults[name] === 'number'
                let min: number | undefined
                let max: number | undefined
                if (name === 'max_concurrent_uploads') {
                  min = 1
                  max = 30
                }
                if (name === 'temp_retention_hours') {
                  min = 1
                  max = 168
                }
                if (name === 'temp_cleanup_interval_minutes') {
                  min = 5
                  max = 1440
                }
                let requirement = t('Required')
                if (name === 'public_endpoint') {
                  requirement = t('Optional')
                } else if (name.startsWith('access_key_')) {
                  requirement = props.config.credentials_configured
                    ? t('Saved; leave blank to keep')
                    : t('Required when first enabling uploads')
                } else if (['endpoint', 'region', 'bucket'].includes(name)) {
                  requirement = t('Required when enabled')
                }
                let inputType = numeric ? 'number' : 'text'
                if (name.startsWith('access_key_')) inputType = 'password'
                return (
                  <div key={name} className='space-y-2'>
                    <div className='flex items-center gap-2'>
                      <label htmlFor={`storage-${name}`}>{label}</label>
                      <span
                        className='text-muted-foreground text-xs'
                        aria-hidden='true'
                      >
                        {requirement}
                      </span>
                    </div>
                    <Input
                      id={`storage-${name}`}
                      type={inputType}
                      autoComplete={
                        name.startsWith('access_key_') ? 'new-password' : 'off'
                      }
                      {...form.register(name, { valueAsNumber: numeric })}
                      aria-invalid={!!form.formState.errors[name]}
                      aria-required={
                        name !== 'public_endpoint' &&
                        (!name.startsWith('access_key_') ||
                          (enabled && !props.config.credentials_configured)) &&
                        (!['endpoint', 'region', 'bucket'].includes(name) ||
                          enabled)
                      }
                      min={min}
                      max={max}
                      step={numeric ? 1 : undefined}
                      aria-describedby={
                        name === 'max_concurrent_uploads'
                          ? `storage-error-${name} storage-concurrency-help`
                          : `storage-error-${name}`
                      }
                    />
                    {name === 'public_endpoint' && (
                      <p className='text-muted-foreground text-sm'>
                        {t(
                          'Optional. Leave blank to use the OSS public bucket domain. A custom domain must be bound to this bucket and have HTTPS configured.'
                        )}
                      </p>
                    )}
                    {name === 'prefix' && (
                      <p className='text-muted-foreground text-sm'>
                        {t(
                          'Required settings with default values can be kept unchanged.'
                        )}
                      </p>
                    )}
                    {name === 'daily_limit_mb' && (
                      <p className='text-muted-foreground text-sm'>
                        {t(
                          'Upload limits are shared by all API keys of the same user. Request limits apply before file processing. Signed download URLs can be reused until they expire; upload limits do not limit OSS download traffic.'
                        )}
                      </p>
                    )}
                    {name === 'max_concurrent_uploads' && (
                      <>
                        <p
                          id='storage-concurrency-help'
                          className='text-muted-foreground text-sm'
                        >
                          {t(
                            'Limits requests receiving files or uploading to OSS on each server instance. Extra requests return 429 without queuing. This is not a user limit or a model API concurrency limit. Higher values use more disk, network, CPU and memory. Lowering it does not interrupt active uploads.'
                          )}
                        </p>
                        <p className='text-muted-foreground text-sm'>
                          {t(
                            'Allowed range: 1–30; default: 2. All users on an instance share this limit. At 30 uploads of 20 MB, temporary files can use about 600 MB, excluding leftovers. Uploads share server bandwidth with model APIs. Increase gradually and monitor bandwidth and model latency.'
                          )}
                        </p>
                      </>
                    )}
                    {form.formState.errors[name] && (
                      <p id={`storage-error-${name}`} role='alert'>
                        {t(
                          'Invalid storage setting. Check the required fields and allowed ranges.'
                        )}
                      </p>
                    )}
                  </div>
                )
              })}
            </>
          )}
          <p>
            {t(
              'OSS images are automatically cleaned up by Alibaba Cloud lifecycle rules. In the OSS console, set objects under the upload prefix to expire after 3 days. This system only checks the rules; it does not create or modify them.'
            )}
          </p>
          <p>
            {t(
              'Save changes before testing. Tests use the saved configuration, even when storage is disabled.'
            )}
          </p>
          <p>
            {t(
              'Temporary files are deleted immediately when a request ends. Scheduled, startup and manual cleanup remove only expired leftovers and never active uploads. Cleanup does not delete OSS objects.'
            )}
          </p>
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              disabled={busy || dirty}
              onClick={() => cleanup.mutate()}
            >
              {t('Clean expired temporary files')}
            </Button>
            <Button
              type='button'
              disabled={busy || dirty}
              onClick={() => check.mutate('test')}
            >
              {t('Test saved storage')}
            </Button>
            <Button
              type='button'
              disabled={busy || dirty}
              onClick={() => check.mutate('lifecycle')}
            >
              {t('Check bucket lifecycle')}
            </Button>
          </div>
        </fieldset>
      </SettingsForm>
    </SettingsSection>
  )
}
