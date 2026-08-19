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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { PricingModel } from '@/features/pricing/types'
import type { ModelRanking } from '@/features/rankings/types'

import {
  formatFirstTierHomePrice,
  HOME_MODEL_LIMIT,
  selectHomeModels,
} from '../../lib/home-models'

function ranking(rank: number): ModelRanking {
  return {
    rank,
    model_name: `model-${rank}`,
    vendor: 'vendor',
    category: 'all',
    total_tokens: rank * 100,
    share: 0,
    growth_pct: 0,
  }
}

describe('selectHomeModels', () => {
  test('shows the first six usage-ranked models in rank order', () => {
    const rows = [
      ranking(4),
      ranking(2),
      ranking(7),
      ranking(1),
      ranking(6),
      ranking(3),
      ranking(5),
    ]

    const selected = selectHomeModels(rows)

    assert.equal(selected.length, HOME_MODEL_LIMIT)
    assert.deepEqual(
      selected.map((row) => row.rank),
      [1, 2, 3, 4, 5, 6]
    )
  })

  test('keeps an empty state empty when rankings have no usage data', () => {
    assert.deepEqual(selectHomeModels([]), [])
  })
})

describe('formatFirstTierHomePrice', () => {
  test('shows only the first dynamic tier with the best available group ratio', () => {
    const model: PricingModel = {
      id: 1,
      model_name: 'deepseek-v4-flash-0731',
      quota_type: 0,
      model_ratio: 0.5,
      completion_ratio: 2,
      enable_groups: ['5.4折', 'default'],
      group_ratio: { '5.4折': 0.54, default: 1 },
      billing_mode: 'tiered_expr',
      billing_expr:
        '(hour("Asia/Shanghai") >= 22 || hour("Asia/Shanghai") < 8)\n' +
        '  ? tier("闲时", p * 1.5 + c * 4.5 + cr * 0.15)\n' +
        '  : tier("忙时", p * 3 + c * 9 + cr * 0.3)',
    }

    const price = formatFirstTierHomePrice(model, 1, 1)

    assert.equal(price, '$0.81 / $2.43')
  })
})
