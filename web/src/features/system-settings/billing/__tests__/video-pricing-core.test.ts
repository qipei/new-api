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
  formatVideoPricingJson,
  parseVideoPricingJsonDraft,
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
          { mode: 'i2i', max_pixels: 2610000, price: 0.3 },
          { mode: 'i2i', min_pixels: 2610001, price: 0.6 },
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
          resolutionBuckets: [],
          tiers: [
            {
              uid: 't1',
              mode: '',
              resolution: ' 1080P ',
              audio: '',
              minPixels: '',
              maxPixels: '',
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

  test('JSON draft parsing rejects invalid syntax and incompatible shapes', () => {
    assert.equal(parseVideoPricingJsonDraft('{').error, 'json')
    assert.equal(parseVideoPricingJsonDraft('[]').error, 'shape')
    assert.equal(
      parseVideoPricingJsonDraft('{"model":{"tiers":"invalid"}}').error,
      'shape'
    )
    assert.equal(
      parseVideoPricingJsonDraft(
        '{"model":{"unit":"per_image","input_image_price":"0.2","tiers":[]}}'
      ).error,
      'shape'
    )
  })

  test('JSON draft parsing and formatting preserve an editable pricing map', () => {
    const raw = '{"model":{"unit":"per_image","tiers":[{"price":0.2}]}}'
    const result = parseVideoPricingJsonDraft(raw)

    assert.equal(result.error, null)
    assert.equal(result.tables?.[0]?.model, 'model')
    assert.equal(formatVideoPricingJson(raw).includes('\n  "model"'), true)
  })

  test('validation flags bad unit, bad prices, duplicates and missing default tier', () => {
    const issues = validateVideoPricingTables([
      {
        model: 'bad-unit',
        unit: 'per_hour',
        inputImagePrice: '',
        inputTokenPrice: '',
        resolutionBuckets: [],
        tiers: [
          {
            uid: 't1',
            mode: '',
            resolution: '',
            audio: '',
            minPixels: '',
            maxPixels: '',
            price: '1',
          },
        ],
      },
      {
        model: 'bad-tier',
        unit: 'per_second',
        inputImagePrice: '',
        inputTokenPrice: '',
        resolutionBuckets: [],
        tiers: [
          {
            uid: 't2',
            mode: '',
            resolution: '1080p',
            audio: '',
            minPixels: '',
            maxPixels: '',
            price: '-1',
          },
          {
            uid: 't3',
            mode: 'v2v',
            resolution: '4k',
            audio: '',
            minPixels: '',
            maxPixels: '',
            price: '5',
          },
          {
            uid: 't4',
            mode: 'v2v',
            resolution: '4K',
            audio: '',
            minPixels: '',
            maxPixels: '',
            price: '6',
          },
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
        resolutionBuckets: [],
        tiers: [
          {
            uid: 't5',
            mode: '',
            resolution: '',
            audio: '',
            minPixels: '',
            maxPixels: '',
            price: '6.3',
          },
          {
            uid: 't6',
            mode: '',
            resolution: '1080p',
            audio: 'on',
            minPixels: '',
            maxPixels: '',
            price: '7',
          },
        ],
      },
    ])
    assert.deepEqual(issues, [])
  })

  test('validation rejects invalid image pixel ranges', () => {
    const issues = validateVideoPricingTables([
      {
        model: 'bad-pixels',
        unit: 'per_image',
        inputImagePrice: '',
        inputTokenPrice: '',
        resolutionBuckets: [],
        tiers: [
          {
            uid: 'default',
            mode: '',
            resolution: '',
            audio: '',
            minPixels: '',
            maxPixels: '',
            price: '0.6',
          },
          {
            uid: 'range',
            mode: '',
            resolution: '',
            audio: '',
            minPixels: '2610001',
            maxPixels: '2610000',
            price: '0.6',
          },
        ],
      },
    ])

    assert.deepEqual(
      issues.map((issue) => `${issue.model}:${issue.reason}`),
      ['bad-pixels:pixel_range']
    )
  })

  test('round-trips model-specific resolution bucket definitions', () => {
    const raw = JSON.stringify({
      'vidu-image': {
        unit: 'per_image',
        resolution_buckets: [
          { name: '1k', sizes: ['1024x1024', '1920x1088'] },
          { name: '4k', sizes: ['3840*1648'] },
        ],
        tiers: [{ price: 0.5 }, { resolution: '4k', price: 0.9 }],
      },
    })

    const tables = parseVideoPricingTables(raw)
    assert.equal(tables[0]?.resolutionBuckets[0]?.name, '1k')
    assert.equal(tables[0]?.resolutionBuckets[0]?.sizes, '1024x1024, 1920x1088')
    assert.deepEqual(JSON.parse(serializeVideoPricingTables(tables)), {
      'vidu-image': {
        unit: 'per_image',
        resolution_buckets: [
          { name: '1k', sizes: ['1024x1024', '1920x1088'] },
          { name: '4k', sizes: ['3840x1648'] },
        ],
        tiers: [{ price: 0.5 }, { resolution: '4k', price: 0.9 }],
      },
    })
  })

  test('validation rejects malformed and cross-bucket duplicate image sizes', () => {
    const issues = validateVideoPricingTables([
      {
        model: 'bad-buckets',
        unit: 'per_image',
        inputImagePrice: '',
        inputTokenPrice: '',
        resolutionBuckets: [
          { uid: 'b1', name: '1k', sizes: '1024x1024, bad' },
          { uid: 'b2', name: '2k', sizes: '1024*1024' },
        ],
        tiers: [
          {
            uid: 'default',
            mode: '',
            resolution: '',
            audio: '',
            minPixels: '',
            maxPixels: '',
            price: '0.5',
          },
        ],
      },
    ])

    assert.deepEqual(
      issues.map((issue) => issue.reason),
      ['resolution_bucket', 'duplicate_bucket_size']
    )
  })
})
