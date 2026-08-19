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
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
} from '@/features/pricing/lib/dynamic-price'
import { stripTrailingZeros } from '@/features/pricing/lib/price'
import type { PricingModel } from '@/features/pricing/types'
import type { ModelRanking } from '@/features/rankings/types'

export const HOME_MODEL_LIMIT = 6

export function selectHomeModels(rows: ModelRanking[]): ModelRanking[] {
  return [...rows].sort((a, b) => a.rank - b.rank).slice(0, HOME_MODEL_LIMIT)
}

export function formatFirstTierHomePrice(
  model: PricingModel,
  priceRate: number,
  usdExchangeRate: number
): string | undefined {
  const summary = getDynamicPricingSummary(model, {
    tokenUnit: 'M',
    showRechargePrice: false,
    priceRate,
    usdExchangeRate,
    groupRatioMultiplier: getDynamicDisplayGroupRatio(model),
  })
  const input = summary?.primaryEntries.find(
    (entry) => entry.field === 'inputPrice'
  )
  const output = summary?.primaryEntries.find(
    (entry) => entry.field === 'outputPrice'
  )

  if (!input || !output) return undefined

  return `${stripTrailingZeros(input.formatted)} / ${stripTrailingZeros(output.formatted)}`
}
