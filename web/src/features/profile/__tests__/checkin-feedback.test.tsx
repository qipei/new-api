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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { CheckinCalendarCard } from '../components/checkin-calendar-card'

const originalAdapter = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})

test('a failed check-in displays exactly one error in the card', async () => {
  const errors = vi.spyOn(toast, 'error')
  api.defaults.adapter = async (config) => ({
    data:
      config.method === 'get'
        ? {
            success: true,
            data: {
              enabled: true,
              captcha_provider: 'none',
              stats: {
                checked_in_today: false,
                records: [],
                total_checkins: 0,
                total_quota: 0,
                checkin_count: 0,
              },
            },
          }
        : { success: false, message: 'check-in rejected' },
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={client}>
      <CheckinCalendarCard
        checkinEnabled
        turnstileEnabled={false}
        turnstileSiteKey=''
      />
    </QueryClientProvider>
  )
  try {
    await userEvent.click(
      await screen.findByRole('button', { name: 'Check in now' })
    )
    await waitFor(() =>
      expect(errors).toHaveBeenCalledWith('check-in rejected')
    )
    expect(errors).toHaveBeenCalledOnce()
  } finally {
    view.unmount()
    client.clear()
  }
})
