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

import { MiniAppQR } from '@/components/mini-app-qr'
import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { MiniAppQRSection } from '../miniapp-qr-section'

const originalAdapter = api.defaults.adapter
let actions: HTMLDivElement

afterEach(() => {
  api.defaults.adapter = originalAdapter
  actions?.remove()
})

function renderSettings(failSave = false) {
  const saved: unknown[] = []
  api.defaults.adapter = async (config) => {
    let data = { enabled: true, image_url: '/miniapp-code.jpg' }
    if (config.method === 'put') {
      data = JSON.parse(config.data as string)
      saved.push(data)
    }
    return {
      data: { success: !(failSave && config.method === 'put'), data },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
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
        <MiniAppQRSection />
        <MiniAppQR />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  return { saved, client }
}

test('administrator replaces image URL then disables the public card using the keyboard', async () => {
  const user = userEvent.setup()
  const { saved } = renderSettings()
  const input = await screen.findByLabelText('Mini program QR image URL')
  await screen.findByRole('button', { name: 'Enlarge mini program QR code' })
  await user.clear(input)
  await user.type(input, '/new-code.png')
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(saved).toEqual([{ enabled: true, image_url: '/new-code.png' }])
  )
  const toggle = screen.getByRole('switch', {
    name: 'Show mini program QR code',
  })
  toggle.focus()
  await user.keyboard(' ')
  expect(toggle).not.toBeChecked()
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(saved).toHaveLength(2))
  expect(saved[1]).toEqual({ enabled: false, image_url: '/new-code.png' })
  await waitFor(() =>
    expect(
      screen.queryByRole('button', { name: 'Enlarge mini program QR code' })
    ).not.toBeInTheDocument()
  )
})

test('invalid image URL is rejected without saving and unavailable images have an accessible fallback', async () => {
  const user = userEvent.setup()
  const { saved } = renderSettings()
  const input = await screen.findByLabelText('Mini program QR image URL')
  await user.clear(input)
  await user.type(input, 'javascript:alert(1)')
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Enter an HTTP(S) image URL or a site-relative path.'
  )
  expect(input).toHaveAttribute('aria-invalid', 'true')
  expect(saved).toEqual([])
  await user.clear(input)
  await user.type(input, '/missing.png')
  fireEvent.error(
    screen.getAllByRole('img', { name: 'Mini program QR code' })[0]
  )
  expect(screen.getByRole('status')).toHaveTextContent(
    'Unable to load the QR image. Check the image URL.'
  )
})

test('failed save retains unsaved changes and keeps the public configuration unchanged', async () => {
  const user = userEvent.setup()
  const { client } = renderSettings(true)
  const input = await screen.findByLabelText('Mini program QR image URL')
  await screen.findByRole('button', { name: 'Enlarge mini program QR code' })
  await user.clear(input)
  await user.type(input, '/new-code.png')
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(client.isMutating()).toBe(0))
  expect(input).toHaveValue('/new-code.png')
  expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
  expect(client.getQueryData(['miniapp-qr-code'])).toEqual({
    enabled: true,
    image_url: '/miniapp-code.jpg',
  })
})
