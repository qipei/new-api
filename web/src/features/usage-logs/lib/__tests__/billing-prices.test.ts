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
import { describe, expect, it } from 'vitest'

import type { LogOtherData } from '../../types'
import { getTieredBillingSummary } from '../format'

function billingLog(overrides: Partial<LogOtherData> = {}): LogOtherData {
  return {
    billing_mode: 'tiered_expr',
    expr_b64: btoa('tier("busy", p * 2 + c * 8 + cr * 0.2)'),
    matched_tier: 'busy',
    group_ratio: 1,
    ...overrides,
  }
}

describe('settled log unit prices', () => {
  it('uses the historical final ratio including promotion instead of the exclusive ratio', () => {
    const summary = getTieredBillingSummary(
      billingLog({
        group_ratio: 0.4,
        user_group_ratio: 0.8,
        promotion_ratio: 0.5,
        promotion_name: 'Summer',
        cache_tokens: 100,
      })
    )
    expect(summary?.priceEntries.map((entry) => entry.price)).toEqual([
      0.8,
      3.2,
      expect.closeTo(0.08, 10),
    ])
  })

  it('includes only matched request multipliers in the settled price', () => {
    const summary = getTieredBillingSummary(
      billingLog({
        expr_b64: btoa(
          '(tier("busy", p * 2 + c * 8)) * (param("fast") == true ? 2 : 1) * (header("sale") == "yes" ? 0.5 : 1)'
        ),
        group_ratio: 0.4,
        request_rules: [
          { cond: 'param("fast") == true', multiplier: 2, matched: true },
          { cond: 'header("sale") == "yes"', multiplier: 0.5, matched: false },
        ],
      })
    )
    expect(summary?.priceEntries.map((entry) => entry.price)).toEqual([
      1.6, 6.4,
    ])
  })

  it('shows zero prices when the final group is free', () => {
    const summary = getTieredBillingSummary(billingLog({ group_ratio: 0 }))
    expect(summary?.priceEntries.map((entry) => entry.price)).toEqual([0, 0])
  })

  it('does not invent a uniform unit price for a multiplier inside a tier', () => {
    const summary = getTieredBillingSummary(
      billingLog({
        expr_b64: btoa(
          'tier("busy", p * 2 * (param("fast") == true ? 2 : 1) + c * 8)'
        ),
        request_rules: [
          { cond: 'param("fast") == true', multiplier: 2, matched: true },
        ],
      })
    )
    expect(summary).toBeNull()
  })

  it('does not infer historical request rule matches from current conditions', () => {
    const summary = getTieredBillingSummary(
      billingLog({
        expr_b64: btoa(
          '(tier("busy", p * 2 + c * 8)) * (param("fast") == true ? 2 : 1)'
        ),
      })
    )
    expect(summary).toBeNull()
  })

  it('falls back to an exclusive ratio only when no final group ratio was recorded', () => {
    const summary = getTieredBillingSummary(
      billingLog({ group_ratio: undefined, user_group_ratio: 0.5 })
    )
    expect(summary?.priceEntries.map((entry) => entry.price)).toEqual([1, 4])
  })

  it('preserves undiscounted legacy prices and does not guess an unknown tier', () => {
    expect(
      getTieredBillingSummary(
        billingLog({ group_ratio: undefined })
      )?.priceEntries.map((entry) => entry.price)
    ).toEqual([2, 8])
    expect(
      getTieredBillingSummary(billingLog({ matched_tier: 'missing' }))
    ).toBeNull()
  })
})
