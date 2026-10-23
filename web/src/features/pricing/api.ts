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

import type { ModelVoiceCatalog, PricingData } from './types'

// ----------------------------------------------------------------------------
// Pricing APIs
// ----------------------------------------------------------------------------

// Get model pricing data
export async function getPricing(): Promise<PricingData> {
  const res = await api.get('/api/pricing')
  return res.data
}

// CUSTOM: 模型的预置音色目录（fork 扩展）。不是语音模型时后端返回 404，这里返回 null。
export async function getModelVoices(
  model: string
): Promise<ModelVoiceCatalog | null> {
  const res = await api.get<{ success: boolean; data: ModelVoiceCatalog }>(
    '/api/pricing/voices',
    {
      params: { model },
      skipBusinessError: true,
      skipErrorHandler: true,
      validateStatus: (status: number) => status === 200 || status === 404,
    }
  )
  if (res.status === 404 || !res.data.success) return null
  return res.data.data
}
