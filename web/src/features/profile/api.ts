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
import {
  openTencentCaptcha,
  type TencentCaptchaTicket,
} from '@/features/auth/lib/tencent-captcha'
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
import { api } from '@/lib/api'
import type { CustomOAuthBinding } from '@/lib/oauth'
import type { LoginSession } from '@/stores/auth-store'

import type {
  ApiResponse,
  UserProfile,
  UpdateUserRequest,
  UpdateUserSettingsRequest,
  DeleteAccountRequest,
  CheckinStatusResponse,
  CheckinResponse,
} from './types'

// ============================================================================
// User Profile APIs
// ============================================================================

/**
 * Get current user profile
 */
export async function getUserProfile(): Promise<ApiResponse<UserProfile>> {
  const res = await api.get('/api/user/self')
  return res.data
}

/**
 * Update user profile
 */
export async function updateUserProfile(
  data: UpdateUserRequest
): Promise<ApiResponse> {
  const res = await api.put('/api/user/self', data, {
    acceptAuthRotation: Boolean(data.password),
  })
  return res.data
}

/**
 * Set the first login password for an account that has none yet
 * (registered by phone or third-party login). The current session stays valid.
 */
export async function setInitialPassword(
  password: string
): Promise<ApiResponse> {
  const res = await api.post(
    '/api/user/self/password',
    { password },
    { skipBusinessError: true }
  )
  return res.data
}

/**
 * Update user settings
 */
export async function updateUserSettings(
  data: UpdateUserSettingsRequest
): Promise<ApiResponse> {
  const res = await api.put('/api/user/setting', data)
  return res.data
}

/**
 * Update interface language preference
 */
export async function updateUserLanguage(
  language: string
): Promise<ApiResponse> {
  const res = await api.put('/api/user/self', { language })
  return res.data
}

/**
 * Delete user account
 */
export async function deleteUserAccount(
  data?: DeleteAccountRequest
): Promise<ApiResponse> {
  const res = await api.delete('/api/user/self', { data })
  return res.data
}

/**
 * Generate/regenerate system access token
 */
export async function generateAccessToken(): Promise<ApiResponse<string>> {
  const res = await api.get('/api/user/token')
  return res.data
}

// ============================================================================
// Account Binding APIs
// ============================================================================

/**
 * Send email verification code
 */
export async function sendEmailVerification(
  email: string,
  turnstileToken?: string
): Promise<ApiResponse> {
  const params = new URLSearchParams({ email })
  if (turnstileToken) {
    params.append('turnstile', turnstileToken)
  }
  const res = await api.get(`/api/verification?${params}`)
  return res.data
}

/**
 * Bind email account
 */
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

/**
 * Bind WeChat account
 */
export async function bindWeChat(code: string): Promise<ApiResponse> {
  const res = await api.post(
    '/api/oauth/wechat/bind',
    { code },
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return res.data
}

export interface TelegramBindFlow {
  flow_token: string
  callback_url: string
  expires_at: number
}

export async function startTelegramBind(): Promise<
  ApiResponse<TelegramBindFlow>
> {
  const res = await api.post('/api/oauth/telegram/bind/start')
  return res.data
}

// ============================================================================
// Login Session APIs
// ============================================================================

export async function getLoginSessions(): Promise<ApiResponse<LoginSession[]>> {
  const res = await api.get('/api/user/sessions')
  return res.data
}

export async function revokeLoginSession(sid: string): Promise<ApiResponse> {
  const res = await api.delete(`/api/user/sessions/${encodeURIComponent(sid)}`)
  return res.data
}

export async function revokeOtherLoginSessions(): Promise<ApiResponse> {
  const res = await api.post('/api/user/sessions/revoke-others')
  return res.data
}

// ============================================================================
// Custom OAuth Binding APIs
// ============================================================================

/**
 * Get current user's custom OAuth bindings
 */
export async function getSelfOAuthBindings(): Promise<
  ApiResponse<CustomOAuthBinding[]>
> {
  const res = await api.get('/api/user/oauth/bindings')
  return res.data
}

/**
 * Unbind a custom OAuth provider for current user
 */
export async function unbindCustomOAuth(
  providerId: number
): Promise<ApiResponse> {
  const res = await api.delete(`/api/user/oauth/bindings/${providerId}`)
  return res.data
}

// ============================================================================
// Checkin APIs
// ============================================================================

/**
 * Get checkin status for a specific month
 */
export async function getCheckinStatus(
  month?: string,
  silent = false
): Promise<ApiResponse<CheckinStatusResponse>> {
  const url = month ? `/api/user/checkin?month=${month}` : '/api/user/checkin'
  const res = await api.get(url, {
    skipBusinessError: silent,
    skipErrorHandler: silent,
    disableDuplicate: silent,
  })
  return res.data
}

/**
 * Perform daily checkin
 */
export async function performCheckin(
  turnstileToken?: string,
  captcha?: TencentCaptchaTicket
): Promise<ApiResponse<CheckinResponse>> {
  const url = turnstileToken
    ? `/api/user/checkin?turnstile=${encodeURIComponent(turnstileToken)}`
    : '/api/user/checkin'
  const res = await api.post(
    url,
    captcha
      ? {
          captcha_client: 'web',
          captcha_ticket: captcha.ticket,
          captcha_randstr: captcha.randstr,
        }
      : undefined,
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return res.data
}

// The server chooses the provider. Retry once with a fresh Tencent ticket;
// cancellation and rejected tickets never trigger an automatic retry loop.
export async function performCheckinWithCaptcha(
  turnstileToken?: string
): Promise<ApiResponse<CheckinResponse> | null> {
  // Recheck eligibility before opening a paid CAPTCHA, including stale tabs.
  const status = await getCheckinStatus(undefined, true)
  if (!status.success || !status.data?.enabled) {
    return { success: false, message: status.message }
  }
  if (status.data.stats.checked_in_today) {
    return { success: false, code: 'CHECKIN_ALREADY_DONE' }
  }
  if (
    status.data.captcha_provider === 'tencent' &&
    status.data.captcha_app_id
  ) {
    const ticket = await openTencentCaptcha(status.data.captcha_app_id)
    if (!ticket) return null
    return performCheckin(undefined, ticket)
  }
  // Keep a one-time challenge fallback if configuration changes after the GET.
  const result = await performCheckin(turnstileToken)
  if (
    result.success ||
    !result.data?.require_captcha ||
    result.data.captcha_provider !== 'tencent' ||
    !result.data.captcha_app_id
  ) {
    return result
  }
  const ticket = await openTencentCaptcha(result.data.captcha_app_id)
  if (!ticket) return null
  return performCheckin(undefined, ticket)
}
