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

import type { PricingModel, VideoPriceTable } from '../../types'
import { getMatrixUnitPriceSummary } from '../price'

// 视频模型在「模型定价」里另有一个按次价，但实际按价格表计费；卡片和列表必须展示
// 价格表里最便宜分组能用的最便宜档位，单位跟价格表走，不能显示成「/ 请求」。
function videoModel(table: VideoPriceTable, groups = ['1.6折', '2.4折']) {
  return {
    model_name: 'MiniMax-H3',
    quota_type: 1,
    model_ratio: 0,
    model_price: 0.8,
    completion_ratio: 0,
    enable_groups: groups,
    group_ratio: { '1.6折': 0.16, '2.4折': 0.24 },
    matrix_price_table: table,
  } as unknown as PricingModel
}

const MINIMAX_TABLE: VideoPriceTable = {
  unit: 'per_second',
  tiers: [
    { resolution: '768p', price: 0.5 },
    { resolution: '2k', price: 0.8, excluded_groups: ['1.6折'] },
    { price: 0.8 },
  ],
}

describe('getMatrixUnitPriceSummary', () => {
  it('shows the cheapest tier of the cheapest group with the table unit', () => {
    expect(getMatrixUnitPriceSummary(videoModel(MINIMAX_TABLE))).toEqual({
      price: '$0.08',
      unit: 'per_second',
    })
  })

  it('uses the selected group and skips tiers excluded for it', () => {
    const summary = getMatrixUnitPriceSummary(
      videoModel({
        unit: 'per_second',
        tiers: [
          { resolution: '768p', price: 0.5, excluded_groups: ['2.4折'] },
          { resolution: '2k', price: 0.8 },
          { price: 0.9 },
        ],
      }),
      false,
      1,
      1,
      '2.4折'
    )
    expect(summary?.price).toBe('$0.192')
  })

  // 文生图按默认档计费，默认档比图生图便宜时展示默认档。
  it('counts the default tier, which unmatched requests are billed at', () => {
    expect(
      getMatrixUnitPriceSummary(
        videoModel({
          unit: 'per_image',
          tiers: [
            { price: 0.18 },
            { mode: 'i2i', resolution: '2k', price: 0.5 },
          ],
        })
      )
    ).toEqual({ price: '$0.0288', unit: 'per_image' })
  })

  it('falls back to the default tier when no tier serves the group', () => {
    const summary = getMatrixUnitPriceSummary(
      videoModel(
        {
          unit: 'per_second',
          tiers: [
            { resolution: '2k', price: 0.8, excluded_groups: ['1.6折'] },
            { price: 0.6 },
          ],
        },
        ['1.6折']
      )
    )
    expect(summary?.price).toBe('$0.096')
  })

  it('covers per-image and default-only tables, not token tables', () => {
    expect(
      getMatrixUnitPriceSummary(
        videoModel({
          unit: 'per_image',
          tiers: [{ price: 0.2 }, { resolution: '2k', price: 0.3 }],
        })
      )
    ).toEqual({ price: '$0.032', unit: 'per_image' })
    expect(
      getMatrixUnitPriceSummary(
        videoModel({ unit: 'per_10k_chars', tiers: [{ price: 3 }] })
      )
    ).toEqual({ price: '$0.48', unit: 'per_10k_chars' })
    expect(
      getMatrixUnitPriceSummary(
        videoModel({ unit: 'per_million_tokens', tiers: [{ price: 70 }] })
      )
    ).toBeNull()
  })
})
