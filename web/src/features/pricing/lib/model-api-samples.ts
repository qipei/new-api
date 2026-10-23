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
// CUSTOM: 按场景生成带注释的请求示例（fork 扩展）。
//
// 示例是给人直接复制去改的，所以字段旁边要写清楚这个值是什么、能填什么。
// curl 的 JSON 不支持注释，因此 curl 只给可直接执行的请求体，说明由参数表承担；
// Python / JavaScript 则逐字段带行内注释。
import type { ModelApiParam, SampleResponseKind } from './model-api-specs'

export type SampleLang = 'curl' | 'python' | 'javascript'

type Annotations = Map<string, string>

/** 用参数表里的说明为示例字段生成注释，key 是字段的完整路径。 */
export function buildAnnotations(
  params: ModelApiParam[],
  translate: (key: string) => string
): Annotations {
  const annotations: Annotations = new Map()
  for (const param of params) {
    const text = translate(param.descriptionKey)
    // 注释只留第一句，完整说明在参数表里，避免代码块被长句撑开。
    // 半角句号后必须跟空白才算句末，否则会把 v3.1 这类版本号截断。
    const firstSentence = text.split(/(?<=[。！？])|(?<=[.!?])\s+/)[0] || text
    const hint =
      param.enumValues && param.enumValues.length > 0
        ? `${firstSentence} [${param.enumValues.join(' | ')}]`
        : firstSentence
    annotations.set(param.name, hint)
  }
  return annotations
}

/**
 * Python 字面量：布尔与空值要写成 True/False/None，数组和嵌套对象逐项转换，
 * 否则复制出去的示例直接 NameError。
 */
function pythonLiteral(value: unknown): string {
  if (value === null || value === undefined) return 'None'
  if (typeof value === 'boolean') return value ? 'True' : 'False'
  if (Array.isArray(value)) return `[${value.map(pythonLiteral).join(', ')}]`
  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>).map(
      ([key, item]) => `${JSON.stringify(key)}: ${pythonLiteral(item)}`
    )
    return `{${entries.join(', ')}}`
  }
  return JSON.stringify(value)
}

/**
 * 把示例请求体渲染成带注释的字面量。
 * path 用于拼出与参数表一致的完整字段路径，从而找到对应注释。
 */
function renderAnnotatedObject(
  value: Record<string, unknown>,
  annotations: Annotations,
  lang: 'python' | 'javascript',
  indent: number,
  path: string
): string[] {
  const pad = ' '.repeat(indent)
  const commentToken = lang === 'python' ? '#' : '//'
  const literal = lang === 'python' ? pythonLiteral : JSON.stringify
  const lines: string[] = []
  const entries = Object.entries(value)

  entries.forEach(([key, item], index) => {
    const fullPath = path ? `${path}.${key}` : key
    const comma = index === entries.length - 1 ? '' : ','

    if (item && typeof item === 'object' && !Array.isArray(item)) {
      lines.push(`${pad}${JSON.stringify(key)}: {`)
      lines.push(
        ...renderAnnotatedObject(
          item as Record<string, unknown>,
          annotations,
          lang,
          indent + 2,
          fullPath
        )
      )
      lines.push(`${pad}}${comma}`)
      return
    }

    const comment = annotations.get(fullPath)
    const body = `${pad}${JSON.stringify(key)}: ${literal(item)}${comma}`
    lines.push(comment ? `${body}  ${commentToken} ${comment}` : body)
  })

  return lines
}

export type SampleContext = {
  baseUrl: string
  endpointPath: string
  body: Record<string, unknown>
  params: ModelApiParam[]
  translate: (key: string) => string
  scenarioTitle: string
  /** 请求之后怎么处理响应；缺省打印 JSON。 */
  response?: SampleResponseKind
  /** 异步任务的查询端点前缀。 */
  fetchPath?: string
  /** 音频示例保存的文件名。 */
  outputFile?: string
}

const POLL_HINT =
  'Poll with the task_id from the response until status is succeeded or failed.'

export function buildScenarioSample(
  lang: SampleLang,
  ctx: SampleContext
): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const fetchUrl = `${ctx.baseUrl}${ctx.fetchPath ?? ctx.endpointPath}`
  const response = ctx.response ?? 'json'
  const outputFile = ctx.outputFile ?? 'speech.mp3'
  const annotations = buildAnnotations(ctx.params, ctx.translate)

  if (lang === 'curl') {
    // JSON 不支持注释，保持可直接执行。
    const body = JSON.stringify(ctx.body, null, 2).split('\n').join('\n  ')
    const lines = [
      `# ${ctx.scenarioTitle}`,
      `curl -X POST "${url}" \\`,
      `  -H "Authorization: Bearer $NEW_API_KEY" \\`,
      `  -H "Content-Type: application/json" \\`,
    ]
    if (response === 'audio-binary') {
      lines.push(`  -d '${body}' \\`, `  --output ${outputFile}`)
      return lines.join('\n')
    }
    lines.push(`  -d '${body}'`)
    if (response === 'audio-base64') {
      lines.push(
        '',
        `# ${ctx.translate('The audio field of the response is Base64; decode it to get the audio file.')}`
      )
    }
    if (response === 'audio-url') {
      lines.push(
        '',
        `# ${ctx.translate('Download the audio from output.audio.url in the response; the link is valid for 24 hours.')}`
      )
    }
    if (response === 'task') {
      lines.push(
        '',
        `# ${ctx.translate(POLL_HINT)}`,
        `curl "${fetchUrl}/$TASK_ID" \\`,
        `  -H "Authorization: Bearer $NEW_API_KEY"`
      )
    }
    return lines.join('\n')
  }

  if (lang === 'python') {
    const lines = renderAnnotatedObject(ctx.body, annotations, 'python', 4, '')
    const imports = {
      json: 'import os, requests',
      'audio-binary': 'import os, requests',
      'audio-base64': 'import base64, os, requests',
      'audio-url': 'import os, requests',
      task: 'import os, time, requests',
    }[response]
    const tail = {
      json: ['print(resp.json())'],
      'audio-binary': [
        'resp.raise_for_status()',
        `with open("${outputFile}", "wb") as f:`,
        '    f.write(resp.content)',
      ],
      'audio-base64': [
        'data = resp.json()',
        `with open("${outputFile}", "wb") as f:`,
        '    f.write(base64.b64decode(data["audio"]))',
        'print(data["original_duration"], data.get("subtitle"))',
      ],
      'audio-url': [
        'data = resp.json()',
        `with open("${outputFile}", "wb") as f:`,
        '    f.write(requests.get(data["output"]["audio"]["url"]).content)',
        'print(data["usage"])',
      ],
      task: [
        'task_id = resp.json()["task_id"]',
        `# ${ctx.translate(POLL_HINT)}`,
        'while True:',
        '    time.sleep(3)',
        `    result = requests.get(f"${fetchUrl}/{task_id}", headers=headers).json()`,
        '    if result["status"] in ("succeeded", "failed"):',
        '        break',
        'print(result.get("audio_url"), result.get("error"))',
      ],
    }[response]
    return [
      imports,
      '',
      `# ${ctx.scenarioTitle}`,
      `headers = {"Authorization": f"Bearer {os.environ['NEW_API_KEY']}"}`,
      `resp = requests.post(`,
      `    "${url}",`,
      `    headers=headers,`,
      `    json={`,
      ...lines.map((line) => `    ${line}`),
      `    },`,
      `)`,
      ...tail,
    ].join('\n')
  }

  const lines = renderAnnotatedObject(
    ctx.body,
    annotations,
    'javascript',
    4,
    ''
  )
  const tail = {
    json: ['console.log(await resp.json())'],
    'audio-binary': [
      'if (!resp.ok) throw new Error(await resp.text())',
      `fs.writeFileSync("${outputFile}", Buffer.from(await resp.arrayBuffer()))`,
    ],
    'audio-base64': [
      'const data = await resp.json()',
      `fs.writeFileSync("${outputFile}", Buffer.from(data.audio, "base64"))`,
      'console.log(data.original_duration, data.subtitle)',
    ],
    'audio-url': [
      'const data = await resp.json()',
      'const audio = await fetch(data.output.audio.url)',
      `fs.writeFileSync("${outputFile}", Buffer.from(await audio.arrayBuffer()))`,
      'console.log(data.usage)',
    ],
    task: [
      'const { task_id } = await resp.json()',
      `// ${ctx.translate(POLL_HINT)}`,
      'let result',
      'do {',
      '  await new Promise((r) => setTimeout(r, 3000))',
      `  result = await (await fetch(\`${fetchUrl}/\${task_id}\`, { headers })).json()`,
      '} while (!["succeeded", "failed"].includes(result.status))',
      'console.log(result.audio_url, result.error)',
    ],
  }[response]
  const needsFs =
    response === 'audio-binary' ||
    response === 'audio-base64' ||
    response === 'audio-url'
  return [
    ...(needsFs ? ['import fs from "node:fs"', ''] : []),
    `// ${ctx.scenarioTitle}`,
    `const headers = { Authorization: \`Bearer \${process.env.NEW_API_KEY}\` }`,
    `const resp = await fetch("${url}", {`,
    `  method: "POST",`,
    `  headers: { ...headers, "Content-Type": "application/json" },`,
    `  body: JSON.stringify({`,
    ...lines.map((line) => `  ${line}`),
    `  }),`,
    `})`,
    ...tail,
  ].join('\n')
}
