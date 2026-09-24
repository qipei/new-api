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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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

interface CustomerServiceConfig {
  phone: string
  qrcode_data_url: string
}

const endpoint = '/api/option/customer-service'

export function CustomerServiceSection() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['customer-service-settings'],
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const res = await api.get<{
        success: boolean
        message?: string
        data: CustomerServiceConfig
      }>(endpoint)
      if (!res.data.success) {
        throw new Error(res.data.message || t('Failed to load settings'))
      }
      return res.data.data
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
  return <CustomerServiceForm initialValues={query.data} />
}

function CustomerServiceForm(props: { initialValues: CustomerServiceConfig }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<CustomerServiceConfig>({
    defaultValues: props.initialValues,
  })
  const [reading, setReading] = useState(false)
  const [imageError, setImageError] = useState('')
  const qrCode = form.watch('qrcode_data_url')
  const isDirty = form.formState.isDirty
  useEffect(() => {
    if (!isDirty) form.reset(props.initialValues)
  }, [props.initialValues, form, isDirty])
  const mutation = useMutation({
    mutationFn: async (values: CustomerServiceConfig) => {
      const res = await api.put<{
        success: boolean
        message?: string
        data: CustomerServiceConfig
      }>(endpoint, values, { skipBusinessError: true, skipErrorHandler: true })
      if (!res.data.success) {
        throw new Error(res.data.message || t('Failed to save settings'))
      }
      return res.data.data
    },
    onSuccess: (data) => {
      queryClient.setQueryData(['customer-service-settings'], data)
      form.reset(data)
      toast.success(t('Settings saved successfully'))
    },
    onError: () => {
      toast.error(t('Failed to save settings'))
    },
  })
  const disabled = reading || mutation.isPending
  const submit = form.handleSubmit((values) => mutation.mutate(values))

  return (
    <SettingsSection title={t('Customer Service')}>
      <SettingsForm onSubmit={submit}>
        <SettingsPageFormActions
          onSave={submit}
          isSaving={mutation.isPending}
          isSaveDisabled={disabled || !form.formState.isDirty}
          saveLabel='Save customer service settings'
        />
        <FieldGroup>
          <Field data-invalid={!!form.formState.errors.phone}>
            <FieldLabel htmlFor='customer-service-phone'>
              {t('Customer service phone')}
            </FieldLabel>
            <Input
              id='customer-service-phone'
              type='tel'
              maxLength={32}
              disabled={disabled}
              aria-invalid={!!form.formState.errors.phone}
              {...form.register('phone', {
                validate: (value) =>
                  !value.trim() ||
                  /^\+?[0-9][0-9 ()-]{2,30}$/.test(value.trim()) ||
                  t('Enter a valid phone number'),
              })}
            />
            {form.formState.errors.phone && (
              <p role='alert' className='text-destructive text-sm'>
                {form.formState.errors.phone.message}
              </p>
            )}
          </Field>
          <Field data-invalid={!!imageError}>
            <FieldLabel htmlFor='customer-service-qrcode'>
              {t('Customer service QR code')}
            </FieldLabel>
            <Input
              id='customer-service-qrcode'
              type='file'
              accept='image/png,image/jpeg'
              disabled={disabled}
              aria-invalid={!!imageError}
              onChange={(event) => {
                const file = event.target.files?.[0]
                event.target.value = ''
                if (!file) return
                setImageError('')
                if (
                  !['image/png', 'image/jpeg'].includes(file.type) ||
                  file.size > 500 * 1024
                ) {
                  setImageError(
                    t(
                      'Use a PNG or JPEG image up to 500 KB and 2048 × 2048 pixels.'
                    )
                  )
                  return
                }
                setReading(true)
                const reader = new FileReader()
                reader.onload = () => {
                  if (typeof reader.result === 'string') {
                    form.setValue('qrcode_data_url', reader.result, {
                      shouldDirty: true,
                    })
                  }
                  setReading(false)
                }
                reader.onerror = () => {
                  setImageError(t('Failed to read image'))
                  setReading(false)
                }
                reader.readAsDataURL(file)
              }}
            />
            <FieldDescription>
              {t(
                'Phone and QR code are public. Leave either empty to hide it.'
              )}
            </FieldDescription>
            <FieldDescription>
              {t('PNG or JPEG, up to 500 KB and 2048 × 2048 pixels.')}
            </FieldDescription>
            {imageError && (
              <p role='alert' className='text-destructive text-sm'>
                {imageError}
              </p>
            )}
            {qrCode && (
              <div className='flex flex-wrap items-end gap-4'>
                <img
                  src={qrCode}
                  alt={t('Customer service QR code preview')}
                  className='size-40 rounded-lg border bg-white object-contain p-2'
                />
                <Button
                  type='button'
                  variant='outline'
                  disabled={disabled}
                  onClick={() => {
                    form.setValue('qrcode_data_url', '', { shouldDirty: true })
                    setImageError('')
                  }}
                >
                  {t('Remove QR code')}
                </Button>
              </div>
            )}
          </Field>
        </FieldGroup>
      </SettingsForm>
    </SettingsSection>
  )
}
