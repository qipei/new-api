import { describe, expect, it } from 'vitest'

import type { ModelPromotionInfo } from '../../types'
import {
  activePromotionForGroup,
  effectiveGroupRatio,
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
