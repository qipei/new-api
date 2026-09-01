import { describe, expect, it } from 'vitest'

import type { PricingModel } from '../../types'
import {
  groupsWithBillingExprOverride,
  resolveBillingExprForGroup,
  withGroupBillingExpr,
} from '../group-billing-expr'

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
})

describe('withGroupBillingExpr', () => {
  it('swaps in the group expression', () => {
    const models = [model({ group_billing_expr: { '8.8折': NIGHT_EXPR } })]
    expect(withGroupBillingExpr(models, '8.8折')[0].billing_expr).toBe(
      NIGHT_EXPR
    )
  })

  // 下游一堆 memo 以模型数组引用为依赖，没有替换时必须原样返回。
  it('returns the same array when nothing needs swapping', () => {
    const models = [model(), model({ model_name: 'other' })]
    expect(withGroupBillingExpr(models, 'default')).toBe(models)
    expect(withGroupBillingExpr(models, 'all')).toBe(models)
    expect(withGroupBillingExpr(models, undefined)).toBe(models)
  })

  it('leaves models without an override untouched', () => {
    const plain = model({ model_name: 'plain' })
    const swapped = model({ group_billing_expr: { '8.8折': NIGHT_EXPR } })
    const result = withGroupBillingExpr([plain, swapped], '8.8折')
    expect(result[0]).toBe(plain)
    expect(result[1]).not.toBe(swapped)
  })
})

describe('groupsWithBillingExprOverride', () => {
  it('lists only enabled groups whose expression actually differs', () => {
    const m = model({
      group_billing_expr: {
        '8.8折': NIGHT_EXPR,
        default: MODEL_EXPR, // 与模型级相同，不算差异
        '5折': NIGHT_EXPR, // 该模型未启用这个分组
        blank: '  ',
      },
    })
    expect(groupsWithBillingExprOverride(m)).toEqual(['8.8折'])
  })

  it('returns nothing when the model has no overrides', () => {
    expect(groupsWithBillingExprOverride(model())).toEqual([])
  })
})

// 选中分组的覆盖会替换 billing_expr，其它分组回落时必须拿到模型级原件，
// 否则详情里的"按分组定价"会把选中分组的价格当成所有分组的价格。
describe('base expression preservation', () => {
  it('keeps resolving other groups against the model expression after a swap', () => {
    const original = model({
      group_billing_expr: { '8.8折': NIGHT_EXPR },
    })
    const [swapped] = withGroupBillingExpr([original], '8.8折')

    expect(swapped.billing_expr).toBe(NIGHT_EXPR)
    expect(resolveBillingExprForGroup(swapped, 'default')).toBe(MODEL_EXPR)
    expect(resolveBillingExprForGroup(swapped, '8.8折')).toBe(NIGHT_EXPR)
    expect(groupsWithBillingExprOverride(swapped)).toEqual(['8.8折'])
  })

  it('does not re-capture the base on a second swap', () => {
    const original = model({
      group_billing_expr: { '8.8折': NIGHT_EXPR, other: 'tier("x", p)' },
    })
    const once = withGroupBillingExpr([original], '8.8折')
    const twice = withGroupBillingExpr(once, 'other')
    expect(twice[0].base_billing_expr).toBe(MODEL_EXPR)
    expect(resolveBillingExprForGroup(twice[0], 'default')).toBe(MODEL_EXPR)
  })
})
