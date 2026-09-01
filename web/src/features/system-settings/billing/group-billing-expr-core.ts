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
// CUSTOM: 分组级计费表达式的解析/序列化/校验（fork 扩展）。

export const GROUP_BILLING_EXPR_OPTION_KEY =
  'billing_setting.group_billing_expr'

/** 模型名 -> 分组名 -> 表达式 */
export type GroupBillingExprMap = Record<string, Record<string, string>>

export function parseJsonRecord<V>(raw: string): Record<string, V> {
  if (!raw || !raw.trim()) return {}
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
      return {}
    return parsed as Record<string, V>
  } catch {
    return {}
  }
}

/**
 * 序列化时丢掉空表达式和空模型：后端把空串当作"没有覆盖"，前端留着只会让
 * 保存按钮永远是脏的。键按字典序输出，保证同样的配置产生同样的 JSON。
 */
export function serializeGroupBillingExpr(map: GroupBillingExprMap): string {
  const out: GroupBillingExprMap = {}
  for (const model of Object.keys(map).sort((a, b) => a.localeCompare(b))) {
    const byGroup = map[model] || {}
    const cleaned: Record<string, string> = {}
    for (const group of Object.keys(byGroup).sort((a, b) =>
      a.localeCompare(b)
    )) {
      const expr = (byGroup[group] || '').trim()
      if (expr) cleaned[group] = expr
    }
    if (Object.keys(cleaned).length > 0) out[model] = cleaned
  }
  return JSON.stringify(out)
}

/**
 * 保存前的浅校验。表达式的完整语义由后端求值，这里只挡住肯定跑不起来的形状，
 * 免得一条写坏的表达式要等到真实请求才炸。返回第一条问题的描述，没问题返回空串。
 */
export function validateGroupBillingExpr(map: GroupBillingExprMap): string {
  for (const model of Object.keys(map)) {
    for (const group of Object.keys(map[model] || {})) {
      const expr = (map[model][group] || '').trim()
      if (!expr) continue
      if (!expr.includes('tier(')) {
        return `${model} / ${group}: missing tier(...)`
      }
      let depth = 0
      for (const char of expr) {
        if (char === '(') depth += 1
        if (char === ')') depth -= 1
        if (depth < 0) break
      }
      if (depth !== 0) {
        return `${model} / ${group}: unbalanced parentheses`
      }
    }
  }
  return ''
}
