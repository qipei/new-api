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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { SMSSection, type FlatSMSDefaults } from '../sms-section'

const originalAdapter = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = originalAdapter
})

test('first-time captcha setup saves credentials before enabling verification', async () => {
  const defaults: FlatSMSDefaults = {
    'sms.local_only': false,
    'sms.debug_code': '123456',
    'sms.endpoint': '',
    'sms.access_key_id': '',
    'sms.access_key_secret': '',
    'sms.sign_name': '',
    'sms.template_code': '',
    'sms.code_length': 6,
    'sms.code_expire_minutes': 10,
    'sms.resend_interval_sec': 60,
    'sms.daily_limit': 1000,
    'sms.phone_daily_limit': 10,
    'sms.ip_daily_limit': 50,
    'sms_captcha.enabled': false,
    'sms_captcha.captcha_app_id': '',
    'sms_captcha.app_secret_key': '',
    'sms_captcha.mini_app_id': '',
    'sms_captcha.mini_app_secret_key': '',
    'sms_captcha.secret_id': '',
    'sms_captcha.secret_key': '',
    'sms_captcha.phone_trigger_count': 3,
    'sms_captcha.ip_trigger_count': 8,
    'sms_captcha.window_seconds': 600,
  }
  const saved: string[] = []
  api.defaults.adapter = async (config) => {
    const data = JSON.parse(config.data as string) as { key: string }
    saved.push(data.key)
    return {
      data: { success: true },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const actions = document.createElement('div')
  document.body.appendChild(actions)
  const view = render(
    <QueryClientProvider client={queryClient}>
      <SettingsPageProvider actionsContainer={actions}>
        <SMSSection defaultValues={defaults} />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  fireEvent.change(screen.getByLabelText('Web/App CaptchaAppId'), {
    target: { value: '123' },
  })
  fireEvent.change(screen.getByLabelText('Web/App AppSecretKey'), {
    target: { value: 'app-secret' },
  })
  fireEvent.change(screen.getByLabelText('Tencent Cloud SecretId'), {
    target: { value: 'secret-id' },
  })
  fireEvent.change(screen.getByLabelText('Tencent Cloud SecretKey'), {
    target: { value: 'secret-key' },
  })
  fireEvent.change(
    screen.getByLabelText('Mini-program CAPTCHA application ID'),
    { target: { value: '456' } }
  )
  fireEvent.change(
    screen.getByLabelText('Mini-program CAPTCHA application secret'),
    { target: { value: 'mini-secret' } }
  )
  await userEvent.click(
    screen.getByRole('switch', { name: 'SMS Anti-abuse Captcha' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(saved).toHaveLength(7))
  expect(saved.at(-1)).toBe('sms_captcha.enabled')
  expect(saved).toContain('sms_captcha.mini_app_secret_key')
  await waitFor(() =>
    expect(
      screen.getByLabelText('Mini-program CAPTCHA application secret')
    ).toHaveValue('')
  )
  view.unmount()
  actions.remove()
  queryClient.clear()
})
