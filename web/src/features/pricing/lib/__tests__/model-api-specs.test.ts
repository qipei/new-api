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

  // MiniMax 语音对外名带不带 MiniMax/ 前缀都要命中，且不能和 MiniMax 视频混在一起。
  test('matches the MiniMax speech family with or without the vendor prefix', () => {
    for (const model of [
      'MiniMax/speech-2.8-hd',
      'MiniMax/speech-02-hd',
      'MiniMax/speech-2.8-turbo',
      'speech-02-turbo',
    ]) {
      assert.equal(findModelApiSpec(model)?.family, 'minimax-speech', model)
    }
    assert.equal(findModelApiSpec('MiniMax-H3')?.family, 'minimax-video')
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

  // 首帧/尾帧/参考图/参考视频/参考音频字段按各族实际支持的用途列出，不多不少。
  test('documents the media role fields each video family supports', () => {
    const roleFields = (family: string) =>
      MODEL_API_SPECS.find((spec) => spec.family === family)
        ?.params.map((param) => param.name)
        .filter((name) =>
          [
            'first_frame_url',
            'last_frame_url',
            'reference_images',
            'video_urls',
            'audio_urls',
          ].includes(name)
        )
    const all = [
      'first_frame_url',
      'last_frame_url',
      'reference_images',
      'video_urls',
      'audio_urls',
    ]
    assert.deepEqual(roleFields('minimax-video'), all)
    assert.deepEqual(roleFields('seedance-video'), all)
    assert.deepEqual(roleFields('wan3-video'), all)
    assert.deepEqual(roleFields('kling-video'), [
      'first_frame_url',
      'last_frame_url',
      'reference_images',
      'video_urls',
    ])
    assert.deepEqual(roleFields('vidu-reference'), ['reference_images'])
    assert.deepEqual(roleFields('vidu-img2video'), ['first_frame_url'])
    assert.deepEqual(roleFields('happyhorse-video'), [
      'first_frame_url',
      'reference_images',
      'video_urls',
    ])
  })

  // OpenAI 视频接口只定义了 input_reference（首帧），支持首帧的族都要列出这种写法。
  test('lists input_reference wherever a first frame is supported', () => {
    for (const spec of MODEL_API_SPECS) {
      const names = spec.params.map((param) => param.name)
      assert.equal(
        names.includes('input_reference'),
        names.includes('first_frame_url'),
        spec.family
      )
    }
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
          findModelApiSpec(model, Boolean(spec.workflowOnly))?.family,
          spec.family,
          `${spec.family} sample uses model ${model}`
        )
      }
    }
  })

  test('routes Doubao speech models to their own families', () => {
    assert.equal(findModelApiSpec('seed-audio-1.0')?.family, 'seed-audio')
    assert.equal(findModelApiSpec('seed-tts-2.0')?.family, 'seed-tts-task')
    assert.equal(findModelApiSpec('seed-icl-2.0')?.family, 'seed-tts-task')
    assert.equal(
      findModelApiSpec('doubao-seedance-2-0-260128')?.family,
      'seedance-video'
    )
  })

  // 用户要求接入时不漏参数：上游文档列出的字段都必须出现在参数表里。
  test('documents every upstream field of the Doubao speech APIs', () => {
    const documented = (family: string) =>
      new Set(
        MODEL_API_SPECS.find((spec) => spec.family === family)?.params.map(
          (param) => param.name
        )
      )
    const seedAudio = documented('seed-audio')
    for (const name of [
      'model',
      'text_prompt',
      'references',
      'references[].speaker',
      'references[].audio_data',
      'references[].audio_url',
      'references[].image_data',
      'references[].image_url',
      'audio_config.format',
      'audio_config.sample_rate',
      'audio_config.speech_rate',
      'audio_config.loudness_rate',
      'audio_config.pitch_rate',
      'audio_config.enable_subtitle',
      'watermark.aigc_watermark',
      'watermark.aigc_metadata.enable',
      'content_producer',
      'produce_id',
      'content_propagator',
      'propagate_id',
    ]) {
      assert.ok(seedAudio.has(name), `seed-audio missing ${name}`)
    }
    const seedTTS = documented('seed-tts-task')
    for (const name of [
      'model',
      'user.uid',
      'req_params.text',
      'req_params.speaker',
      'req_params.model',
      'req_params.ssml',
      'req_params.audio_params.format',
      'req_params.audio_params.sample_rate',
      'req_params.audio_params.bit_rate',
      'req_params.audio_params.speech_rate',
      'req_params.audio_params.loudness_rate',
      'req_params.audio_params.enable_timestamp',
      'req_params.additions',
      'req_params.additions.silence_duration',
      'req_params.additions.disable_markdown_filter',
      'req_params.additions.disable_emoji_filter',
      'req_params.additions.latex_parser',
      'req_params.additions.explicit_language',
      'req_params.additions.max_length_to_filter_parenthesis',
      'req_params.additions.aigc_watermark',
      'req_params.additions.pronunciation_dict.tone',
      'req_params.post_process.pitch',
    ]) {
      assert.ok(seedTTS.has(name), `seed-tts missing ${name}`)
    }
  })

  test('routes Bailian speech models to their own families', () => {
    assert.equal(
      findModelApiSpec('qwen-audio-3.1-tts-plus')?.family,
      'qwen-audio-tts'
    )
    assert.equal(
      findModelApiSpec('qwen-audio-3.1-tts-flash')?.family,
      'qwen-audio-tts'
    )
    assert.equal(
      findModelApiSpec('qwen-audio-3.0-tts-plus')?.family,
      'qwen-audio-tts'
    )
    assert.equal(
      findModelApiSpec('qwen-audio-3.1-tts-next')?.family,
      'qwen-audio-next'
    )
    assert.equal(findModelApiSpec('qwen3-tts-flash'), undefined)
  })

  // 上游文档与实测确认的字段都必须出现在参数表里。
  test('documents every upstream field of the Bailian speech APIs', () => {
    const documented = (family: string) =>
      new Set(
        MODEL_API_SPECS.find((spec) => spec.family === family)?.params.map(
          (param) => param.name
        )
      )
    const tts = documented('qwen-audio-tts')
    for (const name of [
      'model',
      'input.text',
      'input.voice',
      'input.instruction',
      'input.format',
      'input.sample_rate',
      'input.volume',
      'input.rate',
      'input.pitch',
      'input.bit_rate',
      'input.seed',
      'input.language_hints',
      'input.enable_ssml',
      'input.enable_aigc_tag',
      'input.aigc_propagator',
      'input.aigc_propagate_id',
      'input.hot_fix.pronunciation',
      'input.hot_fix.replace',
    ]) {
      assert.ok(tts.has(name), `qwen-audio-tts missing ${name}`)
    }
    const next = documented('qwen-audio-next')
    for (const name of [
      'model',
      'input.text_prompt',
      'input.references',
      'input.references[].audio_url',
      'input.references[].audio_data',
      'input.format',
      'input.sample_rate',
      'input.channels',
      'input.volume',
      'input.rate',
      'input.enable_cbr',
      'input.bit_rate',
      'input.quality',
      'input.seed',
      'input.enable_aigc_tag',
    ]) {
      assert.ok(next.has(name), `qwen-audio-next missing ${name}`)
    }
  })
})

describe('IndexTTS2 spec', () => {
  // 同名模型按渠道选说明：只由 autodl 工作流提供的走工作流参数，腾讯 TokenHub 的走同步合成。
  test('picks the workflow or TokenHub spec by the serving channels', () => {
    for (const model of ['indextts2', 'IndexTTS-2', 'indextts-2-emotion']) {
      assert.equal(
        findModelApiSpec(model, true)?.family,
        'autodl-indextts2',
        model
      )
      assert.equal(findModelApiSpec(model)?.family, 'tokenhub-indextts', model)
    }
  })

  // 工作流拒收未定义的字段：参数表必须正好是上游实测接受的字段，必填项与上游一致。
  test('documents exactly the fields the workflow accepts', () => {
    const spec = findModelApiSpec('indextts2', true)
    assert.ok(spec)
    const names = spec.params.map((param) => param.name).sort()
    assert.deepEqual(names, [
      'emo_afraid',
      'emo_angry',
      'emo_calm',
      'emo_control_method',
      'emo_disgusted',
      'emo_happy',
      'emo_melancholic',
      'emo_random',
      'emo_ref_audio',
      'emo_sad',
      'emo_surprised',
      'model',
      'prompt_simple',
      'prompt_text',
    ])
    assert.deepEqual(
      spec.params.filter((param) => param.required).map((param) => param.name),
      ['model', 'prompt_text', 'prompt_simple', 'emo_control_method']
    )
  })
})

describe('autodl H3 spec', () => {
  // autodl 的 H3 可以沿用 MiniMax-H3 开头的名字：由渠道决定用工作流说明还是官方说明。
  test('keeps MiniMax-H3 names on the official spec unless the model is workflow-only', () => {
    for (const model of ['MiniMax-H3', 'MiniMax-H3-Turbo', 'h3-turbo']) {
      assert.equal(
        findModelApiSpec(model, true)?.family,
        'autodl-h3-video',
        model
      )
    }
    assert.equal(findModelApiSpec('MiniMax-H3')?.family, 'minimax-video')
    assert.equal(findModelApiSpec('MiniMax-H3-Turbo')?.family, 'minimax-video')
    assert.equal(findModelApiSpec('h3-turbo'), undefined)
  })

  // 工作流不支持参考视频，也没有官方的 2K、prompt 扩写和水印参数。
  test('documents only the inputs the adaptor forwards', () => {
    const spec = findModelApiSpec('MiniMax-H3-Turbo', true)
    assert.ok(spec)
    const names = spec.params.map((param) => param.name)
    for (const name of [
      'video_urls',
      'content',
      'metadata.prompt_expansion_mode',
    ]) {
      assert.ok(!names.includes(name), name)
    }
    for (const name of [
      'first_frame_url',
      'last_frame_url',
      'reference_images',
      'audio_urls',
      'seed',
      'ratio',
    ]) {
      assert.ok(names.includes(name), name)
    }
  })
})
