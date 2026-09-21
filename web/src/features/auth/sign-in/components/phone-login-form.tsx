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
import axios from 'axios'
import { Loader2, LogIn } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { Trans, useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { z } from 'zod'

import { Button } from '@/components/ui/button'
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
import { phoneLogin, sendLoginSMSCode } from '@/features/auth/api'
import { phoneLoginFormSchema } from '@/features/auth/constants'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'
import { useSMSVerification } from '@/features/auth/hooks/use-sms-verification'
import { getAffiliateCode } from '@/features/auth/lib/storage'
import { isAuthBundle } from '@/lib/api'
import { getServerErrorMessageKey } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

interface PhoneLoginFormProps {
  redirectTo?: string
  /** Set while the legal consent checkbox is still unchecked. */
  disabled?: boolean
  turnstileToken?: string
  validateTurnstile?: () => boolean
  /** Lets the parent reset its Turnstile widget after a token is spent. */
  onTurnstileConsumed?: () => void
}

export function PhoneLoginForm(props: PhoneLoginFormProps) {
  const { t } = useTranslation()
  const { handleLoginSuccess, redirectTo2FA } = useAuthRedirect()
  const setPending2FAFlowToken = useAuthStore(
    (state) => state.auth.setPending2FAFlowToken
  )

  const form = useForm<z.infer<typeof phoneLoginFormSchema>>({
    resolver: zodResolver(phoneLoginFormSchema),
    defaultValues: {
      phone: '',
      code: '',
    },
  })

  const {
    isSending,
    secondsLeft,
    isActive: isCountingDown,
    sendCode,
  } = useSMSVerification({
    send: sendLoginSMSCode,
    turnstileToken: props.turnstileToken,
    validateTurnstile: props.validateTurnstile,
    onTurnstileConsumed: props.onTurnstileConsumed,
  })

  const handleSendCode = async () => {
    if (props.disabled) {
      toast.error(t('Please agree to the legal terms first'))
      return
    }
    await sendCode(form.getValues('phone'))
  }

  const onSubmit = async (values: z.infer<typeof phoneLoginFormSchema>) => {
    if (props.disabled) {
      toast.error(t('Please agree to the legal terms first'))
      return
    }
    if (props.validateTurnstile && !props.validateTurnstile()) return

    try {
      props.onTurnstileConsumed?.()
      const res = await phoneLogin({
        phone: values.phone.replaceAll(/\D/g, ''),
        code: values.code.trim(),
        aff_code: getAffiliateCode() || undefined,
        turnstile: props.turnstileToken,
      })

      if (!res.success) {
        if (getServerErrorMessageKey(res)) return
        toast.error(res.message || t('Login failed'))
        return
      }

      if (res.data && 'require_2fa' in res.data && res.data.require_2fa) {
        if (!res.data.flow_token) {
          throw new Error(t('Login flow expired. Please sign in again.'))
        }
        setPending2FAFlowToken(res.data.flow_token)
        redirectTo2FA()
        return
      }

      if (!isAuthBundle(res.data)) {
        throw new Error(t('Login failed'))
      }
      await handleLoginSuccess(res.data, props.redirectTo)
      toast.success(t('Welcome back!'))
    } catch (error: unknown) {
      if (axios.isAxiosError(error)) return
      toast.error(error instanceof Error ? error.message : t('Login failed'))
    }
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className='grid gap-4'>
        <FormField
          control={form.control}
          name='phone'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Mobile number')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('Enter your mobile number')}
                  inputMode='numeric'
                  autoComplete='tel'
                  maxLength={11}
                  {...field}
                />
              </FormControl>
              <FormMessage />
              <FormDescription className='text-xs leading-5'>
                <Trans
                  t={t}
                  i18nKey='A new account will be created automatically after verifying an unregistered mobile number.'
                  components={{
                    strong: (
                      <strong className='font-bold text-[var(--auth-link)]' />
                    ),
                  }}
                />
              </FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='code'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Verification code')}</FormLabel>
              <div className='flex items-center gap-2'>
                <FormControl>
                  <Input
                    placeholder={t('Enter the verification code')}
                    inputMode='numeric'
                    autoComplete='one-time-code'
                    maxLength={8}
                    {...field}
                  />
                </FormControl>
                <Button
                  type='button'
                  variant='outline'
                  className='shrink-0'
                  disabled={isSending || isCountingDown}
                  onClick={handleSendCode}
                >
                  {isSending ? (
                    <Loader2 className='h-4 w-4 animate-spin' />
                  ) : null}
                  {isCountingDown
                    ? t('Resend in {{seconds}}s', { seconds: secondsLeft })
                    : t('Get code')}
                </Button>
              </div>
              <FormMessage />
            </FormItem>
          )}
        />

        <Button
          type='submit'
          className='mt-2 w-full justify-center gap-2'
          disabled={form.formState.isSubmitting || props.disabled}
        >
          {form.formState.isSubmitting ? (
            <Loader2 className='animate-spin' />
          ) : (
            <LogIn />
          )}
          {t('Sign in')}
        </Button>
      </form>
    </Form>
  )
}
