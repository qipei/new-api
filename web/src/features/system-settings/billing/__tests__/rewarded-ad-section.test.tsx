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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test } from 'vitest'

import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { SettingsPageProvider } from '../../components/settings-page-context'
import {
  RewardedAdSection,
  type RewardedAdConfig,
} from '../rewarded-ad-section'

const originalAdapter = api.defaults.adapter
const originalCurrency = useSystemConfigStore.getState().config.currency
let actions: HTMLDivElement
const config: RewardedAdConfig = {
  enabled: false,
  reward_quota: 1000,
  daily_limit: 3,
  cooldown_seconds: 60,
  daily_budget_quota: 100000,
  adpid: 'ad-test',
  dcloud_app_id: '__UNI__TEST',
  wechat_app_id: 'wx-test',
  bridge_url: 'https://bridge.example.com',
  s2s_auth_mode: 'connectCode',
  secret_configured: true,
  wechat_secret_configured: true,
  s2s_key_configured: true,
  callback_url: 'https://token01.net/api/rewarded-ad/dcloud/callback',
  session_ttl_seconds: 900,
}
afterEach(() => {
  useSystemConfigStore.getState().setConfig({ currency: originalCurrency })
  api.defaults.adapter = originalAdapter
  actions?.remove()
})
function renderSettings(
  options: {
    failLoad?: boolean
    failSave?: boolean
    noSecrets?: boolean
    s2s_auth_mode?: 'sign' | 'connectCode'
  } = {}
) {
  const saved: Record<string, unknown>[] = []
  const initial = {
    ...config,
    s2s_auth_mode: options.s2s_auth_mode ?? config.s2s_auth_mode,
    secret_configured: !options.noSecrets,
  }
  api.defaults.adapter = async (request) => {
    let data = initial
    if (request.method === 'put') {
      const values = JSON.parse(request.data as string) as Record<
        string,
        unknown
      >
      saved.push(values)
      data = { ...initial, ...values } as RewardedAdConfig
    }
    return {
      data: {
        success: !(
          options.failLoad ||
          (options.failSave && request.method === 'put')
        ),
        data,
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config: request,
    }
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  actions = document.createElement('div')
  document.body.appendChild(actions)
  render(
    <QueryClientProvider client={client}>
      <SettingsPageProvider actionsContainer={actions}>
        <RewardedAdSection />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  return { saved, client }
}

test('load failure shows retry and prevents configuration changes', async () => {
  renderSettings({ failLoad: true })
  await screen.findByRole('button', { name: 'Retry' })
  expect(screen.queryByRole('switch')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Save Changes' })
  ).not.toBeInTheDocument()
})

test('administrator enables rewards with saved credentials and secrets stay blank', async () => {
  const user = userEvent.setup()
  const { saved, client } = renderSettings()
  const toggle = await screen.findByRole('switch', {
    name: 'Enable ad token rewards',
  })
  expect(toggle).not.toBeChecked()
  expect(screen.getByLabelText('DCloud callback secret')).toHaveValue('')
  expect(screen.getByLabelText('uniCloud S2S connectCode')).toHaveValue('')
  await user.click(toggle)
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(saved).toHaveLength(1))
  expect(saved[0]).toMatchObject({
    enabled: true,
    secret: '',
    s2s_auth_key: '',
    reward_quota: 1000,
  })
  await waitFor(() =>
    expect(
      client.getQueryData<RewardedAdConfig>(['rewarded-ad-settings'])?.enabled
    ).toBe(true)
  )
  expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled()
})

test('missing callback secret prevents enabling rewards and explains missing configuration', async () => {
  const user = userEvent.setup()
  const { saved } = renderSettings({ noSecrets: true })
  await user.click(
    await screen.findByRole('switch', { name: 'Enable ad token rewards' })
  )
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await screen.findByRole('alert')
  expect(saved).toHaveLength(0)
})

test('negative rewards are rejected before sending configuration', async () => {
  const { saved } = renderSettings()
  const input = await screen.findByLabelText('Reward per ad (quota points)')
  fireEvent.change(input, { target: { value: '-1' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(input).toHaveAttribute('aria-invalid', 'true'))
  expect(saved).toHaveLength(0)
})

test('save failure preserves original configuration and allows retry', async () => {
  const user = userEvent.setup()
  const { saved, client } = renderSettings({ failSave: true })
  await user.click(
    await screen.findByRole('switch', { name: 'Enable ad token rewards' })
  )
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(saved).toHaveLength(1))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
  )
  expect(
    client.getQueryData<RewardedAdConfig>(['rewarded-ad-settings'])?.enabled
  ).toBe(false)
})

test('short S2S signing key is rejected and callback URL remains read only', async () => {
  const { saved } = renderSettings({ s2s_auth_mode: 'sign' })
  const input = await screen.findByLabelText('uniCloud S2S signKey')
  fireEvent.change(input, { target: { value: 'short-key' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(input).toHaveAttribute('aria-invalid', 'true'))
  expect(saved).toHaveLength(0)
  expect(
    screen.getByRole('textbox', { name: 'Server callback URL' })
  ).toHaveAttribute('readonly')
})

test('changing reward and budget quota updates their currency previews using the configured exchange rate', async () => {
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CNY',
      usdExchangeRate: 7.3,
    },
  })
  renderSettings()
  const reward = await screen.findByLabelText('Reward per ad (quota points)')
  const budget = screen.getByLabelText('Daily ad reward budget (quota points)')
  expect(reward).toHaveAccessibleDescription(/Equivalent to ¥0.0146/)
  expect(budget).toHaveAccessibleDescription(/Equivalent to ¥1.46/)
  fireEvent.change(reward, { target: { value: '10000' } })
  fireEvent.change(budget, { target: { value: '1000000' } })
  expect(reward).toHaveAccessibleDescription(/Equivalent to ¥0.146/)
  expect(budget).toHaveAccessibleDescription(/Equivalent to ¥14.6/)
  expect(reward).toHaveValue(10000)
  expect(budget).toHaveValue(1000000)
})

test('mini program tab is selected and contains the WeChat-only configuration', async () => {
  const user = userEvent.setup()
  renderSettings()
  const tab = screen.getByRole('tab', { name: 'Mini Program' })
  expect(tab).toHaveAttribute('aria-selected', 'true')
  expect(screen.getAllByRole('tab')).toHaveLength(1)
  expect(
    screen.getByRole('tabpanel', { name: 'Mini Program' })
  ).toHaveTextContent(
    'These settings apply only to rewarded video ads in the WeChat mini program.'
  )
  await screen.findByRole('switch', { name: 'Enable ad token rewards' })
  await user.click(tab)
  await user.keyboard('{ArrowRight}')
  expect(tab).toHaveFocus()
  expect(tab).toHaveAttribute('aria-selected', 'true')
})

test('callback copy action shares the compact address control and copies its value', async () => {
  const user = userEvent.setup()
  renderSettings()
  const group = await screen.findByRole('group', {
    name: 'Server callback URL',
  })
  const address = within(group).getByRole('textbox', {
    name: 'Server callback URL',
  })
  const copy = within(group).getByRole('button', { name: 'Copy' })
  expect(address).toHaveValue(config.callback_url)
  expect(address).toHaveAttribute('readonly')
  expect(group).toHaveClass('flex')
  expect(copy).not.toHaveClass('w-full')
  await user.click(copy)
  expect(await navigator.clipboard.readText()).toBe(config.callback_url)
})

test('bridge authentication selector shows only the selected credential and saves its mode', async () => {
  const user = userEvent.setup()
  const { saved } = renderSettings()
  const selector = await screen.findByRole('combobox', {
    name: 'uniCloud S2S authentication method',
  })
  await user.click(selector)
  await user.click(
    await screen.findByRole('option', { name: 'Signature (sign)' })
  )
  const key = screen.getByLabelText('uniCloud S2S signKey')
  expect(
    screen.queryByLabelText('uniCloud S2S connectCode')
  ).not.toBeInTheDocument()
  await user.type(key, '0123456789abcdef0123456789abcdef')
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(saved).toHaveLength(1))
  expect(saved[0]).toMatchObject({
    s2s_auth_mode: 'sign',
    s2s_auth_key: '0123456789abcdef0123456789abcdef',
  })
  expect(saved[0]).not.toHaveProperty('callback_auth_key')
  expect(saved[0]).not.toHaveProperty('bridge_auth_key')
  expect(
    screen.queryByLabelText('uniCloud S2S signing key')
  ).not.toBeInTheDocument()
  await user.click(
    screen.getByRole('combobox', { name: 'uniCloud S2S authentication method' })
  )
  await user.click(
    await screen.findByRole('option', { name: 'Shared code (connectCode)' })
  )
  expect(screen.getByLabelText('uniCloud S2S connectCode')).toBeInTheDocument()
  expect(
    screen.queryByLabelText('uniCloud S2S signKey')
  ).not.toBeInTheDocument()
})

test('changing bridge authentication with a blank credential preserves the saved key', async () => {
  const user = userEvent.setup()
  const { saved } = renderSettings()
  const selector = await screen.findByRole('combobox', {
    name: 'uniCloud S2S authentication method',
  })
  expect(screen.getByLabelText('uniCloud S2S connectCode')).toHaveValue('')
  expect(
    screen.queryByLabelText('uniCloud S2S signKey')
  ).not.toBeInTheDocument()
  await user.click(selector)
  await user.click(
    await screen.findByRole('option', { name: 'Signature (sign)' })
  )
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(saved).toHaveLength(1))
  expect(saved[0]).toMatchObject({
    s2s_auth_mode: 'sign',
    s2s_auth_key: '',
  })
})
