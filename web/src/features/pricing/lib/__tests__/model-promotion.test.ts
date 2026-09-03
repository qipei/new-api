import { describe, expect, it } from 'vitest'

import type { ModelPromotionInfo } from '../../types'
import {
  activePromotionForGroup,
  bestDiscount,
  effectiveGroupRatio,
  formatDiscountLabel,
  hasAnyActivePromotion,
} from '../model-promotion'

const promo = (over: Partial<ModelPromotionInfo> = {}): ModelPromotionInfo => ({
  name: '开学季',
  start: '2026-08-30',
  end: '2026-09-10',
  ratio: 0.5,
  ...over,
})

describe('activePromotionForGroup', () => {
  it('applies to every group when no group is listed', () => {
    const model = { promotions: [promo()] }
    expect(activePromotionForGroup(model, '任意分组')?.ratio).toBe(0.5)
  })

  it('honours a group-scoped promotion', () => {
    const model = { promotions: [promo({ groups: ['8.8折'] })] }
    expect(activePromotionForGroup(model, '8.8折')?.ratio).toBe(0.5)
    expect(activePromotionForGroup(model, 'default')).toBeNull()
  })

  // 和后端 ActivePromotionAt 一致：多条命中取最低，结果不受顺序影响。
  it('picks the lowest ratio regardless of order', () => {
    const ascending = {
      promotions: [promo({ ratio: 0.5 }), promo({ name: '三折', ratio: 0.3 })],
    }
    const descending = { promotions: [...ascending.promotions].reverse() }
    expect(activePromotionForGroup(ascending, 'default')?.ratio).toBe(0.3)
    expect(activePromotionForGroup(descending, 'default')?.ratio).toBe(0.3)
  })

  it('ignores nonsensical ratios instead of zeroing the price', () => {
    const model = {
      promotions: [promo({ ratio: 0 }), promo({ ratio: Number.NaN })],
    }
    expect(activePromotionForGroup(model, 'default')).toBeNull()
  })

  it('returns null when there are no promotions', () => {
    expect(activePromotionForGroup({}, 'default')).toBeNull()
    expect(activePromotionForGroup({ promotions: [] }, 'default')).toBeNull()
  })
})

// 展示价必须和实际扣费一致：后端把活动乘进了 GroupRatio，前端也要乘。
describe('effectiveGroupRatio', () => {
  it('multiplies the group ratio by the promotion', () => {
    const model = { promotions: [promo({ groups: ['8.8折'] })] }
    expect(effectiveGroupRatio(model, '8.8折', 0.88)).toBeCloseTo(0.44)
  })

  it('leaves untouched groups alone', () => {
    const model = { promotions: [promo({ groups: ['8.8折'] })] }
    expect(effectiveGroupRatio(model, 'default', 1)).toBe(1)
  })

  it('is a no-op without promotions', () => {
    expect(effectiveGroupRatio({}, 'default', 0.65)).toBe(0.65)
  })
})

describe('hasAnyActivePromotion', () => {
  it.each([
    [{ promotions: [promo()] }, true],
    [{ promotions: [] }, false],
    [{}, false],
  ])('%o -> %s', (model, expected) => {
    expect(hasAnyActivePromotion(model)).toBe(expected)
  })
})

// 列表和卡片上的"最低折扣"。折扣有两个来源——分组倍率本身小于 1，或者倍率为 1
// 的分组上挂了限时活动——只看其中一个都会漏。
describe('bestDiscount', () => {
  it('takes the cheapest group ratio', () => {
    expect(
      bestDiscount({
        enable_groups: ['default', '8.3折', '9.5折'],
        group_ratio: { default: 1, '8.3折': 0.83, '9.5折': 0.95 },
      })
    ).toMatchObject({ ratio: 0.83, group: '8.3折', promotion: null })
  })

  // default 倍率是 1，本来算不上折扣，挂了活动之后就成了最便宜的。
  it('finds a discount that only exists because of a promotion', () => {
    const result = bestDiscount({
      enable_groups: ['default'],
      group_ratio: { default: 1 },
      promotions: [promo({ ratio: 0.5, groups: ['default'] })],
    })
    expect(result?.ratio).toBeCloseTo(0.5)
    expect(result?.promotion?.name).toBe('开学季')
  })

  // 活动叠在已经打折的分组上，比只靠倍率的分组更便宜时要胜出。
  it('prefers a promoted group over a plain cheaper one', () => {
    const result = bestDiscount({
      enable_groups: ['8.8折', '6.5折'],
      group_ratio: { '8.8折': 0.88, '6.5折': 0.65 },
      promotions: [promo({ ratio: 0.5, groups: ['8.8折'] })],
    })
    expect(result?.group).toBe('8.8折')
    expect(result?.ratio).toBeCloseTo(0.44)
  })

  it.each([
    [
      '所有分组都不打折',
      { enable_groups: ['default'], group_ratio: { default: 1 } },
    ],
    [
      '倍率大于 1 不算折扣',
      { enable_groups: ['vip'], group_ratio: { vip: 1.2 } },
    ],
    ['没有分组', { enable_groups: [], group_ratio: {} }],
    ['倍率缺失', { enable_groups: ['x'], group_ratio: {} }],
    ['倍率为 0 视为无效', { enable_groups: ['x'], group_ratio: { x: 0 } }],
  ])('%s 时返回 null', (_name, model) => {
    expect(bestDiscount(model)).toBeNull()
  })
})

describe('formatDiscountLabel', () => {
  it.each([
    [0.83, '8.3折'],
    [0.5, '5折'],
    [0.25, '2.5折'],
    [0.44, '4.4折'],
    [0.41, '4.1折'],
    [0.125, '1.3折'],
  ])('%s -> %s', (ratio, expected) => {
    expect(formatDiscountLabel(ratio)).toBe(expected)
  })
})
