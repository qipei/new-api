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

  // 复制出去的 Python 示例必须能直接运行：布尔值和数组不能写成 JS 字面量。
  test('renders Python literals for booleans and arrays', () => {
    const python = buildScenarioSample('python', {
      ...ctx,
      body: {
        model: 'seed-audio-1.0',
        references: [{ speaker: 'v', enabled: false }],
        audio_config: { enable_subtitle: true },
      },
    })
    assert.ok(
      python.includes('"references": [{"speaker": "v", "enabled": False}]')
    )
    assert.ok(python.includes('"enable_subtitle": True'))
    assert.ok(!python.includes('[object Object]'))
  })

  test('saves binary audio responses to a file', () => {
    const audioCtx = {
      ...ctx,
      response: 'audio-binary' as const,
      outputFile: 'out.wav',
    }
    assert.ok(
      buildScenarioSample('curl', audioCtx).endsWith('  --output out.wav')
    )
    assert.ok(
      buildScenarioSample('python', audioCtx).includes('f.write(resp.content)')
    )
    assert.ok(
      buildScenarioSample('javascript', audioCtx).includes(
        'fs.writeFileSync("out.wav", Buffer.from(await resp.arrayBuffer()))'
      )
    )
  })

  test('decodes Base64 audio from JSON responses', () => {
    const python = buildScenarioSample('python', {
      ...ctx,
      response: 'audio-base64',
    })
    assert.ok(python.startsWith('import base64, os, requests'))
    assert.ok(python.includes('base64.b64decode(data["audio"])'))
  })

  // 异步任务提交后只拿到 task_id，示例必须演示轮询到终态。
  test('polls the fetch endpoint for async tasks', () => {
    const taskCtx = {
      ...ctx,
      endpointPath: '/v1/audio/speech/tasks',
      fetchPath: '/v1/audio/speech/tasks',
      response: 'task' as const,
    }
    assert.ok(
      buildScenarioSample('curl', taskCtx).includes(
        'curl "https://api.example.com/v1/audio/speech/tasks/$TASK_ID"'
      )
    )
    const python = buildScenarioSample('python', taskCtx)
    assert.ok(
      python.includes(
        'requests.get(f"https://api.example.com/v1/audio/speech/tasks/{task_id}", headers=headers)'
      )
    )
    assert.ok(
      python.includes('if result["status"] in ("succeeded", "failed"):')
    )
    assert.ok(
      buildScenarioSample('javascript', taskCtx).includes(
        '} while (!["succeeded", "failed"].includes(result.status))'
      )
    )
  })

  // 百炼原生接口返回的是音频下载地址，示例要演示把它下载成文件。
  test('downloads audio from output.audio.url', () => {
    const urlCtx = {
      ...ctx,
      response: 'audio-url' as const,
      outputFile: 'a.mp3',
    }
    assert.ok(
      buildScenarioSample('python', urlCtx).includes(
        'f.write(requests.get(data["output"]["audio"]["url"]).content)'
      )
    )
    const js = buildScenarioSample('javascript', urlCtx)
    assert.ok(js.startsWith('import fs from "node:fs"'))
    assert.ok(js.includes('await fetch(data.output.audio.url)'))
  })

  // MiniMax 原生接口把音频以 hex 编码放在 output.data.audio，示例要演示解码成文件。
  test('decodes hex audio from output.data.audio', () => {
    const hexCtx = {
      ...ctx,
      response: 'audio-hex' as const,
      outputFile: 'a.mp3',
    }
    assert.ok(
      buildScenarioSample('python', hexCtx).includes(
        'f.write(bytes.fromhex(data["output"]["data"]["audio"]))'
      )
    )
    const js = buildScenarioSample('javascript', hexCtx)
    assert.ok(js.startsWith('import fs from "node:fs"'))
    assert.ok(js.includes('Buffer.from(data.output.data.audio, "hex")'))
  })

  test('does not cut the comment at a version number', () => {
    const python = buildScenarioSample('python', {
      ...ctx,
      body: { model: 'qwen-audio-3.1-tts-plus' },
      params: [
        {
          name: 'model',
          location: 'body',
          type: 'string',
          descriptionKey:
            'qwen-audio-3.1-tts-plus is billed per character. More text.',
        },
      ],
    })
    assert.ok(
      python.includes('# qwen-audio-3.1-tts-plus is billed per character.')
    )
    assert.ok(!python.includes('More text'))
  })
})
