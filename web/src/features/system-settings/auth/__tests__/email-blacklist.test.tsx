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
import { BasicAuthSection } from '../basic-auth-section'

const originalAdapter = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = originalAdapter
})
test.each([true, false])(
  'saving the blacklist before enabling it handles success=%s without changing the whitelist',
  async (success) => {
    const saved: Array<{ key: string; value: string | boolean }> = []
    api.defaults.adapter = async (config) => {
      saved.push(JSON.parse(config.data as string))
      return {
        data: { success, message: success ? '' : 'Invalid domain' },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const actions = document.createElement('div')
    document.body.appendChild(actions)
    const view = render(
      <QueryClientProvider client={queryClient}>
        <SettingsPageProvider actionsContainer={actions}>
          <BasicAuthSection
            defaultValues={{
              PhoneLoginEnabled: true,
              TwoFAEnabled: true,
              PasswordLoginEnabled: true,
              PasswordRegisterEnabled: true,
              EmailVerificationEnabled: true,
              RegisterEnabled: false,
              EmailDomainRestrictionEnabled: false,
              EmailAliasRestrictionEnabled: false,
              EmailDomainWhitelist: 'qq.com',
              EmailDomainBlacklistEnabled: false,
              EmailDomainBlacklist: '',
            }}
          />
        </SettingsPageProvider>
      </QueryClientProvider>
    )
    try {
      fireEvent.change(screen.getByLabelText('Email Domain Blacklist'), {
        target: { value: 'maildrop.cc\n example.org ' },
      })
      await userEvent.click(
        screen.getByRole('switch', { name: 'Enable Email Domain Blacklist' })
      )
      await userEvent.click(
        screen.getByRole('button', { name: 'Save Changes' })
      )
      await waitFor(() =>
        expect(
          screen.getByRole('button', { name: 'Save Changes' })
        ).not.toBeDisabled()
      )
      expect(saved).toEqual(
        success
          ? [
              { key: 'EmailDomainBlacklist', value: 'maildrop.cc,example.org' },
              { key: 'EmailDomainBlacklistEnabled', value: true },
            ]
          : [{ key: 'EmailDomainBlacklist', value: 'maildrop.cc,example.org' }]
      )
      expect(
        screen.getByRole('switch', { name: 'Email Domain Restriction' })
      ).not.toBeChecked()
    } finally {
      view.unmount()
      actions.remove()
      queryClient.clear()
    }
  }
)
