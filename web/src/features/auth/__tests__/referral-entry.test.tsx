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
  createRootRoute,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  createMemoryHistory,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { generateAffiliateLink } from '@/features/wallet/lib/affiliate'
import * as api from '@/lib/api'
import { parseRouterSearch, stringifyRouterSearch } from '@/lib/router-search'
import { Route as InviteRoute } from '@/routes/(auth)/invite'
import { Route as SignInRoute } from '@/routes/(auth)/sign-in'
import { Route as SignUpRoute } from '@/routes/(auth)/sign-up'
import { Route as RootRoute } from '@/routes/__root'
import { useAuthStore } from '@/stores/auth-store'

import { getAffiliateCode } from '../lib/storage'
import { SignUp } from '../sign-up'

vi.mock('@/lib/auth-session', () => ({
  bootstrapAuthentication: vi.fn().mockResolvedValue(undefined),
  clearAuthenticatedClientState: vi.fn(),
  clearAuthentication: vi.fn(),
}))
vi.mock('@/features/setup/api', () => ({
  getSetupStatus: vi
    .fn()
    .mockResolvedValue({ success: true, data: { status: true } }),
}))

const status = {
  register_enabled: true,
  password_register_enabled: false,
  phone_login_enabled: true,
}
const clients: QueryClient[] = []
beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
})
afterEach(() => {
  clients.forEach((client) => client.clear())
  clients.length = 0
  localStorage.clear()
  useAuthStore.getState().auth.reset('idle')
})

async function setup(
  path: string,
  flags: Record<string, boolean> = status,
  statusError = false
) {
  const getStatus = vi.spyOn(api, 'getStatus')
  if (statusError) getStatus.mockRejectedValue(new Error('Offline'))
  else getStatus.mockResolvedValue(flags)
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const root = createRootRouteWithContext<{ queryClient: QueryClient }>()({
    beforeLoad: (ctx) => RootRoute.options.beforeLoad?.(ctx),
    onEnter: (match) =>
      RootRoute.options.onEnter?.({ ...match, fullPath: '/' }),
    onStay: (match) => RootRoute.options.onStay?.({ ...match, fullPath: '/' }),
  })
  // The fixture tree has different generated route identities; callbacks receive
  // the same runtime query client and validated search context as production.
  const auth = createRoute({ getParentRoute: () => root, id: '(auth)' })
  const signup = createRoute({
    getParentRoute: () => auth,
    path: '/sign-up',
    beforeLoad: (ctx) =>
      SignUpRoute.options.beforeLoad?.(
        ctx as unknown as Parameters<
          NonNullable<typeof SignUpRoute.options.beforeLoad>
        >[0]
      ),
    validateSearch: SignUpRoute.options.validateSearch,
  })
  const signin = createRoute({
    getParentRoute: () => auth,
    path: '/sign-in',
    beforeLoad: (ctx) =>
      SignInRoute.options.beforeLoad?.(
        ctx as unknown as Parameters<
          NonNullable<typeof SignInRoute.options.beforeLoad>
        >[0]
      ),
    validateSearch: SignInRoute.options.validateSearch,
  })
  const dashboard = createRoute({
    getParentRoute: () => root,
    path: '/dashboard',
  })
  const invite = createRoute({
    getParentRoute: () => auth,
    path: '/invite',
    beforeLoad: (ctx) =>
      InviteRoute.options.beforeLoad?.(
        ctx as unknown as Parameters<
          NonNullable<typeof InviteRoute.options.beforeLoad>
        >[0]
      ),
    validateSearch: InviteRoute.options.validateSearch,
  })
  const router = createRouter({
    routeTree: root.addChildren([
      auth.addChildren([signup, signin, invite]),
      dashboard,
    ]),
    parseSearch: parseRouterSearch,
    stringifySearch: stringifyRouterSearch,
    context: { queryClient: client },
    history: createMemoryHistory({ initialEntries: [path] }),
  })
  await router.load()
  return { router, client }
}

test('legacy phone-only referral redirects with the code already captured', async () => {
  const { router } = await setup('/sign-up?aff=fbrO')
  expect(router.state.location.pathname).toBe('/sign-in')
  expect(router.state.location.search).toMatchObject({ aff: 'fbrO' })
  expect(getAffiliateCode()).toBe('fbrO')
})

test('signup hides username registration when its switch is off', async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  })
  clients.push(client)
  client.setQueryData(['status'], { ...status, phone_login_enabled: false })
  const root = createRootRoute()
  const signup = createRoute({
    getParentRoute: () => root,
    path: '/sign-up',
    component: SignUp,
  })
  const router = createRouter({
    routeTree: root.addChildren([signup]),
    history: createMemoryHistory({ initialEntries: ['/sign-up'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  expect(await screen.findByText('Registration is closed')).toBeVisible()
  expect(screen.queryByLabelText('Username')).not.toBeInTheDocument()
})

test('generated web referral links use the stable invite entry', () => {
  expect(generateAffiliateLink('fbrO')).toBe(
    `${window.location.origin}/invite?aff=fbrO`
  )
})

test('SPA navigation captures a new referral before the login page opens', async () => {
  const { router } = await setup('/sign-in')
  await router.navigate({ to: '/sign-up', search: { aff: 'NEXT' } })
  expect(getAffiliateCode()).toBe('NEXT')
  expect(router.state.location.pathname).toBe('/sign-in')
})

test('preloading an invitation does not replace the current referral', async () => {
  const { router } = await setup('/sign-in?aff=FIRST')
  await router.preloadRoute({ to: '/sign-up', search: { aff: 'PRELOAD' } })
  expect(getAffiliateCode()).toBe('FIRST')
})

test.each([
  [true, false, true, '/sign-in'],
  [true, true, true, '/sign-in'],
  [false, true, true, '/sign-up'],
  [false, true, false, '/sign-in'],
  [false, false, true, '/sign-in'],
])(
  'invite with phone=%s passwordRegistration=%s registration=%s selects %s',
  async (phone, password, registration, destination) => {
    const { router } = await setup('/invite?aff=fbrO', {
      phone_login_enabled: phone,
      password_register_enabled: password,
      register_enabled: registration,
    })
    expect(router.state.location.pathname).toBe(destination)
    expect(router.state.location.search).toMatchObject({ aff: 'fbrO' })
    expect(getAffiliateCode()).toBe('fbrO')
  }
)

test('legacy signup retains the username option when both methods are enabled', async () => {
  const { router } = await setup('/sign-up?aff=fbrO', {
    ...status,
    password_register_enabled: true,
  })
  expect(router.state.location.pathname).toBe('/sign-up')
  expect(getAffiliateCode()).toBe('fbrO')
})

test('signed-in visitors go to dashboard without storing another user referral', async () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'existing', role: 1 })
  const { router } = await setup('/invite?aff=fbrO')
  expect(router.state.location.pathname).toBe('/dashboard')
  expect(getAffiliateCode()).toBe('')
})

test('self-use mode sends invites to login instead of password registration', async () => {
  const { router } = await setup('/invite?aff=fbrO', {
    ...status,
    phone_login_enabled: false,
    password_register_enabled: true,
    self_use_mode_enabled: true,
  })
  expect(router.state.location.pathname).toBe('/sign-in')
})

test('failed status lookup keeps the invite on an error route without guessing a registration method', async () => {
  const { router } = await setup('/invite?aff=fbrO', status, true)
  expect(router.state.location.pathname).toBe('/invite')
  expect(router.state.matches.some((match) => match.status === 'error')).toBe(
    true
  )
})

test('an empty referral parameter does not erase a saved referral', async () => {
  const { router } = await setup('/sign-in?aff=FIRST')
  await router.navigate({ to: '/sign-up', search: { aff: '   ' } })
  expect(getAffiliateCode()).toBe('FIRST')
})

test.each(['1234', '1e03', 'true', 'null'])(
  'invite preserves the original text of JSON-like affiliate code %s',
  async (aff) => {
    const { router } = await setup(`/invite?aff=${aff}`)
    expect(router.state.location.pathname).toBe('/sign-in')
    expect(router.state.location.search).toMatchObject({ aff })
    expect(getAffiliateCode()).toBe(aff)
    expect(
      new URLSearchParams(router.state.location.searchStr).get('aff')
    ).toBe(aff)
  }
)

test('the referral parser preserves the existing JSON handling for other search parameters', () => {
  const input = {
    aff: '1e03',
    page: 2,
    filter: { active: true },
    redirect: '/wallet',
  }
  expect(parseRouterSearch(stringifyRouterSearch(input))).toEqual(input)
})

test('signup waits for status instead of exposing a registration form during loading', async () => {
  vi.spyOn(api, 'getStatus').mockImplementation(() => new Promise(() => {}))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute()
  const signup = createRoute({
    getParentRoute: () => root,
    path: '/sign-up',
    component: SignUp,
  })
  const router = createRouter({
    routeTree: root.addChildren([signup]),
    history: createMemoryHistory({ initialEntries: ['/sign-up'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  expect(await screen.findByRole('status')).toHaveTextContent('Loading...')
  expect(screen.queryByLabelText('Username')).not.toBeInTheDocument()
})
