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
/**
 * Utilities for managing authentication-related browser storage
 */

// ============================================================================
// LocalStorage Keys
// ============================================================================

const STORAGE_KEYS = {
  AFFILIATE: 'aff',
  LOGIN_AFFILIATE: 'login_affiliate',
  STATUS: 'status',
} as const

// ============================================================================
// Affiliate Code Storage
// ============================================================================

/**
 * Get affiliate code from localStorage
 */
export function getAffiliateCode(): string {
  if (typeof window === 'undefined') return ''
  try {
    return window.localStorage.getItem(STORAGE_KEYS.AFFILIATE) ?? ''
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to get affiliate code:', error)
    return ''
  }
}

/**
 * Save affiliate code to localStorage
 */
export function saveAffiliateCode(code: string): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(STORAGE_KEYS.AFFILIATE, code)
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save affiliate code:', error)
  }
}

/** Clear a consumed referral without removing a newer link opened in another tab. */
export function clearAffiliateCode(expectedCode?: string): void {
  if (typeof window === 'undefined') return
  try {
    if (
      expectedCode === undefined ||
      window.localStorage.getItem(STORAGE_KEYS.AFFILIATE) === expectedCode
    ) {
      window.localStorage.removeItem(STORAGE_KEYS.AFFILIATE)
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to clear affiliate code:', error)
  }
}

/** Keep the original referral across the second-factor screen and reloads. */
export function saveLoginAffiliateCode(flowToken: string, code: string): void {
  try {
    window.sessionStorage.setItem(
      STORAGE_KEYS.LOGIN_AFFILIATE,
      JSON.stringify({ flowToken, code })
    )
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save login affiliate code:', error)
  }
}

export function consumeLoginAffiliateCode(flowToken: string): void {
  try {
    const stored = window.sessionStorage.getItem(STORAGE_KEYS.LOGIN_AFFILIATE)
    if (!stored) return
    const pending: unknown = JSON.parse(stored)
    if (
      typeof pending !== 'object' ||
      pending === null ||
      !('flowToken' in pending) ||
      pending.flowToken !== flowToken ||
      !('code' in pending) ||
      typeof pending.code !== 'string'
    ) {
      return
    }
    clearAffiliateCode(pending.code)
    window.sessionStorage.removeItem(STORAGE_KEYS.LOGIN_AFFILIATE)
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to consume login affiliate code:', error)
  }
}
