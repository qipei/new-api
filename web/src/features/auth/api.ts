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

import {
  api,
  isAuthBundle,
  refreshAuthentication,
  type RefreshOutcome,
} from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import {
  clearAffiliateCode,
  consumeLoginAffiliateCode,
  getAffiliateCode,
  saveLoginAffiliateCode,
} from './lib/storage'
import type { TelegramAuthorization } from './lib/telegram-login'
import type {
  LoginPayload,
  LoginResponse,
  Login2FAResponse,
  TwoFAPayload,
  RegisterPayload,
  ApiResponse,
  PhoneLoginPayload,
  SMSCodePayload,
  SMSCodeResponse,
  BindPhonePayload,
} from './types'

// ============================================================================
// Authentication APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Login & Logout
// ----------------------------------------------------------------------------

// User login with username and password
export async function login(payload: LoginPayload) {
  const affiliateCode = getAffiliateCode()
  const turnstile = payload.turnstile ?? ''
  const res = await api.post<LoginResponse>(
    `/api/user/login?turnstile=${turnstile}`,
    {
      username: payload.username,
      password: payload.password,
    },
    { skipAuthRefresh: true }
  )
  if (res.data.success && isAuthBundle(res.data.data)) {
    clearAffiliateCode(affiliateCode)
  } else if (
    res.data.success &&
    res.data.data &&
    'require_2fa' in res.data.data &&
    res.data.data.require_2fa &&
    res.data.data.flow_token
  ) {
    saveLoginAffiliateCode(res.data.data.flow_token, affiliateCode)
  }
  return res.data
}

// Login with a phone number and an SMS verification code. The account is
// created on the fly when the number has never signed in before.
export async function phoneLogin(payload: PhoneLoginPayload) {
  const affiliateCode = payload.aff_code ?? getAffiliateCode()
  const turnstile = payload.turnstile ?? ''
  const res = await api.post<LoginResponse>(
    `/api/user/login/phone?turnstile=${turnstile}`,
    {
      phone: payload.phone,
      code: payload.code,
      aff_code: payload.aff_code,
    },
    { skipAuthRefresh: true }
  )
  if (res.data.success && isAuthBundle(res.data.data)) {
    clearAffiliateCode(affiliateCode)
  } else if (
    res.data.success &&
    res.data.data &&
    'require_2fa' in res.data.data &&
    res.data.data.require_2fa &&
    res.data.data.flow_token
  ) {
    saveLoginAffiliateCode(res.data.data.flow_token, affiliateCode)
  }
  return res.data
}

// Request an SMS login code. The response asks for a captcha instead of
// sending anything when the anti-abuse thresholds have been reached.
export async function sendLoginSMSCode(
  payload: SMSCodePayload
): Promise<SMSCodeResponse> {
  const turnstile = payload.turnstile ?? ''
  const res = await api.post<SMSCodeResponse>(
    `/api/sms/code?turnstile=${turnstile}`,
    {
      phone: payload.phone,
      captcha_ticket: payload.captcha_ticket,
      captcha_randstr: payload.captcha_randstr,
    },
    { skipAuthRefresh: true, skipErrorHandler: true, skipBusinessError: true }
  )
  return res.data
}

// Request an SMS code for binding a phone number to the signed-in account.
export async function sendBindPhoneCode(
  payload: SMSCodePayload
): Promise<SMSCodeResponse> {
  const res = await api.post<SMSCodeResponse>(
    '/api/user/phone/code',
    {
      phone: payload.phone,
      captcha_ticket: payload.captcha_ticket,
      captcha_randstr: payload.captcha_randstr,
    },
    { skipErrorHandler: true, skipBusinessError: true }
  )
  return res.data
}

// Bind a phone number to the signed-in account.
export async function bindPhone(
  payload: BindPhonePayload
): Promise<ApiResponse> {
  const res = await api.post('/api/user/phone/bind', payload)
  return res.data
}

// Two-factor authentication login
export async function login2fa(payload: TwoFAPayload) {
  const res = await api.post<Login2FAResponse>('/api/user/login/2fa', payload, {
    skipAuthRefresh: true,
  })
  if (res.data.success && isAuthBundle(res.data.data)) {
    consumeLoginAffiliateCode(payload.flow_token)
  }
  return res.data
}

interface LogoutRuntime {
  getExpectedSID: () => string | undefined
  request: (expectedSID?: string) => Promise<ApiResponse>
  refresh: () => Promise<RefreshOutcome>
}

export async function executeLogout(
  runtime: LogoutRuntime,
  allowMismatchRecovery = true
): Promise<ApiResponse> {
  try {
    return await runtime.request(runtime.getExpectedSID())
  } catch (error: unknown) {
    const code = axios.isAxiosError(error)
      ? error.response?.data?.code
      : undefined
    if (
      allowMismatchRecovery &&
      axios.isAxiosError(error) &&
      error.response?.status === 409 &&
      code === 'AUTH_SESSION_MISMATCH'
    ) {
      const outcome = await runtime.refresh()
      if (outcome.kind === 'authenticated') {
        return executeLogout(runtime, false)
      }
      if (outcome.kind === 'anonymous') {
        return { success: true, message: '' }
      }
    }
    throw error
  }
}

// User logout
export async function logout(): Promise<ApiResponse> {
  return executeLogout({
    getExpectedSID: () => useAuthStore.getState().auth.session?.sid,
    request: async (sid) => {
      const res = await api.post('/api/user/auth/logout', undefined, {
        headers: sid ? { 'X-Auth-Session': sid } : undefined,
        skipAuthRefresh: true,
        skipErrorHandler: true,
      })
      return res.data
    },
    refresh: refreshAuthentication,
  })
}

// ----------------------------------------------------------------------------
// Password Management
// ----------------------------------------------------------------------------

// Send password reset email
export async function sendPasswordResetEmail(
  email: string,
  turnstile?: string
): Promise<ApiResponse> {
  const res = await api.get('/api/reset_password', {
    params: { email, turnstile },
  })
  return res.data
}

// ----------------------------------------------------------------------------
// OAuth
// ----------------------------------------------------------------------------

// Start GitHub OAuth flow
export async function githubOAuthStart(clientId: string, state: string) {
  const url = `https://github.com/login/oauth/authorize?client_id=${clientId}&state=${state}&scope=user:email`
  window.open(url)
}

// Get OAuth state for CSRF protection
export async function createOAuthFlow(
  provider: string,
  intent: 'login' | 'bind'
): Promise<string> {
  const aff = intent === 'login' ? getAffiliateCode() : ''
  const res = await api.post(
    '/api/oauth/state',
    { provider, intent, aff: aff || undefined },
    { skipAuthRefresh: intent === 'login' }
  )
  if (res.data?.success) {
    if (typeof res.data.data === 'string') return res.data.data
    if (typeof res.data.data?.flow_token === 'string') {
      return res.data.data.flow_token
    }
  }
  throw new Error(res.data?.message || 'Failed to initialize OAuth')
}

// WeChat login by authorization code
export async function wechatLoginByCode(code: string): Promise<ApiResponse> {
  const res = await api.get('/api/oauth/wechat', { params: { code } })
  return res.data
}

export async function telegramLogin(
  authorization: TelegramAuthorization
): Promise<ApiResponse> {
  const res = await api.get('/api/oauth/telegram/login', {
    params: authorization,
    disableDuplicate: true,
    skipAuthRefresh: true,
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  return res.data
}

// ----------------------------------------------------------------------------
// Registration
// ----------------------------------------------------------------------------

// User registration
export async function register(payload: RegisterPayload): Promise<ApiResponse> {
  const res = await api.post(`/api/user/register`, payload, {
    params: { turnstile: payload.turnstile ?? '' },
  })
  if (res.data.success && payload.aff_code) {
    clearAffiliateCode(payload.aff_code)
  }
  return res.data
}

// Send email verification code
export async function sendEmailVerification(
  email: string,
  turnstile?: string
): Promise<ApiResponse> {
  const res = await api.get('/api/verification', {
    params: { email, turnstile },
  })
  return res.data
}

// Bind email to OAuth account
export async function bindEmail(
  email: string,
  code: string
): Promise<ApiResponse> {
  const res = await api.post('/api/oauth/email/bind', {
    email,
    code,
  })
  return res.data
}
