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
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { ThemeProvider } from '@/context/theme-provider'
import { Home } from '@/features/home'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { useNotificationStore } from '@/stores/notification-store'

import { PublicHeader } from '../public-header'

// The browser bundler handles this third-party JSON import; native Node requires an attribute.
vi.mock('@lobehub/icons', () => ({
  DeepSeek: undefined,
  Moonshot: undefined,
  Qwen: undefined,
  Zhipu: undefined,
  OpenAI: undefined,
  Claude: undefined,
  Gemini: undefined,
  OpenRouter: undefined,
}))

const adapter = api.defaults.adapter
const clients: QueryClient[] = []
let notice = 'Maintenance notice'
let navModules = ''
let homeContent = ''
let success = true
let fail = false
let requests = 0
let resolveNotice: (() => void) | undefined

beforeEach(() => {
  Object.defineProperty(Element.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
  localStorage.clear()
  useNotificationStore.setState({
    lastReadNotice: '',
    readAnnouncementKeys: [],
    closedUntilDate: null,
  })
  useAuthStore.getState().auth.setUser(null)
  notice = 'Maintenance notice'
  navModules = ''
  homeContent = ''
  success = true
  fail = false
  requests = 0
  resolveNotice = undefined
  api.defaults.adapter = async (config) => {
    if (config.url === '/api/notice') {
      requests++
      if (resolveNotice) {
        await new Promise<void>((resolve) => {
          resolveNotice = resolve
        })
      }
      if (fail) throw new Error('offline')
    }
    if (config.url === '/api/home_page_content') {
      return {
        data: { success: true, data: homeContent },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    if (config.url === '/api/pricing') {
      return {
        data: { success: true, data: [], vendors: [] },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    return {
      data:
        config.url === '/api/notice'
          ? { success, data: notice }
          : {
              success: true,
              data: {
                HeaderNavModules: navModules,
                announcements_enabled: true,
                announcements: [{ content: 'Timeline item', id: 1 }],
              },
            },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
})
afterEach(() => {
  api.defaults.adapter = adapter
  clients.forEach((client) => client.clear())
  clients.length = 0
  vi.restoreAllMocks()
})

async function mountHeader(path = '/', cached?: string, actualHome = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  if (cached) client.setQueryData(['notice'], { success: true, data: cached })
  const root = createRootRoute({
    component: () =>
      actualHome ? (
        <Home />
      ) : (
        <PublicHeader
          showThemeSwitch={false}
          showLanguageSwitcher={false}
          showAuthButtons={false}
        />
      ),
  })
  const home = createRoute({ getParentRoute: () => root, path: '/' })
  const about = createRoute({ getParentRoute: () => root, path: '/about' })
  const router = createRouter({
    routeTree: root.addChildren([home, about]),
    history: createMemoryHistory({ initialEntries: [path] }),
  })
  await router.load()
  const view = render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <RouterProvider router={router} />
      </ThemeProvider>
    </QueryClientProvider>
  )
  return { ...view, client, router }
}

test.each([false, true])(
  'home opens a single unread Notice dialog for authenticated=%s',
  async (loggedIn) => {
    if (loggedIn) {
      useAuthStore
        .getState()
        .auth.setUser({ id: 1, username: 'reader', role: 1 })
    }
    await mountHeader()
    const dialog = await screen.findByRole('dialog', {
      name: 'System Announcements',
    })
    expect(within(dialog).getByText(notice)).toBeVisible()
    expect(within(dialog).queryByText('Timeline item')).not.toBeInTheDocument()
    expect(requests).toBe(1)
    expect(useNotificationStore.getState().lastReadNotice).toBe('')
  }
)

test('closing persists the content, refresh stays quiet, and an edit beyond 100 chars prompts again', async () => {
  notice = `${'a'.repeat(120)}first`
  const first = await mountHeader()
  await screen.findByRole('dialog', { name: 'System Announcements' })
  await userEvent.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(useNotificationStore.getState().lastReadNotice).toBe(notice)
  first.unmount()
  const saved = localStorage.getItem('notification-storage') ?? ''
  useNotificationStore.setState({ lastReadNotice: '' })
  localStorage.setItem('notification-storage', saved)
  await useNotificationStore.persist.rehydrate()
  const second = await mountHeader()
  await waitFor(() => expect(second.client.isFetching()).toBe(0))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  second.unmount()
  notice = `${'a'.repeat(120)}edited`
  await mountHeader()
  expect(
    await screen.findByRole('dialog', { name: 'System Announcements' })
  ).toHaveTextContent('edited')
})

test.each(['empty', 'business failure', 'network failure'])(
  '%s never opens stale cached Notice',
  async (mode) => {
    if (mode === 'empty') notice = '  '
    if (mode === 'business failure') success = false
    if (mode === 'network failure') fail = true
    const view = await mountHeader('/', 'Stale cached notice')
    await waitFor(() => expect(view.client.isFetching()).toBe(0))
    expect(requests).toBe(1)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(useNotificationStore.getState().lastReadNotice).toBe('')
  }
)

test('cached notice waits for the current request before opening', async () => {
  resolveNotice = () => undefined
  const view = await mountHeader('/', 'Stale cached notice')
  await waitFor(() => expect(requests).toBe(1))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await act(async () => resolveNotice?.())
  expect(await screen.findByRole('dialog')).toHaveTextContent(notice)
  expect(view.client.getQueryData(['notice'])).toEqual({
    success: true,
    data: notice,
  })
})

test('non-home stays quiet, bell remains usable, and entering home revalidates', async () => {
  const view = await mountHeader('/about')
  await waitFor(() => expect(view.client.isFetching()).toBe(0))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Notifications' }))
  expect(await screen.findByText(notice)).toBeVisible()
  expect(useNotificationStore.getState().lastReadNotice).toBe(notice)
  await userEvent.keyboard('{Escape}')
  notice = 'Edited notice'
  await act(async () => {
    await view.router.navigate({ to: '/' })
  })
  expect(
    await screen.findByRole('dialog', { name: 'System Announcements' })
  ).toHaveTextContent(notice)
})

test('closing with unavailable storage keeps the page usable and allows manual bell viewing', async () => {
  await mountHeader()
  await screen.findByRole('dialog', { name: 'System Announcements' })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('quota exceeded')
  })
  await userEvent.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  await userEvent.click(screen.getByRole('button', { name: 'Notifications' }))
  expect(await screen.findByText(notice)).toBeVisible()
})

test.each([
  '',
  '<h1>Custom HTML home</h1>',
  '# Custom Markdown home',
  'https://example.com/home',
])('Notice opens for homepage content %s', async (content) => {
  homeContent = content
  await mountHeader('/', undefined, true)
  expect(
    await screen.findByRole('dialog', { name: 'System Announcements' })
  ).toHaveTextContent(notice)
  await waitFor(() =>
    expect(screen.queryByText('Loading...')).not.toBeInTheDocument()
  )
  expect(requests).toBe(1)
})

test('a pending notice waits for the mobile menu to close', async () => {
  resolveNotice = () => undefined
  await mountHeader()
  await userEvent.click(
    screen.getByRole('button', { name: 'Toggle navigation menu' })
  )
  await act(async () => resolveNotice?.())
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(useNotificationStore.getState().lastReadNotice).toBe('')
  await userEvent.click(
    screen.getByRole('button', { name: 'Toggle navigation menu' })
  )
  expect(
    await screen.findByRole('dialog', { name: 'System Announcements' })
  ).toHaveTextContent(notice)
})

test('opening the bell during a pending request prevents a second overlay', async () => {
  resolveNotice = () => undefined
  await mountHeader()
  await userEvent.click(screen.getByRole('button', { name: 'Notifications' }))
  await act(async () => resolveNotice?.())
  expect(await screen.findByText(notice)).toBeVisible()
  expect(screen.getAllByRole('dialog')).toHaveLength(1)
  await userEvent.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(useNotificationStore.getState().lastReadNotice).toBe(notice)
})

test('a pending Notice waits for the sign-in prompt to close', async () => {
  navModules = JSON.stringify({
    home: true,
    pricing: { enabled: true, requireAuth: true },
  })
  resolveNotice = () => undefined
  await mountHeader()
  const links = await screen.findAllByRole('link', { name: 'Model Square' })
  await userEvent.click(links[0])
  await screen.findByRole('dialog', { name: 'Sign in required' })
  await act(async () => resolveNotice?.())
  expect(screen.getAllByRole('dialog')).toHaveLength(1)
  expect(
    screen.queryByRole('dialog', { name: 'System Announcements' })
  ).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(
    await screen.findByRole('dialog', { name: 'System Announcements' })
  ).toHaveTextContent(notice)
})
