import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  within,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { MiniAppQR } from '..'

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
})

function renderCard(
  config: { enabled: boolean; image_url: string },
  placement: 'home' | 'pricing' = 'home',
  success = true
) {
  api.defaults.adapter = async (request) => ({
    data: { success, data: config },
    status: 200,
    statusText: 'OK',
    headers: {},
    config: request,
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <MiniAppQR placement={placement} />
    </QueryClientProvider>
  )
  return client
}

test('opens the configured QR image with the keyboard and returns focus after closing', async () => {
  const user = userEvent.setup()
  renderCard({ enabled: true, image_url: '/custom-miniapp.png' })
  const trigger = await screen.findByRole('button', {
    name: 'Enlarge mini program QR code',
  })
  expect(screen.queryByText('Watch ads to earn tokens')).not.toBeInTheDocument()
  trigger.focus()
  await user.keyboard('{Enter}')
  const dialog = await screen.findByRole('dialog', {
    name: 'WeChat Mini Program',
  })
  expect(
    within(dialog).getByRole('img', { name: 'Mini program QR code' })
  ).toHaveAttribute('src', '/custom-miniapp.png')
  expect(
    within(dialog).getByRole('link', { name: 'Save QR code' })
  ).toHaveAttribute('href', '/custom-miniapp.png')
  await user.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(trigger).toHaveFocus()
})

test.each(['home', 'pricing'] as const)(
  'disabled configuration hides the entire %s card',
  async (placement) => {
    const client = renderCard(
      { enabled: false, image_url: '/custom-miniapp.png' },
      placement
    )
    await waitFor(() => expect(client.isFetching()).toBe(0))
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  }
)

test('failed configuration request leaves no stale promotion visible', async () => {
  const client = renderCard(
    { enabled: true, image_url: '/custom-miniapp.png' },
    'home',
    false
  )
  await waitFor(() => expect(client.isFetching()).toBe(0))
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

test('broken configured image hides the unusable card', async () => {
  renderCard({ enabled: true, image_url: '/missing.png' })
  fireEvent.error(
    await screen.findByRole('img', { name: 'Mini program QR code' })
  )
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})
