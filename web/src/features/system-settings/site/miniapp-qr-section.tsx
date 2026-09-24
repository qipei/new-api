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
import { useEffect, useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
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
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'

const endpoint = '/api/option/miniapp-qr-code'
const queryKey = ['miniapp-qr-code-settings']
interface MiniAppQRCodeConfig {
  enabled: boolean
  image_url: string
}

export function MiniAppQRSection() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const response = await api.get<{
        success: boolean
        data: MiniAppQRCodeConfig
      }>(endpoint)
      if (!response.data.success) throw new Error(t('Failed to load settings'))
      return response.data.data
    },
  })
  if (query.isPending) return <p>{t('Loading...')}</p>
  if (query.isError) {
    return (
      <div role='alert'>
        <p>{t('Failed to load settings')}</p>
        <Button onClick={() => query.refetch()}>{t('Retry')}</Button>
      </div>
    )
  }
  return <MiniAppQRForm initialValues={query.data} />
}

function MiniAppQRForm(props: { initialValues: MiniAppQRCodeConfig }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [failedImage, setFailedImage] = useState('')
  const schema = z.object({
    enabled: z.boolean(),
    image_url: z
      .string()
      .trim()
      .min(1)
      .max(2048)
      .refine((value) => {
        if (/[\\\r\n\t]/.test(value)) return false
        if (value.startsWith('/') && !value.startsWith('//')) return true
        try {
          const url = new URL(value)
          return (
            ['http:', 'https:'].includes(url.protocol) &&
            !!url.hostname &&
            !url.username &&
            !url.password
          )
        } catch {
          return false
        }
      }, t('Enter an HTTP(S) image URL or a site-relative path.')),
  })
  const form = useForm<MiniAppQRCodeConfig>({
    defaultValues: props.initialValues,
    resolver: zodResolver(schema),
  })
  const imageURL = form.watch('image_url').trim()
  const isDirty = form.formState.isDirty
  useEffect(() => {
    if (!isDirty) form.reset(props.initialValues)
  }, [form, isDirty, props.initialValues])
  const mutation = useMutation({
    mutationFn: async (values: MiniAppQRCodeConfig) => {
      const response = await api.put<{
        success: boolean
        data: MiniAppQRCodeConfig
      }>(endpoint, values, { skipBusinessError: true, skipErrorHandler: true })
      if (!response.data.success) throw new Error(t('Failed to save settings'))
      return response.data.data
    },
    onSuccess: (data) => {
      queryClient.setQueryData(queryKey, data)
      queryClient.setQueryData(['miniapp-qr-code'], data)
      form.reset(data)
      toast.success(t('Settings saved successfully'))
    },
    onError: () => toast.error(t('Failed to save settings')),
  })
  const submit = form.handleSubmit((values) => mutation.mutate(values))
  const imageValid = schema.shape.image_url.safeParse(imageURL).success

  return (
    <SettingsSection title={t('Mini program QR code')}>
      <SettingsForm onSubmit={submit}>
        <SettingsPageFormActions
          onSave={submit}
          isSaving={mutation.isPending}
          isSaveDisabled={mutation.isPending || !isDirty}
        />
        <FieldGroup>
          <Field orientation='horizontal'>
            <FieldLabel htmlFor='miniapp-qr-enabled'>
              {t('Show mini program QR code')}
            </FieldLabel>
            <Controller
              name='enabled'
              control={form.control}
              render={({ field }) => (
                <Switch
                  id='miniapp-qr-enabled'
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={mutation.isPending}
                />
              )}
            />
          </Field>
          <FieldDescription>
            {t('Display the mini program card on the home and pricing pages.')}
          </FieldDescription>
          <Field data-invalid={!!form.formState.errors.image_url}>
            <FieldLabel htmlFor='miniapp-qr-image'>
              {t('Mini program QR image URL')}
            </FieldLabel>
            <Input
              id='miniapp-qr-image'
              maxLength={2048}
              disabled={mutation.isPending}
              aria-invalid={!!form.formState.errors.image_url}
              aria-describedby='miniapp-qr-image-help miniapp-qr-image-error'
              {...form.register('image_url')}
            />
            <FieldDescription id='miniapp-qr-image-help'>
              {t('Use a public HTTP(S) image URL or a path starting with /.')}
            </FieldDescription>
            <p
              id='miniapp-qr-image-error'
              role={form.formState.errors.image_url ? 'alert' : undefined}
              className='text-destructive text-sm'
            >
              {form.formState.errors.image_url &&
                t('Enter an HTTP(S) image URL or a site-relative path.')}
            </p>
            {imageValid && imageURL !== failedImage && (
              <img
                src={imageURL}
                onError={() => setFailedImage(imageURL)}
                alt={t('Mini program QR code')}
                className='size-40 max-w-full rounded-lg border bg-white object-contain p-2'
              />
            )}
            {imageValid && imageURL === failedImage && (
              <p role='status' className='text-muted-foreground text-sm'>
                {t('Unable to load the QR image. Check the image URL.')}
              </p>
            )}
          </Field>
        </FieldGroup>
      </SettingsForm>
    </SettingsSection>
  )
}
