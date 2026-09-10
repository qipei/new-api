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

import { describe, test } from 'vitest'

import type { PricingModel } from '../../types'
import {
  MODEL_API_SPECS,
  findModelApiSpec,
  resolveModelApiParams,
  resolutionsFromMatrix,
} from '../model-api-specs'

function pricingModel(overrides: Partial<PricingModel>): PricingModel {
  return {
    id: 1,
    model_name: 'MiniMax-H3',
    quota_type: 0,
    model_ratio: 0,
    completion_ratio: 0,
    enable_groups: ['default'],
    ...overrides,
  } as PricingModel
}

describe('model API specs', () => {
  // 未登记的模型必须回落到通用逻辑，而不是显示一张空表。
  test('returns undefined for models without a dedicated spec', () => {
    assert.equal(
      resolveModelApiParams(pricingModel({ model_name: 'gpt-4o' })),
      undefined
    )
    assert.equal(findModelApiSpec('deepseek-v3'), undefined)
  })

  // 同前缀下混着不同端点的模型：Kling-3.0-image 走 /v1/images/generations，
  // 套用视频参数会给出完全错误的文档。
  test('does not pull sibling models with a different endpoint into a family', () => {
    assert.equal(findModelApiSpec('kling-3.0-video')?.family, 'kling-video')
    assert.equal(findModelApiSpec('kling-3.0-Omni')?.family, 'kling-video')
    // 图片模型必须归到图片族，绝不能落进视频族。
    assert.equal(
      findModelApiSpec('Kling-3.0-image')?.family,
      'image-generation'
    )
  })

  // Vidu 的两个族请求协议不同，参考生视频收多张图，图生视频只收一张。
  test('separates the two Vidu families', () => {
    assert.equal(findModelApiSpec('vidu-q3-ad')?.family, 'vidu-reference')
    assert.equal(findModelApiSpec('vidu-q3-drama')?.family, 'vidu-reference')
    assert.equal(findModelApiSpec('vidu-q3-pro')?.family, 'vidu-img2video')
    assert.equal(findModelApiSpec('vidu-q3-pro-fast')?.family, 'vidu-img2video')
    assert.equal(findModelApiSpec('vidu-q3-turbo')?.family, 'vidu-img2video')
  })

  test('matches the MiniMax video family case-insensitively', () => {
    assert.equal(findModelApiSpec('MiniMax-H3')?.family, 'minimax-video')
    assert.equal(findModelApiSpec('minimax-h3')?.family, 'minimax-video')
    // 同名前缀的对话模型不应命中视频族。
    assert.equal(findModelApiSpec('MiniMax-M2.5'), undefined)
  })

  test('matches both wan3.0 video variants but not other wan models', () => {
    assert.equal(findModelApiSpec('wan3.0-video')?.family, 'wan3-video')
    assert.equal(findModelApiSpec('wan3.0-video-prime')?.family, 'wan3-video')
    assert.equal(findModelApiSpec('WAN3.0-VIDEO')?.family, 'wan3-video')
    // 图片模型必须归到图片族，绝不能落进视频族。
    assert.equal(findModelApiSpec('wan2.7-image')?.family, 'image-generation')
  })

  // 万相3.0 的两套素材互斥，文档必须把这条规则写在 media 参数里，否则用户会
  // 照着首尾帧的例子再塞一段参考音频，任务跑几分钟才失败。
  test('documents the wan3.0 media exclusivity rule', () => {
    const params = resolveModelApiParams(
      pricingModel({ model_name: 'wan3.0-video' })
    )
    const media = params?.find((p) => p.name === 'metadata.input.media')
    assert.ok(media?.descriptionKey.includes('cannot be mixed'))
    assert.equal(media?.range, '≤ 20')
  })

  // -1 是编辑和延长的推荐用法，漏写用户就只会传正整数。
  test('documents the wan3.0 smart duration', () => {
    const params = resolveModelApiParams(
      pricingModel({ model_name: 'wan3.0-video' })
    )
    const duration = params?.find((p) => p.name === 'duration')
    assert.equal(duration?.range, '2 ~ 30, or -1')
  })

  // 分辨率取值由部署自己的价格矩阵决定：矩阵里没有的档位会被后端以
  // model_price_error 拒绝，写死在文档里必然与实际不符。
  test('derives the resolution enum from the deployment price matrix', () => {
    const params = resolveModelApiParams(
      pricingModel({
        matrix_price_table: {
          unit: 'per_second',
          tiers: [
            { price: 0.5, resolution: '768p' },
            { price: 0.8, resolution: '2k' },
            { price: 0.8 },
          ],
        },
      })
    )
    const size = params?.find((p) => p.name === 'size')
    assert.deepEqual(size?.enumValues, ['768p', '2k'])
  })

  test('keeps the static enum when the model has no matrix', () => {
    const params = resolveModelApiParams(pricingModel({}))
    assert.equal(params?.find((p) => p.name === 'size')?.enumValues, undefined)
  })

  test('ignores tiers without a resolution dimension', () => {
    assert.deepEqual(
      resolutionsFromMatrix({ unit: 'per_second', tiers: [{ price: 1 }] }),
      []
    )
  })

  // 参数放错层级会被静默忽略，所以每条都必须声明位置，且名字要带上完整路径。
  test('every parameter declares where it belongs in the request body', () => {
    for (const spec of MODEL_API_SPECS) {
      for (const param of spec.params) {
        assert.ok(
          param.location,
          `${spec.family}.${param.name} missing location`
        )
        if (param.location !== 'body') {
          assert.ok(
            param.name.startsWith(param.location),
            `${spec.family}.${param.name} must be named with its full path`
          )
        }
      }
    }
  })

  // 示例是给用户直接复制的，必须能被对应的族匹配到。
  test('samples target the family they are documented under', () => {
    for (const spec of MODEL_API_SPECS) {
      assert.ok(spec.samples.length > 0, `${spec.family} has no sample`)
      for (const sample of spec.samples) {
        const model = String(sample.body.model ?? '')
        assert.equal(
          findModelApiSpec(model)?.family,
          spec.family,
          `${spec.family} sample uses model ${model}`
        )
      }
    }
  })
})
