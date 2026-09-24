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
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test } from 'vitest'

import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { CheckinSettingsSection } from '../checkin-settings-section'

const originalConfig = useSystemConfigStore.getState().config
const originalAdapter = api.defaults.adapter
afterEach(() => {
  useSystemConfigStore.setState({ config: originalConfig })
  api.defaults.adapter = originalAdapter
})

function renderCheckinSettings(
  enabled = true,
  actionsContainer: HTMLDivElement | null = null
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <SettingsPageProvider actionsContainer={actionsContainer}>
        <CheckinSettingsSection
          defaultValues={{ enabled, minQuota: 1000, maxQuota: 10000 }}
        />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
}

test('CNY rewards preview the configured exchange rate and update while typing', () => {
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CNY',
      usdExchangeRate: 7.3,
    },
  })
  renderCheckinSettings()

  expect(screen.getByText(/500,000 quota points = 1 USD\./)).toBeInTheDocument()
  expect(screen.getByText(/1 USD = ¥7.3/)).toBeInTheDocument()
  expect(screen.getByText('Equivalent to ¥0.0146')).toBeInTheDocument()
  expect(screen.getByText('Equivalent to ¥0.146')).toBeInTheDocument()
  fireEvent.change(
    screen.getByLabelText('Maximum check-in reward (quota points)'),
    {
      target: { value: '100000' },
    }
  )
  expect(screen.getByText('Equivalent to ¥1.46')).toBeInTheDocument()
})

test('enabling Tencent check-in saves only its independent protection switch', async () => {
  const saved: Array<{ key: string; value: string }> = []
  api.defaults.adapter = async (config) => {
    saved.push(JSON.parse(config.data as string))
    return {
      data: { success: true },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  const actions = document.createElement('div')
  document.body.appendChild(actions)
  const view = renderCheckinSettings(true, actions)
  try {
    await userEvent.click(
      screen.getByRole('switch', {
        name: 'Protect check-in with Tencent CAPTCHA',
      })
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Save check-in settings' })
    )
    await waitFor(() =>
      expect(saved).toEqual([
        { key: 'checkin_setting.captcha_enabled', value: 'true' },
      ])
    )
  } finally {
    view.unmount()
    actions.remove()
  }
})

test('changing display currency updates previews without changing the quota inputs', () => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, usdExchangeRate: 7.3 },
  })
  renderCheckinSettings()
  expect(screen.getByText('Equivalent to $0.002')).toBeInTheDocument()

  act(() => {
    useSystemConfigStore.getState().setConfig({
      currency: {
        ...DEFAULT_CURRENCY_CONFIG,
        quotaDisplayType: 'CUSTOM',
        customCurrencySymbol: '€',
        customCurrencyExchangeRate: 0.8,
        quotaPerUnit: 200000,
      },
    })
  })
  expect(screen.getByText(/200,000 quota points = 1 USD\./)).toBeInTheDocument()
  expect(screen.getByText('Equivalent to € 0.004')).toBeInTheDocument()
  expect(
    screen.getByLabelText('Minimum check-in reward (quota points)')
  ).toHaveValue(1000)
})

test('quota-only mode explains the unit even when check-in is disabled', () => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'TOKENS' },
  })
  renderCheckinSettings(false)
  expect(screen.getByText('500,000 quota points = 1 USD.')).toBeInTheDocument()
  expect(screen.getByText(/not currency or model tokens/)).toBeInTheDocument()
  expect(
    screen.queryByText(/At the current display rate/)
  ).not.toBeInTheDocument()
  expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
})
