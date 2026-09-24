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
import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'

import { isPasswordRegistrationOpen } from '@/features/auth/lib/registration'
import { getStatus } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/(auth)/invite')({
  validateSearch: z.object({ aff: z.string().trim().optional() }),
  beforeLoad: async ({ context, search }) => {
    if (useAuthStore.getState().auth.user) {
      throw redirect({ to: '/dashboard', replace: true })
    }
    const status = await context.queryClient.fetchQuery({
      queryKey: ['status'],
      queryFn: getStatus,
      staleTime: 5 * 60 * 1000,
    })
    if (!status) throw new Error('System status unavailable')
    const destination =
      !status.phone_login_enabled && isPasswordRegistrationOpen(status)
        ? '/sign-up'
        : '/sign-in'
    throw redirect({
      to: destination,
      search: { aff: search.aff },
      replace: true,
    })
  },
})
