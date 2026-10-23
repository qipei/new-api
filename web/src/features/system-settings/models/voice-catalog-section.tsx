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
// CUSTOM: 火山预置音色同步设置（fork 扩展）。
// AK/SK 只写不读：后端只返回是否已配置，留空保存表示保持原值。
// 百炼音色是随代码发布的静态清单，这里不需要配置。
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
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
import { api } from '@/lib/api'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'

const endpoint = '/api/option/voice-catalog'
const queryKey = ['voice-catalog-settings']

type SyncTask = {
  status: string
  updated_at: number
  error?: string
  result?: {
    counts?: Record<string, number>
    errors?: Record<string, string>
  } | null
}

export type VoiceCatalogConfig = {
  volc_access_key_id_configured: boolean
  volc_secret_access_key_configured: boolean
  volc_resource_ids: string[]
  sync_interval_hours: number
  last_sync?: SyncTask
}

const schema = z.object({
  volc_access_key_id: z.string().trim().max(256),
  volc_secret_access_key: z.string().trim().max(512),
  volc_resource_ids: z.string().trim().min(1),
  sync_interval_hours: z.number().int().min(1).max(168),
})
type FormValues = z.infer<typeof schema>

function toFormValues(config: VoiceCatalogConfig): FormValues {
  return {
    volc_access_key_id: '',
    volc_secret_access_key: '',
    volc_resource_ids: config.volc_resource_ids.join(', '),
    sync_interval_hours: config.sync_interval_hours,
  }
}

const SYNC_STATUS_LABEL_KEYS: Record<string, string> = {
  pending: 'Queued',
  running: 'Running',
  succeeded: 'Succeeded',
  failed: 'Failed',
}

function isSyncRunning(task?: SyncTask): boolean {
  return task?.status === 'pending' || task?.status === 'running'
}

export function VoiceCatalogSection() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey,
    refetchOnWindowFocus: false,
    // 同步在系统任务里异步执行，进行中时轮询结果。
    refetchInterval: (q) =>
      isSyncRunning(q.state.data?.last_sync) ? 3000 : false,
    queryFn: async () => {
      const response = await api.get<{
        success: boolean
        data: VoiceCatalogConfig
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
    content = <VoiceCatalogForm config={query.data} />
  }
  return <SettingsSection title={t('Voice Catalog')}>{content}</SettingsSection>
}

function VoiceCatalogForm(props: { config: VoiceCatalogConfig }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: toFormValues(props.config),
  })
  const isDirty = form.formState.isDirty
  useEffect(() => {
    if (!isDirty) form.reset(toFormValues(props.config))
  }, [form, isDirty, props.config])

  const save = useMutation({
    mutationFn: async (body: Record<string, unknown>) => {
      const response = await api.put<{
        success: boolean
        message?: string
        data: VoiceCatalogConfig
      }>(endpoint, body, { skipBusinessError: true, skipErrorHandler: true })
      if (!response.data.success) {
        throw new Error(response.data.message || t('Failed to save settings'))
      }
      return response.data.data
    },
    onSuccess: (data) => {
      queryClient.setQueryData(queryKey, data)
      form.reset(toFormValues(data))
      toast.success(t('Settings saved successfully'))
    },
    onError: (error) => toast.error(error.message),
  })

  const sync = useMutation({
    mutationFn: async () => {
      const response = await api.post<{ success: boolean; message?: string }>(
        `${endpoint}/sync`,
        {},
        { skipBusinessError: true, skipErrorHandler: true }
      )
      if (!response.data.success) {
        throw new Error(response.data.message || t('Sync failed'))
      }
    },
    onSuccess: () => {
      toast.success(t('Voice sync started'))
      void queryClient.invalidateQueries({ queryKey })
    },
    onError: (error) => toast.error(error.message),
  })

  const submit = form.handleSubmit((values) => {
    save.mutate({
      ...values,
      volc_resource_ids: values.volc_resource_ids
        .split(/[,，\s]+/)
        .map((id) => id.trim())
        .filter(Boolean),
    })
  })

  const configured =
    props.config.volc_access_key_id_configured &&
    props.config.volc_secret_access_key_configured
  const lastSync = props.config.last_sync
  const busy = save.isPending || sync.isPending

  return (
    <SettingsForm onSubmit={submit}>
      <SettingsPageFormActions
        onSave={submit}
        isSaving={save.isPending}
        isSaveDisabled={busy || !isDirty}
      />
      <FieldGroup>
        <p className='text-muted-foreground text-sm'>
          {t(
            'The model marketplace lists preset voices for speech models. Bailian voices ship with the system; Volcengine voices are synced from the ListSpeakers API with an OpenAPI access key, which is different from the speech API key.'
          )}
        </p>
        <Field>
          <FieldLabel htmlFor='voice-catalog-ak'>
            {t('Volcengine Access Key ID')}
          </FieldLabel>
          <Input
            id='voice-catalog-ak'
            type='password'
            autoComplete='new-password'
            disabled={busy}
            placeholder={
              props.config.volc_access_key_id_configured
                ? t('Configured. Leave blank to keep it.')
                : 'AKLT...'
            }
            {...form.register('volc_access_key_id')}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor='voice-catalog-sk'>
            {t('Volcengine Secret Access Key')}
          </FieldLabel>
          <Input
            id='voice-catalog-sk'
            type='password'
            autoComplete='new-password'
            disabled={busy}
            placeholder={
              props.config.volc_secret_access_key_configured
                ? t('Configured. Leave blank to keep it.')
                : ''
            }
            {...form.register('volc_secret_access_key')}
          />
          <FieldDescription>
            {t(
              'Create it under Access Control in the Volcengine console. A sub-user with read-only Doubao Speech permission is enough. Saved keys are never displayed.'
            )}
          </FieldDescription>
        </Field>
        <Field data-invalid={!!form.formState.errors.volc_resource_ids}>
          <FieldLabel htmlFor='voice-catalog-resources'>
            {t('Models to sync')}
          </FieldLabel>
          <Input
            id='voice-catalog-resources'
            disabled={busy}
            {...form.register('volc_resource_ids')}
          />
          <FieldDescription>
            {t(
              'ListSpeakers ResourceIDs, separated by commas, for example seed-tts-2.0, seed-tts-1.0.'
            )}
          </FieldDescription>
        </Field>
        <Field data-invalid={!!form.formState.errors.sync_interval_hours}>
          <FieldLabel htmlFor='voice-catalog-interval'>
            {t('Sync interval (hours)')}
          </FieldLabel>
          <Input
            id='voice-catalog-interval'
            type='number'
            min={1}
            max={168}
            step={1}
            disabled={busy}
            {...form.register('sync_interval_hours', { valueAsNumber: true })}
          />
          <FieldDescription>{t('Range: 1–168 hours.')}</FieldDescription>
        </Field>

        <div className='bg-muted/30 space-y-2 rounded-lg border p-3 text-sm'>
          <div className='flex flex-wrap items-center gap-2'>
            <span className='font-medium'>{t('Last sync')}</span>
            {lastSync ? (
              <span className='text-muted-foreground'>
                {t(SYNC_STATUS_LABEL_KEYS[lastSync.status] ?? lastSync.status)}{' '}
                · {new Date(lastSync.updated_at * 1000).toLocaleString()}
              </span>
            ) : (
              <span className='text-muted-foreground'>{t('Never synced')}</span>
            )}
          </div>
          {Object.entries(lastSync?.result?.counts ?? {}).map(
            ([resource, count]) => (
              <div key={resource} className='text-muted-foreground'>
                {t('{{resource}}: {{count}} voices', { resource, count })}
              </div>
            )
          )}
          {Object.entries(lastSync?.result?.errors ?? {}).map(
            ([resource, message]) => (
              <div key={resource} className='text-destructive break-all'>
                {resource}: {message}
              </div>
            )
          )}
          <div className='flex flex-wrap gap-2 pt-1'>
            <Button
              type='button'
              size='sm'
              disabled={!configured || busy || isSyncRunning(lastSync)}
              onClick={() => sync.mutate()}
            >
              {isSyncRunning(lastSync) ? t('Syncing...') : t('Sync now')}
            </Button>
            {configured && (
              <Button
                type='button'
                size='sm'
                variant='outline'
                disabled={busy}
                onClick={() =>
                  save.mutate({
                    clear_volc_credentials: true,
                    volc_resource_ids: props.config.volc_resource_ids,
                    sync_interval_hours: props.config.sync_interval_hours,
                  })
                }
              >
                {t('Clear access key')}
              </Button>
            )}
          </div>
          {!configured && (
            <p className='text-muted-foreground'>
              {t('Save the access key first to enable syncing.')}
            </p>
          )}
        </div>
      </FieldGroup>
    </SettingsForm>
  )
}
