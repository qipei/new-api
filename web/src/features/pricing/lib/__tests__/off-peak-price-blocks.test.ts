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
import type { TFunction } from 'i18next'
import { describe, expect, it } from 'vitest'

import type { PricingModel, VideoPriceTable } from '../../types'
import {
  buildGroupPriceBlocks,
  formatOffPeakWindows,
  formatPeakWindows,
  modelOffPeakWindows,
} from '../group-price-blocks'

const t = ((key: string, values?: Record<string, unknown>) =>
  key.replace(/\{\{(\w+)\}\}/g, (_, name) =>
    String(values?.[name] ?? '')
  )) as unknown as TFunction

const TABLE: VideoPriceTable = {
  unit: 'per_second',
  tiers: [
    { resolution: '480p', price: 0.5, off_peak_price: 0.4 },
    { resolution: '768p', price: 0.6 },
    { price: 0.8, off_peak_price: 0.8 },
  ],
}

function model(groupOffPeak?: PricingModel['group_off_peak']): PricingModel {
  return {
    model_name: 'MiniMax-H3-Fast',
    enable_groups: ['0.1折', 'default'],
    group_off_peak: groupOffPeak,
  } as unknown as PricingModel
}

function blocksFor(m: PricingModel, group: string) {
  return buildGroupPriceBlocks({
    model: m,
    group,
    ratio: 0.1,
    matrixTable: TABLE,
    tokenUnit: 'M',
    showRechargePrice: false,
    priceRate: 1,
    usdExchangeRate: 1,
    t,
  })
}

// 闲时价挂在渠道上：只有该分组有渠道配了闲时时段才分成高峰、空闲两列，表头写明时段；
// 没填闲时价的档位在闲时仍按原价计费。
describe('off-peak price in group cards', () => {
  const night = { start: '00:00', end: '07:50', tz: 'Asia/Shanghai' }

  it('splits the price into peak and off-peak columns with their hours', () => {
    const table = blocksFor(model({ '0.1折': [night] }), '0.1折').find(
      (item) => item.kind === 'table'
    )
    expect(table?.kind).toBe('table')
    if (table?.kind !== 'table') return
    expect(table.columns).toEqual([
      'Peak price (07:50~24:00)',
      'Off-peak price (00:00~07:50)',
    ])
    expect(table.rows).toEqual([
      { label: '480p', cells: ['$0.05', '$0.04'] },
      { label: '768p', cells: ['$0.06', '$0.06'] },
    ])
  })

  it('keeps a single price column for groups without an off-peak window', () => {
    const table = blocksFor(model({ '0.1折': [night] }), 'default').find(
      (item) => item.kind === 'table'
    )
    if (table?.kind !== 'table') throw new Error('missing price table')
    expect(table.columns).toEqual(['Price'])
  })

  it('derives the peak hours from the off-peak window', () => {
    expect(formatOffPeakWindows([night])).toBe('00:00~07:50')
    expect(formatPeakWindows([night], t)).toBe('07:50~24:00')
    expect(formatPeakWindows([{ start: '22:00', end: '06:00' }], t)).toBe(
      '06:00~22:00'
    )
    expect(
      formatOffPeakWindows([night, { start: '22:00', end: '06:00', tz: 'UTC' }])
    ).toBe('00:00~07:50、22:00~06:00 UTC')
    expect(
      formatPeakWindows([night, { start: '22:00', end: '06:00' }], t)
    ).toBe('Other hours')
    expect(modelOffPeakWindows(model({ a: [night], b: [night] }))).toEqual([
      night,
    ])
  })
})
