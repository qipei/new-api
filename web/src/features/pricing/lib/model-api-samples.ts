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
import type { ModelApiParam } from './model-api-specs'

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
    const firstSentence = text.split(/(?<=[。.！!？?])\s*/)[0] || text
    const hint =
      param.enumValues && param.enumValues.length > 0
        ? `${firstSentence} [${param.enumValues.join(' | ')}]`
        : firstSentence
    annotations.set(param.name, hint)
  }
  return annotations
}

function quote(value: unknown): string {
  if (typeof value === 'string') return JSON.stringify(value)
  return String(value)
}

/**
 * 把示例请求体渲染成带注释的字面量。
 * path 用于拼出与参数表一致的完整字段路径，从而找到对应注释。
 */
function renderAnnotatedObject(
  value: Record<string, unknown>,
  annotations: Annotations,
  commentToken: string,
  indent: number,
  path: string
): string[] {
  const pad = ' '.repeat(indent)
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
          commentToken,
          indent + 2,
          fullPath
        )
      )
      lines.push(`${pad}}${comma}`)
      return
    }

    const comment = annotations.get(fullPath)
    const body = `${pad}${JSON.stringify(key)}: ${quote(item)}${comma}`
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
}

export function buildScenarioSample(
  lang: SampleLang,
  ctx: SampleContext
): string {
  const url = `${ctx.baseUrl}${ctx.endpointPath}`
  const annotations = buildAnnotations(ctx.params, ctx.translate)

  if (lang === 'curl') {
    // JSON 不支持注释，保持可直接执行。
    const body = JSON.stringify(ctx.body, null, 2).split('\n').join('\n  ')
    return [
      `# ${ctx.scenarioTitle}`,
      `curl -X POST "${url}" \\`,
      `  -H "Authorization: Bearer $NEW_API_KEY" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '${body}'`,
    ].join('\n')
  }

  if (lang === 'python') {
    const lines = renderAnnotatedObject(ctx.body, annotations, '#', 4, '')
    return [
      'import os, requests',
      '',
      `# ${ctx.scenarioTitle}`,
      `resp = requests.post(`,
      `    "${url}",`,
      `    headers={"Authorization": f"Bearer {os.environ['NEW_API_KEY']}"},`,
      `    json={`,
      ...lines.map((line) => `    ${line}`),
      `    },`,
      `)`,
      'print(resp.json())',
    ].join('\n')
  }

  const lines = renderAnnotatedObject(ctx.body, annotations, '//', 4, '')
  return [
    `// ${ctx.scenarioTitle}`,
    `const resp = await fetch("${url}", {`,
    `  method: "POST",`,
    `  headers: {`,
    `    Authorization: \`Bearer \${process.env.NEW_API_KEY}\`,`,
    `    "Content-Type": "application/json",`,
    `  },`,
    `  body: JSON.stringify({`,
    ...lines.map((line) => `  ${line}`),
    `  }),`,
    `})`,
    `console.log(await resp.json())`,
  ].join('\n')
}
