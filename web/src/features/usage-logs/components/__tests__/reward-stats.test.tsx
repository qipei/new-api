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
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import { CommonLogsStats } from '../common-logs-stats'
import { UsageLogsProvider } from '../usage-logs-provider'

describe('reward log statistics', () => {
  test.each(['100', '101'])(
    'hides consumption statistics when filtering reward type %s',
    async (type) => {
      const rootRoute = createRootRoute({ component: Outlet })
      const authenticated = createRoute({
        getParentRoute: () => rootRoute,
        id: '_authenticated',
        component: Outlet,
      })
      const logs = createRoute({
        getParentRoute: () => authenticated,
        path: 'usage-logs/$section',
        component: () => (
          <>
            <span>Reward filter loaded</span>
            <CommonLogsStats />
          </>
        ),
      })
      const router = createRouter({
        routeTree: rootRoute.addChildren([authenticated.addChildren([logs])]),
        history: createMemoryHistory({
          initialEntries: [
            `/usage-logs/common?type=${encodeURIComponent(JSON.stringify([type]))}`,
          ],
        }),
      })
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      })
      render(
        <QueryClientProvider client={queryClient}>
          <UsageLogsProvider>
            <RouterProvider router={router} />
          </UsageLogsProvider>
        </QueryClientProvider>
      )
      await waitFor(() =>
        expect(screen.getByText('Reward filter loaded')).toBeInTheDocument()
      )
      expect(screen.queryByText('Usage')).not.toBeInTheDocument()
      expect(screen.queryByText('RPM')).not.toBeInTheDocument()
      expect(screen.queryByText('TPM')).not.toBeInTheDocument()
      expect(queryClient.isFetching()).toBe(0)
      queryClient.clear()
    }
  )
})
