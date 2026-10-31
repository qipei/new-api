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
import { buildGroupPriceBlocks } from '../group-price-blocks'

const t = ((key: string, values?: Record<string, unknown>) =>
  key.replaceAll(/\{\{(\w+)\}\}/g, (_, name) =>
    String(values?.[name] ?? '')
  )) as unknown as TFunction

const TABLE: VideoPriceTable = {
  unit: 'per_second',
  tiers: [
    {
      resolution: '768p',
      price: 0.6,
      off_peak_price: 0.5,
      group_prices: { wenex: 0.04 },
    },
    { resolution: '1088p', price: 0.7, group_prices: { wenex: 0.06 } },
    { price: 0.8 },
  ],
}

const night = { start: '00:00', end: '07:50', tz: 'Asia/Shanghai' }

function priceTable(group: string, ratio: number) {
  const model = {
    model_name: 'MiniMax-H3-Fast',
    enable_groups: ['wenex', '1折'],
    group_off_peak: { wenex: [night], '1折': [night] },
  } as unknown as PricingModel
  const block = buildGroupPriceBlocks({
    model,
    group,
    ratio,
    matrixTable: TABLE,
    tokenUnit: 'M',
    showRechargePrice: false,
    priceRate: 1,
    usdExchangeRate: 1,
    t,
  }).find((item) => item.kind === 'table')
  if (block?.kind !== 'table') throw new Error('missing price table')
  return block
}

// 分组卡片与后端取价一致：配了分组价的分组按分组价乘分组倍率展示，且不显示闲时价；
// 其他分组仍按原价和闲时价展示。
describe('group tier prices in group cards', () => {
  it('shows the group price times the group ratio without off-peak columns', () => {
    const table = priceTable('wenex', 1)
    expect(table.columns).toEqual(['Price'])
    expect(table.rows).toEqual([
      { label: '768p', cells: ['$0.04'] },
      { label: '1088p', cells: ['$0.06'] },
    ])
  })

  it('keeps the tier and off-peak prices for other groups', () => {
    const table = priceTable('1折', 0.1)
    expect(table.columns).toEqual([
      'Peak price (07:50~24:00)',
      'Off-peak price (00:00~07:50)',
    ])
    expect(table.rows).toEqual([
      { label: '768p', cells: ['$0.06', '$0.05'] },
      { label: '1088p', cells: ['$0.07', '$0.07'] },
    ])
  })
})

// 分组输入价：配了的分组卡片按它展示（0 表示不收输入图费用，不显示这一行），其他分组
// 仍按价格表的输入价展示。
describe('group input prices in group cards', () => {
  const table: VideoPriceTable = {
    unit: 'per_second',
    tiers: [{ resolution: '768p', price: 0.5 }],
    input_image_price: 0.02,
    free_input_images: 5,
    input_video_price: 0.8,
    group_input_prices: {
      wenex: { input_image_price: 0, input_video_price: 0.1 },
    },
  }
  const inputRows = (group: string) => {
    const model = {
      model_name: 'MiniMax-H3',
      enable_groups: ['wenex', 'default'],
    } as unknown as PricingModel
    const block = buildGroupPriceBlocks({
      model,
      group,
      ratio: 1,
      matrixTable: table,
      tokenUnit: 'M',
      showRechargePrice: false,
      priceRate: 1,
      usdExchangeRate: 1,
      t,
    }).find((item) => item.kind === 'list' && item.title === 'Input price')
    if (block?.kind !== 'list') throw new Error('missing input price block')
    return block.rows.map((row) => [row.label, row.value])
  }

  it('uses the group input prices', () => {
    expect(inputRows('wenex')).toEqual([['Input video price', '$0.1']])
  })

  it('keeps the table input prices for other groups', () => {
    expect(inputRows('default')).toEqual([
      ['Input image price', '$0.02'],
      ['Input video price', '$0.8'],
    ])
  })
})
