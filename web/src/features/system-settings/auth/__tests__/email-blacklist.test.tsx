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
