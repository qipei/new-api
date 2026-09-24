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
import { CustomerServiceSection } from '../customer-service-section'

const originalAdapter = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = originalAdapter
})

test('administrator can save a phone with an uploaded QR code and remove the image', async () => {
  const saved: unknown[] = []
  api.defaults.adapter = async (config) => {
    let data = { phone: '', qrcode_data_url: '' }
    if (config.method === 'put') {
      data = JSON.parse(config.data as string)
      saved.push(data)
    }
    return {
      data: { success: true, data },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const actions = document.createElement('div')
  document.body.appendChild(actions)
  render(
    <QueryClientProvider client={client}>
      <SettingsPageProvider actionsContainer={actions}>
        <CustomerServiceSection />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  const phone = await screen.findByLabelText('Customer service phone')
  await userEvent.type(phone, '13800138000')
  await userEvent.upload(
    screen.getByLabelText('Customer service QR code'),
    new File([new Uint8Array(500 * 1024)], 'contact.png', { type: 'image/png' })
  )
  await screen.findByAltText('Customer service QR code preview')
  await userEvent.click(
    screen.getByRole('button', { name: 'Save customer service settings' })
  )
  await waitFor(() =>
    expect(saved).toEqual([
      {
        phone: '13800138000',
        qrcode_data_url: expect.stringMatching(/^data:image\/png;base64,/),
      },
    ])
  )
  await userEvent.click(screen.getByRole('button', { name: 'Remove QR code' }))
  await userEvent.click(
    screen.getByRole('button', { name: 'Save customer service settings' })
  )
  await waitFor(() => expect(saved).toHaveLength(2))
  expect(saved[1]).toEqual({ phone: '13800138000', qrcode_data_url: '' })
  const large = new File([new Uint8Array(500 * 1024 + 1)], 'large.png', {
    type: 'image/png',
  })
  fireEvent.change(screen.getByLabelText('Customer service QR code'), {
    target: { files: [large] },
  })
  expect(
    await screen.findByText(
      'Use a PNG or JPEG image up to 500 KB and 2048 × 2048 pixels.'
    )
  ).toBeInTheDocument()
  expect(
    screen.queryByAltText('Customer service QR code preview')
  ).not.toBeInTheDocument()
  actions.remove()
})
