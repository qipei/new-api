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

import type { PricingModel } from '../../types'
import { resolveBillingExprForGroup } from '../group-billing-expr'

const MODEL_EXPR = 'tier("base", p * 3 + c * 9)'
const NIGHT_EXPR =
  'hour("Asia/Shanghai") >= 22 ? tier("night", p * 1.5) : tier("day", p * 3)'

function model(overrides: Partial<PricingModel> = {}): PricingModel {
  return {
    model_name: 'deepseek-v4-flash-0731',
    quota_type: 0,
    model_ratio: 1,
    model_price: 0,
    owner_by: '',
    completion_ratio: 1,
    enable_groups: ['default', '8.8折'],
    billing_mode: 'tiered_expr',
    billing_expr: MODEL_EXPR,
    ...overrides,
  } as PricingModel
}

describe('resolveBillingExprForGroup', () => {
  const withOverride = model({ group_billing_expr: { '8.8折': NIGHT_EXPR } })

  it.each([
    ['a group with an override', '8.8折', NIGHT_EXPR],
    ['a group without one', 'default', MODEL_EXPR],
    ['the all-groups filter', 'all', MODEL_EXPR],
    ['no group at all', undefined, MODEL_EXPR],
  ])('uses the right expression for %s', (_label, group, expected) => {
    expect(resolveBillingExprForGroup(withOverride, group)).toBe(expected)
  })

  it('treats a blank override as no override', () => {
    const blank = model({ group_billing_expr: { '8.8折': '   ' } })
    expect(resolveBillingExprForGroup(blank, '8.8折')).toBe(MODEL_EXPR)
  })

  // billing_expr 会被换成当前展示分组的覆盖，其它分组回落时必须拿到模型级原件，
  // 否则详情里的"按分组定价"会把展示分组的价格当成所有分组的价格。
  it('falls back to the base expression once billing_expr has been swapped', () => {
    const swapped = model({
      group_billing_expr: { '8.8折': NIGHT_EXPR },
      billing_expr: NIGHT_EXPR,
      base_billing_expr: MODEL_EXPR,
    })
    expect(resolveBillingExprForGroup(swapped, 'default')).toBe(MODEL_EXPR)
    expect(resolveBillingExprForGroup(swapped, '8.8折')).toBe(NIGHT_EXPR)
  })
})
