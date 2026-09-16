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
import { api } from '@/lib/api'

import type { CommissionOverride, CommissionOverridePayload } from './types'

interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

interface PagedData<T> {
  items: T[]
  total: number
}

export async function getCommissionOverrides(
  page: number,
  pageSize: number,
  keyword: string
): Promise<ApiResponse<PagedData<CommissionOverride>>> {
  const res = await api.get('/api/option/commission/overrides', {
    params: { p: page, page_size: pageSize, keyword },
  })
  return res.data
}

export async function saveCommissionOverride(
  payload: CommissionOverridePayload
): Promise<ApiResponse<CommissionOverride>> {
  const res = await api.post('/api/option/commission/overrides', payload)
  return res.data
}

export async function deleteCommissionOverride(
  id: number
): Promise<ApiResponse> {
  const res = await api.delete(`/api/option/commission/overrides/${id}`)
  return res.data
}
