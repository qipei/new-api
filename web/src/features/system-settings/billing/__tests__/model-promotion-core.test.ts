import { describe, expect, it } from 'vitest'

import {
  effectiveDiscounts,
  parsePromotions,
  promotionPhase,
  serializePromotions,
  todayInZone,
  validatePromotion,
  type ModelPromotion,
} from '../model-promotion-core'

const base: ModelPromotion = {
  name: '开学季',
  start: '2026-08-30',
  end: '2026-09-10',
  ratio: 0.5,
}

describe('promotion parsing', () => {
  it('drops entries that are not arrays of objects', () => {
    const raw = JSON.stringify({
      good: [base],
      notAnArray: { name: 'x' },
      emptyArray: [],
      arrayOfJunk: ['x', 1, null],
    })
    expect(Object.keys(parsePromotions(raw))).toEqual(['good'])
  })

  it('survives malformed json', () => {
    expect(parsePromotions('{not json')).toEqual({})
    expect(parsePromotions('')).toEqual({})
  })

  it('omits empty group lists so the backend sees "all groups"', () => {
    const serialized = serializePromotions({
      m: [{ ...base, groups: [] }],
      empty: [],
    })
    const parsed = JSON.parse(serialized)
    expect(Object.keys(parsed)).toEqual(['m'])
    expect(parsed.m[0].groups).toBeUndefined()
  })
})

describe('promotion validation mirrors the backend rules', () => {
  it.each([
    ['valid', base, null],
    ['blank name', { ...base, name: '  ' }, 'Promotion name is required.'],
    [
      'bad start',
      { ...base, start: '2026/08/30' },
      'Start date must be YYYY-MM-DD.',
    ],
    [
      'end before start',
      { ...base, end: '2026-08-29' },
      'The end date must not be earlier than the start date.',
    ],
    [
      'ratio above 1',
      { ...base, ratio: 1.5 },
      'The promotion multiplier must be greater than 0 and at most 1.',
    ],
    [
      'ratio zero',
      { ...base, ratio: 0 },
      'The promotion multiplier must be greater than 0 and at most 1.',
    ],
    [
      'ratio negative',
      { ...base, ratio: -0.5 },
      'The promotion multiplier must be greater than 0 and at most 1.',
    ],
  ])('%s', (_name, promotion, expected) => {
    expect(validatePromotion(promotion as ModelPromotion)).toBe(expected)
  })
})

describe('promotion phase', () => {
  it.each([
    ['2026-08-29', 'upcoming'],
    ['2026-08-30', 'live'],
    ['2026-09-10', 'live'],
    ['2026-09-11', 'ended'],
  ])('%s is %s', (today, expected) => {
    expect(promotionPhase(base, today)).toBe(expected)
  })
})

describe('todayInZone', () => {
  it('formats as YYYY-MM-DD in the given zone', () => {
    // 北京时间比 UTC 早 8 小时，UTC 的 20:00 在上海已经是第二天。
    const at = new Date('2026-09-10T20:00:00Z')
    expect(todayInZone('Asia/Shanghai', at)).toBe('2026-09-11')
    expect(todayInZone('UTC', at)).toBe('2026-09-10')
  })

  it('falls back instead of throwing on a bad zone', () => {
    expect(
      todayInZone('Mars/Olympus', new Date('2026-09-10T20:00:00Z'))
    ).toMatch(/^\d{4}-\d{2}-\d{2}$/)
  })
})

// 这是运营配置时唯一的护栏：叠出来的最低折扣必须能看见。
describe('effectiveDiscounts', () => {
  const groupRatios = { '2.5折': 0.25, '8.8折': 0.88, default: 1 }

  it('multiplies the group ratio and sorts cheapest first', () => {
    const rows = effectiveDiscounts({ ...base, groups: [] }, groupRatios, [
      'default',
      '2.5折',
      '8.8折',
    ])
    expect(rows.map((r) => r.group)).toEqual(['2.5折', '8.8折', 'default'])
    expect(rows[0].effective).toBeCloseTo(0.125)
    expect(rows.at(-1)?.effective).toBeCloseTo(0.5)
  })

  it('honours a group-scoped promotion', () => {
    const rows = effectiveDiscounts(
      { ...base, groups: ['8.8折'] },
      groupRatios,
      ['default', '2.5折', '8.8折']
    )
    expect(rows.map((r) => r.group)).toEqual(['8.8折'])
    expect(rows[0].effective).toBeCloseTo(0.44)
  })

  it('skips groups with no configured ratio', () => {
    expect(
      effectiveDiscounts({ ...base, groups: ['不存在的分组'] }, groupRatios, [])
    ).toEqual([])
  })
})
