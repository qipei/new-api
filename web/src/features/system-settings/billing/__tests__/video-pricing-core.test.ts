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
      'seedance-2-0': {
        unit: 'per_million_tokens',
        tiers: [
          { price: 6.3 },
          { resolution: '1080p', price: 7 },
          { mode: 'v2v', resolution: '4k', price: 2.2 },
          { mode: 't2v', audio: 'on', price: 8 },
        ],
      },
      'qwen-image-3.0': {
        unit: 'per_image',
        input_image_price: 0.02,
        input_token_price: 6.82,
        tiers: [
          { price: 0.18 },
          { mode: 'i2i', resolution: '2k', price: 0.5 },
        ],
      },
    })

    const tables = parseVideoPricingTables(raw)
    assert.equal(tables.length, 2)
    const image = tables.find((t) => t.model === 'qwen-image-3.0')
    const video = tables.find((t) => t.model === 'seedance-2-0')
    assert.ok(image && video)
    assert.equal(video.unit, 'per_million_tokens')
    assert.equal(video.tiers.length, 4)
    assert.equal(image.unit, 'per_image')
    assert.equal(image.inputImagePrice, '0.02')
    assert.equal(image.inputTokenPrice, '6.82')

    const serialized = JSON.parse(serializeVideoPricingTables(tables))
    assert.deepEqual(serialized, JSON.parse(raw))
  })

  test('serialization omits empty dimensions and normalizes resolution case', () => {
    const serialized = JSON.parse(
      serializeVideoPricingTables([
        {
          model: 'm',
          unit: 'per_second',
          inputImagePrice: '',
          inputTokenPrice: '',
          tiers: [
            {
              uid: 't1',
              mode: '',
              resolution: ' 1080P ',
              audio: '',
              price: '0.5',
            },
          ],
        },
      ])
    )
    assert.deepEqual(serialized, {
      m: { unit: 'per_second', tiers: [{ resolution: '1080p', price: 0.5 }] },
    })
  })

  test('parse tolerates malformed input', () => {
    assert.deepEqual(parseVideoPricingTables('not json'), [])
    assert.deepEqual(parseVideoPricingTables('[]'), [])
    assert.deepEqual(parseVideoPricingTables(''), [])
  })

  test('validation flags bad unit, bad prices, duplicates and missing default tier', () => {
    const issues = validateVideoPricingTables([
      {
        model: 'bad-unit',
        unit: 'per_hour',
        inputImagePrice: '',
        inputTokenPrice: '',
        tiers: [{ uid: 't1', mode: '', resolution: '', audio: '', price: '1' }],
      },
      {
        model: 'bad-tier',
        unit: 'per_second',
        inputImagePrice: '',
        inputTokenPrice: '',
        tiers: [
          { uid: 't2', mode: '', resolution: '1080p', audio: '', price: '-1' },
          { uid: 't3', mode: 'v2v', resolution: '4k', audio: '', price: '5' },
          { uid: 't4', mode: 'v2v', resolution: '4K', audio: '', price: '6' },
        ],
      },
    ])
    assert.deepEqual(
      issues.map((issue) => `${issue.model}:${issue.reason}`),
      [
        'bad-unit:unit',
        'bad-tier:tier_price',
        'bad-tier:duplicate_tier',
        'bad-tier:missing_default',
      ]
    )
  })

  test('valid tables produce no issues', () => {
    const issues = validateVideoPricingTables([
      {
        model: 'ok',
        unit: 'per_million_tokens',
        inputImagePrice: '',
        inputTokenPrice: '',
        tiers: [
          { uid: 't5', mode: '', resolution: '', audio: '', price: '6.3' },
          { uid: 't6', mode: '', resolution: '1080p', audio: 'on', price: '7' },
        ],
      },
    ])
    assert.deepEqual(issues, [])
  })
})
