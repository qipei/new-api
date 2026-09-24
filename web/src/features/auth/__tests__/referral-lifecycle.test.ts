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
import { afterEach, expect, test } from 'vitest'

import { api, applyAuthBundle } from '@/lib/api'
import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

import { login, login2fa, phoneLogin, register } from '../api'
import { getAffiliateCode, saveAffiliateCode } from '../lib/storage'

const bundle: AuthBundle = {
  access_token: 'token',
  token_type: 'Bearer',
  access_expires_at: 1900000000,
  user: { id: 1, username: 'invitee', role: 1 },
  session: {
    sid: 'referral-login',
    current: true,
    login_method: 'phone',
    ip: '127.0.0.1',
    user_agent: 'test',
    created_at: 1,
    last_active_at: 1,
    expires_at: 1900000000,
  },
}

const originalAdapter = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = originalAdapter
  localStorage.removeItem('aff')
  sessionStorage.clear()
  useAuthStore.getState().auth.reset('idle')
})

test.each([true, false])(
  'registration success=%s consumes the referral only after success',
  async (success) => {
    saveAffiliateCode('PG3A')
    api.defaults.adapter = async (config) => {
      expect(JSON.parse(config.data as string).aff_code).toBe('PG3A')
      return {
        data: { success },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    await register({
      username: 'invitee',
      password: 'password123',
      aff_code: getAffiliateCode(),
    })
    expect(getAffiliateCode()).toBe(success ? '' : 'PG3A')
  }
)

test('a referral opened while registration is pending is not consumed by the earlier registration', async () => {
  saveAffiliateCode('PG3A')
  api.defaults.adapter = async (config) => {
    saveAffiliateCode('NEXT')
    return {
      data: { success: true },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  await register({
    username: 'invitee',
    password: 'password123',
    aff_code: 'PG3A',
  })
  expect(getAffiliateCode()).toBe('NEXT')
})

test.each(['', 'NEXT'])(
  'phone login preserves its referral through two-factor authentication (new link: %s)',
  async (newerCode) => {
    saveAffiliateCode('PG3A')
    api.defaults.adapter = async (config) => {
      expect(JSON.parse(config.data as string).aff_code).toBe('PG3A')
      return {
        data: {
          success: true,
          data: { require_2fa: true, flow_token: 'flow' },
        },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    await phoneLogin({
      phone: '13800138888',
      code: '123456',
      aff_code: getAffiliateCode(),
    })
    expect(getAffiliateCode()).toBe('PG3A')
    if (newerCode) saveAffiliateCode(newerCode)
    api.defaults.adapter = async (config) => ({
      data: { success: true, data: bundle },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    })
    await login2fa({ code: '123456', flow_token: 'flow' })
    expect(getAffiliateCode()).toBe(newerCode)
  }
)

test.each(['phone', 'password'])(
  '%s login consumes only its own referral after full authentication',
  async (method) => {
    for (const newerCode of ['', 'NEXT']) {
      saveAffiliateCode('PG3A')
      api.defaults.adapter = async (config) => {
        if (newerCode) saveAffiliateCode(newerCode)
        return {
          data: { success: true, data: bundle },
          status: 200,
          statusText: 'OK',
          headers: {},
          config,
        }
      }
      if (method === 'phone') {
        await phoneLogin({
          phone: '13800138888',
          code: '123456',
          aff_code: 'PG3A',
        })
      } else {
        await login({ username: 'invitee', password: 'password123' })
      }
      expect(getAffiliateCode()).toBe(newerCode)
    }
  }
)

test('refreshing an authenticated session does not consume a newly opened referral', () => {
  saveAffiliateCode('NEXT')
  applyAuthBundle(bundle, false)
  expect(getAffiliateCode()).toBe('NEXT')
})
