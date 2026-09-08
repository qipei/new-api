import { describe, expect, it } from 'vitest'

import type { PricingModel } from '../../types'
import {
  displayGroupRatio,
  resolveDisplayGroup,
  withDisplayGroupPricing,
} from '../group-price-rank'

const CHEAP_EXPR = 'tier("base", p * 2.4 + c * 4.5 + cr * 0.9)'
const PRICEY_EXPR =
  'len > 0 && len <= 32000 ? tier("base", p * 4 + c * 18) : tier("第2档", p * 6 + c * 22)'

function model(overrides: Partial<PricingModel> = {}): PricingModel {
  return {
    model_name: 'glm-5',
    quota_type: 0,
    model_ratio: 1,
    model_price: 0,
    owner_by: '',
    completion_ratio: 1,
    enable_groups: ['default', 'tok'],
    group_ratio: { default: 1, tok: 1 },
    billing_mode: 'tiered_expr',
    billing_expr: PRICEY_EXPR,
    group_billing_expr: { tok: CHEAP_EXPR },
    ...overrides,
  } as PricingModel
}

/** 覆盖当天的活动，避免测试随日期失效。 */
function runningPromotion(ratio: number, groups?: string[]) {
  return {
    name: '限时活动',
    start: '2000-01-01',
    end: '2099-12-31',
    ratio,
    groups,
  }
}

describe('resolveDisplayGroup', () => {
  // 这是改动的由来：两个分组倍率都是 1，只有表达式不同。此前只比倍率，会打平后
  // 落到模型级表达式，把最贵的那条当成"最低价"展示。
  it('picks the cheaper expression when group ratios tie', () => {
    expect(resolveDisplayGroup(model())).toBe('tok')
  })

  it('picks the cheaper ratio when expressions tie', () => {
    const m = model({
      group_billing_expr: undefined,
      group_ratio: { default: 1, tok: 0.5 },
    })
    expect(resolveDisplayGroup(m)).toBe('tok')
  })

  // 贵表达式 × 深折扣可以反超便宜表达式 × 原价，两个因子必须一起比。
  it('weighs the expression against the group ratio', () => {
    const m = model({ group_ratio: { default: 0.1, tok: 1 } })
    expect(resolveDisplayGroup(m)).toBe('default')
  })

  it('lets a running promotion decide the cheapest group', () => {
    const m = model({
      group_billing_expr: undefined,
      promotions: [runningPromotion(0.5, ['default'])],
    })
    expect(resolveDisplayGroup(m)).toBe('default')
  })

  it('honours an explicitly selected group', () => {
    expect(resolveDisplayGroup(model(), 'default')).toBe('default')
  })

  it('ignores a selected group the model does not serve', () => {
    expect(resolveDisplayGroup(model(), '7折')).toBe('tok')
    expect(resolveDisplayGroup(model(), 'all')).toBe('tok')
  })

  // 页面上没有请求可读，param()/header() 求不出来。这种分组只能排最后：退回比
  // 倍率会拿一个 1 附近的数去和别人的"单价 × 倍率"比，量纲差几个数量级，等于让
  // 它稳稳排第一。
  it('ranks a group whose expression cannot be evaluated last', () => {
    const m = model({
      billing_expr: 'tier("base", p * 4 + c * 18)',
      group_billing_expr: {
        tok: 'param("mode") == "fast" ? tier("a", p) : tier("b", p * 0.1)',
      },
      group_ratio: { default: 0.5, tok: 1 },
    })
    expect(resolveDisplayGroup(m)).toBe('default')
  })

  // 一个都算不出来时也得给出稳定的结果，页面总要显示一个分组。
  it('still returns a stable group when nothing can be evaluated', () => {
    const broken = 'param("mode") == "fast" ? tier("a", p) : tier("b", p)'
    const m = model({
      billing_expr: broken,
      group_billing_expr: { tok: broken },
    })
    expect(resolveDisplayGroup(m)).toBe('default')
  })

  // 分时表达式必须按此刻求值，否则闲时/忙时会一直按同一档比。
  it('follows the current time tier', () => {
    const night = `(hour("Asia/Shanghai") >= 22 || hour("Asia/Shanghai") < 8) ? tier("闲时", p * 1 + c * 1) : tier("忙时", p * 100 + c * 100)`
    const m = model({
      billing_expr: 'tier("固定", p * 20 + c * 20)',
      group_billing_expr: { tok: night },
    })
    const hour = Number(
      new Intl.DateTimeFormat('en-US', {
        timeZone: 'Asia/Shanghai',
        hour12: false,
        hour: 'numeric',
      }).format(new Date())
    ) % 24
    const offPeak = hour >= 22 || hour < 8
    expect(resolveDisplayGroup(m)).toBe(offPeak ? 'tok' : 'default')
  })

  // 口径是"输入 + 输出 + 缓存读"，缓存单价差别不能被忽略。
  it('counts the cache read price', () => {
    const m = model({
      billing_expr: 'tier("base", p * 1 + c * 1 + cr * 0.1)',
      group_billing_expr: { tok: 'tier("base", p * 1 + c * 1 + cr * 5)' },
    })
    expect(resolveDisplayGroup(m)).toBe('default')
  })

  it('compares ratios only for models that are not expression-billed', () => {
    const m = model({
      billing_mode: undefined,
      billing_expr: undefined,
      group_billing_expr: undefined,
      group_ratio: { default: 1, tok: 0.8 },
    })
    expect(resolveDisplayGroup(m)).toBe('tok')
  })

  // 同价时按分组名排，否则 enable_groups 的顺序一变，页面上的价格就换一个分组。
  it('breaks ties by group name regardless of input order', () => {
    const ratios = { a: 1, b: 1, c: 1 }
    const forward = model({
      enable_groups: ['c', 'b', 'a'],
      group_ratio: ratios,
      group_billing_expr: undefined,
    })
    const reverse = model({
      enable_groups: ['a', 'b', 'c'],
      group_ratio: ratios,
      group_billing_expr: undefined,
    })
    expect(resolveDisplayGroup(forward)).toBe('a')
    expect(resolveDisplayGroup(reverse)).toBe('a')
  })

  // 按倍率计费的模型没有表达式可换，withDisplayGroupPricing 不会碰它；卡片上的
  // 分组标签只能靠现算，算错就会出现"标着 default、价格却是活动价"。
  it('still resolves for models the pricing pipeline leaves untouched', () => {
    const m = model({
      billing_mode: undefined,
      billing_expr: undefined,
      group_billing_expr: undefined,
      promotions: [runningPromotion(0.5, ['tok'])],
    })
    expect(withDisplayGroupPricing([m])[0]).toBe(m)
    expect(resolveDisplayGroup(m)).toBe('tok')
    expect(displayGroupRatio(m)).toBe(0.5)
  })

  it('returns undefined when the model has no groups', () => {
    expect(resolveDisplayGroup(model({ enable_groups: [] }))).toBeUndefined()
  })
})

describe('displayGroupRatio', () => {
  // 后端把活动倍率乘进了实际计费的 GroupRatio；页面不乘就会显示原价而按活动价扣费。
  it('multiplies in a running promotion', () => {
    const m = model({
      group_billing_expr: undefined,
      group_ratio: { default: 0.8, tok: 0.8 },
      promotions: [runningPromotion(0.5, ['default'])],
    })
    expect(displayGroupRatio(m)).toBeCloseTo(0.4)
  })

  it('uses the selected group over the cheapest one', () => {
    const m = model({ group_ratio: { default: 1, tok: 0.5 } })
    expect(displayGroupRatio(m, 'default')).toBe(1)
  })

  // 表达式被替换成某个分组的覆盖之后，重算展示分组必须还得到同一个——否则倍率
  // 和已替换的表达式会来自两个分组。
  it('stays stable after the expression has been swapped', () => {
    const [swapped] = withDisplayGroupPricing([
      model({ group_ratio: { default: 1, tok: 0.5 } }),
    ])
    expect(resolveDisplayGroup(swapped)).toBe('tok')
    expect(displayGroupRatio(swapped)).toBe(0.5)
  })

  it('preserves a valid zero ratio', () => {
    const m = model({
      group_billing_expr: undefined,
      group_ratio: { default: 0, tok: 1 },
    })
    expect(displayGroupRatio(m)).toBe(0)
  })
})

describe('withDisplayGroupPricing', () => {
  it('swaps in the cheapest group expression', () => {
    const [resolved] = withDisplayGroupPricing([model()])
    expect(resolved.billing_expr).toBe(CHEAP_EXPR)
  })

  it('swaps in the selected group expression', () => {
    const [resolved] = withDisplayGroupPricing([model()], 'tok')
    expect(resolved.billing_expr).toBe(CHEAP_EXPR)
  })

  // 下游一堆 memo 以模型数组引用为依赖，没有替换时必须原样返回。
  it('returns the same array when no expression changes', () => {
    const models = [
      model({ group_billing_expr: undefined }),
      model({ model_name: 'other', group_billing_expr: undefined }),
    ]
    expect(withDisplayGroupPricing(models)).toBe(models)
    expect(withDisplayGroupPricing(models, 'default')).toBe(models)
    expect(withDisplayGroupPricing([], 'tok')).toEqual([])
  })

  it('leaves models without an override untouched', () => {
    const plain = model({ model_name: 'plain', group_billing_expr: undefined })
    const swapped = model()
    const result = withDisplayGroupPricing([plain, swapped])
    expect(result[0]).toBe(plain)
    expect(result[1]).not.toBe(swapped)
  })

  // 选中分组的覆盖会替换 billing_expr，其它分组回落时必须拿到模型级原件，
  // 否则详情里的"按分组定价"会把选中分组的价格当成所有分组的价格。
  it('keeps the model expression as the fallback after a swap', () => {
    const [swapped] = withDisplayGroupPricing([model()])
    expect(swapped.base_billing_expr).toBe(PRICEY_EXPR)
  })

  it('does not re-capture the base on a second swap', () => {
    const once = withDisplayGroupPricing([model()])
    const twice = withDisplayGroupPricing(once, 'default')
    expect(twice[0].billing_expr).toBe(PRICEY_EXPR)
    expect(twice[0].base_billing_expr).toBe(PRICEY_EXPR)
  })
})
