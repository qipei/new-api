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

import {
  parseVideoPricingTables,
  serializeVideoPricingTables,
  validateVideoPricingTables,
} from '../video-pricing-core'

// 序列化结果是与后端 setting/video_billing 的存储契约，必须保持结构稳定。
describe('video pricing core', () => {
  test('round-trips backend payload structure', () => {
    const raw = JSON.stringify({
      'doubao-seedance-2-0-260128': {
        base_price: 46,
        tiers: [
          { resolution: '1080p', price: 51 },
          { mode: 'v2v', resolution: '4k', price: 16 },
          { mode: 't2v', audio: 'on', price: 60 },
        ],
      },
    })

    const tables = parseVideoPricingTables(raw)
    assert.equal(tables.length, 1)
    assert.equal(tables[0].model, 'doubao-seedance-2-0-260128')
    assert.equal(tables[0].basePrice, '46')
    assert.equal(tables[0].tiers.length, 3)

    const serialized = JSON.parse(serializeVideoPricingTables(tables))
    assert.deepEqual(serialized, JSON.parse(raw))
  })

  test('serialization omits empty dimensions and normalizes resolution case', () => {
    const serialized = JSON.parse(
      serializeVideoPricingTables([
        {
          model: 'm',
          basePrice: '10',
          tiers: [{ uid: 't1', mode: '', resolution: ' 1080P ', audio: '', price: '20' }],
        },
      ])
    )
    assert.deepEqual(serialized, {
      m: { base_price: 10, tiers: [{ resolution: '1080p', price: 20 }] },
    })
  })

  test('parse tolerates malformed input', () => {
    assert.deepEqual(parseVideoPricingTables('not json'), [])
    assert.deepEqual(parseVideoPricingTables('[]'), [])
    assert.deepEqual(parseVideoPricingTables(''), [])
  })

  test('validation flags non-positive prices and duplicate tiers', () => {
    const issues = validateVideoPricingTables([
      {
        model: 'bad-base',
        basePrice: '0',
        tiers: [],
      },
      {
        model: 'bad-tier',
        basePrice: '10',
        tiers: [
          { uid: 't2', mode: '', resolution: '1080p', audio: '', price: '-1' },
          { uid: 't3', mode: 'v2v', resolution: '4k', audio: '', price: '5' },
          { uid: 't4', mode: 'v2v', resolution: '4K', audio: '', price: '6' },
        ],
      },
    ])
    assert.deepEqual(
      issues.map((issue) => `${issue.model}:${issue.reason}`),
      ['bad-base:base_price', 'bad-tier:tier_price', 'bad-tier:duplicate_tier']
    )
  })

  test('valid tables produce no issues', () => {
    const issues = validateVideoPricingTables([
      {
        model: 'ok',
        basePrice: '46',
        tiers: [
          { uid: 't5', mode: '', resolution: '1080p', audio: '', price: '51' },
          { uid: 't6', mode: '', resolution: '1080p', audio: 'on', price: '55' },
        ],
      },
    ])
    assert.deepEqual(issues, [])
  })
})
