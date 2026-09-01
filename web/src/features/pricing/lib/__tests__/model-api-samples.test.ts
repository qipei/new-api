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

import { buildScenarioSample } from '../model-api-samples'
import type { ModelApiParam } from '../model-api-specs'

const params: ModelApiParam[] = [
  {
    name: 'duration',
    location: 'body',
    type: 'integer',
    descriptionKey: '生成视频的时长（秒）。按秒计费，时长越长费用越高。',
  },
  {
    name: 'metadata.parameters.ratio',
    location: 'metadata.parameters',
    type: 'enum',
    enumValues: ['16:9', '9:16'],
    descriptionKey: '画面宽高比。文生视频必填。',
  },
]

const ctx = {
  baseUrl: 'https://api.example.com',
  endpointPath: '/v1/video/generations',
  body: {
    model: 'MiniMax-H3',
    duration: 5,
    metadata: { parameters: { ratio: '16:9' } },
  },
  params,
  translate: (key: string) => key,
  scenarioTitle: '文生视频',
}

describe('scenario samples', () => {
  // 注释是这个功能的主要价值：字段旁边要说明这个值是什么、能填什么。
  test('annotates nested fields by their full path', () => {
    const python = buildScenarioSample('python', ctx)
    assert.ok(python.includes('"duration": 5,  # 生成视频的时长（秒）。'))
    // 嵌套字段要按完整路径匹配到注释，而不是只看末级键名。
    assert.ok(python.includes('"ratio": "16:9"  # 画面宽高比。'))
  })

  test('appends the enum values so the reader sees what is accepted', () => {
    assert.ok(buildScenarioSample('python', ctx).includes('[16:9 | 9:16]'))
  })

  // 注释只留第一句，完整说明在参数表里，避免代码块被长句撑开。
  test('keeps only the first sentence in the inline comment', () => {
    const python = buildScenarioSample('python', ctx)
    assert.ok(!python.includes('按秒计费'))
  })

  test('uses the comment token of each language', () => {
    assert.ok(
      buildScenarioSample('javascript', ctx).includes('// 画面宽高比。')
    )
  })

  // curl 的 JSON 不能带注释，必须保持可直接执行。
  test('keeps the curl body as valid JSON', () => {
    const curl = buildScenarioSample('curl', ctx)
    assert.ok(!curl.includes('#  '))
    const match = curl.match(/-d '([\s\S]+)'$/)
    assert.ok(match, 'curl sample must carry a JSON body')
    assert.deepEqual(JSON.parse(match![1].replace(/\n  /g, '\n')), ctx.body)
  })
})
