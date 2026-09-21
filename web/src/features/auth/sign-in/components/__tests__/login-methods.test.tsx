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
  createRoute,
  createRouter,
  createMemoryHistory,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Toaster } from 'sonner'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import * as authApi from '@/features/auth/api'

import { SignIn } from '../../index'

beforeEach(() => {
  // jsdom has no viewport scrolling; route navigation still runs normally.
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
})

afterEach(() => {
  window.localStorage.clear()
  vi.restoreAllMocks()
})

async function renderLogin(
  phone: boolean,
  password: boolean,
  registration = true,
  selfUseMode = false,
  legalRequired = false
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(['status'], {
    phone_login_enabled: phone,
    password_login_enabled: password,
    register_enabled: registration,
    self_use_mode_enabled: selfUseMode,
    user_agreement_enabled: legalRequired,
  })
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '(auth)' })
  const signIn = createRoute({
    getParentRoute: () => auth,
    path: 'sign-in',
    component: SignIn,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([signIn])]),
    history: createMemoryHistory({ initialEntries: ['/sign-in'] }),
  })
  await router.load()
  const view = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
      <Toaster />
    </QueryClientProvider>
  )
  return () => {
    view.unmount()
    client.clear()
  }
}

test('phone login is primary and old accounts can switch to password', async () => {
  const cleanup = await renderLogin(true, true)
  expect(await screen.findByLabelText('Mobile number')).toBeVisible()
  expect(screen.queryByRole('tablist')).not.toBeInTheDocument()
  expect(
    screen.queryByLabelText('Password', { selector: 'input' })
  ).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Password login' }))
  expect(screen.getByLabelText('Password', { selector: 'input' })).toBeVisible()
  expect(screen.queryByLabelText('Mobile number')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Phone login' }))
  expect(screen.getByLabelText('Mobile number')).toBeVisible()
  cleanup()
})

test('disabled phone login keeps the existing password form', async () => {
  const cleanup = await renderLogin(false, true)
  expect(
    await screen.findByLabelText('Password', { selector: 'input' })
  ).toBeVisible()
  expect(screen.queryByLabelText('Mobile number')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Phone login' })
  ).not.toBeInTheDocument()
  cleanup()
})

test('phone-only login hides the password alternative', async () => {
  const cleanup = await renderLogin(true, false)
  expect(
    screen.queryByRole('button', { name: 'Password login' })
  ).not.toBeInTheDocument()
  expect(await screen.findByLabelText('Mobile number')).toBeVisible()
  expect(
    screen.queryByLabelText('Password', { selector: 'input' })
  ).not.toBeInTheDocument()
  cleanup()
})

test('phone login explains automatic registration and hides the notice on the password form', async () => {
  const cleanup = await renderLogin(true, true)
  const notice =
    'A new account will be created automatically after verifying an unregistered mobile number.'
  expect(await screen.findByText(notice)).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Password login' }))
  expect(screen.queryByText(notice)).not.toBeInTheDocument()
  cleanup()
})

test('disabled registration retains phone auto-registration notice and hides signup', async () => {
  const cleanup = await renderLogin(true, false, false)
  expect(
    await screen.findByText(
      'A new account will be created automatically after verifying an unregistered mobile number.'
    )
  ).toBeVisible()
  expect(
    screen.queryByRole('link', { name: 'Sign up' })
  ).not.toBeInTheDocument()
  cleanup()
})

test.each([
  [true, false, true],
  [false, false, false],
  [true, true, false],
])(
  'registration enabled=%s and self-use=%s controls signup visibility',
  async (registration, selfUse, visible) => {
    const cleanup = await renderLogin(true, true, registration, selfUse)
    await screen.findByLabelText('Mobile number')
    const signup = screen.queryByRole('link', { name: 'Sign up' })
    if (visible) {
      expect(signup).toHaveAttribute('href', '/sign-up')
    } else {
      expect(signup).not.toBeInTheDocument()
    }
    cleanup()
  }
)

test('clicking get code without legal consent shows a reminder and sends no SMS', async () => {
  const sendSMS = vi
    .spyOn(authApi, 'sendLoginSMSCode')
    .mockResolvedValue({
      success: true,
      message: '',
      data: { require_captcha: false, expires_in: 300, resend_after: 60 },
    })
  const cleanup = await renderLogin(true, false, false, false, true)
  await userEvent.type(
    await screen.findByLabelText('Mobile number'),
    '13800138000'
  )
  const getCode = screen.getByRole('button', { name: 'Get code' })
  expect(getCode).toBeEnabled()
  await userEvent.click(getCode)
  expect(
    await screen.findByText('Please agree to the legal terms first')
  ).toBeVisible()
  expect(sendSMS).not.toHaveBeenCalled()
  cleanup()
})
