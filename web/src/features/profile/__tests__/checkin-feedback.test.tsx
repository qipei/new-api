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
