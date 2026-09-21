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
import axios from 'axios'
import i18next from 'i18next'
import { useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { useCountdown } from '@/hooks/use-countdown'

import { SMS_VERIFICATION_COUNTDOWN, PHONE_NUMBER_REGEX } from '../constants'
import { openTencentCaptcha } from '../lib/tencent-captcha'
import type { SMSCodePayload, SMSCodeResponse } from '../types'

interface UseSMSVerificationOptions {
  /** Which endpoint to call: anonymous login codes or bind codes. */
  send: (payload: SMSCodePayload) => Promise<SMSCodeResponse>
  turnstileToken?: string
  validateTurnstile?: () => boolean
  onTurnstileConsumed?: () => void
}

/**
 * Drives the SMS code request flow, including the anti-abuse detour: when the
 * server answers `require_captcha`, the Tencent captcha is opened and the same
 * request is retried once with the resulting ticket.
 */
export function useSMSVerification(options: UseSMSVerificationOptions) {
  const [isSending, setIsSending] = useState(false)
  const sendingRef = useRef(false)
  const tokenRef = useRef(options.turnstileToken)
  const tokenWaiter = useRef<((token: string | undefined) => void) | null>(null)
  const retryWaiter = useRef<(() => void) | null>(null)
  const mountedRef = useRef(true)

  useEffect(() => {
    tokenRef.current = options.turnstileToken
    if (options.turnstileToken) tokenWaiter.current?.(options.turnstileToken)
  }, [options.turnstileToken])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      tokenWaiter.current?.(undefined)
      retryWaiter.current?.()
    }
  }, [])
  const {
    secondsLeft,
    isActive,
    start: startCountdown,
  } = useCountdown({ initialSeconds: SMS_VERIFICATION_COUNTDOWN })

  const requestCode = async (
    phone: string,
    turnstile: string | undefined,
    ticket?: { ticket: string; randstr: string }
  ): Promise<SMSCodeResponse> => {
    tokenRef.current = undefined
    options.onTurnstileConsumed?.()
    try {
      return await options.send({
        phone,
        captcha_ticket: ticket?.ticket,
        captcha_randstr: ticket?.randstr,
        turnstile,
      })
    } catch (error) {
      if (
        axios.isAxiosError<SMSCodeResponse>(error) &&
        error.response?.status === 429
      ) {
        const body = error.response.data
        return {
          success: false,
          message: body?.message || i18next.t('Too many requests'),
          data: {
            ...body?.data,
            resend_after:
              body?.data?.resend_after ??
              Number(error.response.headers['retry-after'] || 30),
          },
        }
      }
      throw error
    }
  }

  const sendCode = async (phone: string): Promise<boolean> => {
    if (sendingRef.current || isActive) return false
    const normalized = phone.replaceAll(/\D/g, '')
    if (!PHONE_NUMBER_REGEX.test(normalized)) {
      toast.error(i18next.t('Please enter a valid mobile number'))
      return false
    }
    if (options.validateTurnstile && !options.validateTurnstile()) {
      return false
    }

    sendingRef.current = true
    setIsSending(true)
    try {
      let res = await requestCode(normalized, options.turnstileToken)

      if (res.data?.require_captcha) {
        const appId = res.data.captcha_app_id
        if (!appId) {
          toast.error(i18next.t('Security verification is not available'))
          return false
        }
        const retryAfter = Math.max(0, res.data.resend_after ?? 0)
        const retryAt = Date.now() + retryAfter * 1000
        if (retryAfter > 0) startCountdown(retryAfter)
        const ticket = await openTencentCaptcha(appId)
        // The user closed the captcha without solving it.
        if (!ticket || !mountedRef.current) return false
        const remaining = retryAt - Date.now()
        if (remaining > 0) {
          await new Promise<void>((resolve) => {
            const timeout = window.setTimeout(
              () => retryWaiter.current?.(),
              remaining
            )
            retryWaiter.current = () => {
              window.clearTimeout(timeout)
              retryWaiter.current = null
              resolve()
            }
          })
        }
        if (!mountedRef.current) return false
        let retryToken = tokenRef.current
        if (options.turnstileToken && !retryToken) {
          retryToken = await new Promise<string | undefined>((resolve) => {
            const timeout = window.setTimeout(
              () => tokenWaiter.current?.(undefined),
              60000
            )
            tokenWaiter.current = (token) => {
              window.clearTimeout(timeout)
              tokenWaiter.current = null
              resolve(token)
            }
          })
          if (!retryToken) return false
        }
        res = await requestCode(normalized, retryToken, ticket)
        if (res?.success && res.data?.require_captcha) {
          toast.error(i18next.t('Security verification failed, please retry'))
          return false
        }
      }

      if (!res?.success) {
        if (res.data?.resend_after && res.data.resend_after > 0) {
          startCountdown(res.data.resend_after)
        }
        toast.error(
          res?.message || i18next.t('Failed to send the verification code')
        )
        return false
      }

      const resendAfter = res.data?.resend_after ?? SMS_VERIFICATION_COUNTDOWN
      if (resendAfter > 0) startCountdown(resendAfter)
      if (res.data?.debug_code) {
        // Debug mode never actually sends an SMS, so surface the code instead
        // of leaving the operator waiting for a message that will not arrive.
        toast.success(
          i18next.t('Debug verification code: {{code}}', {
            code: res.data.debug_code,
          })
        )
      } else {
        toast.success(i18next.t('Verification code sent'))
      }
      return true
    } catch (error: unknown) {
      if (axios.isAxiosError<SMSCodeResponse>(error)) {
        toast.error(
          error.response?.data?.message ||
            i18next.t('Failed to send the verification code')
        )
      } else if (error instanceof Error && error.message) {
        toast.error(error.message)
      }
      return false
    } finally {
      sendingRef.current = false
      setIsSending(false)
    }
  }

  return { isSending, secondsLeft, isActive, sendCode }
}
