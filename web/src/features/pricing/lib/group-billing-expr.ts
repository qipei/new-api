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
// CUSTOM: 分组级计费表达式的前端解析（fork 扩展）。
//
// 同一个模型在不同分组下可以走不同的表达式（例如某个分组分时打折、另一个分组
// 固定价）。价格页有十几处地方读 model.billing_expr，与其把 selectedGroup 一路
// 传下去，不如在数据进入渲染之前就把 billing_expr 换成该分组实际生效的那条——
// 下游一行都不用改，和上游的合并面也小。
import { FILTER_ALL } from '../constants'
import type { PricingModel } from '../types'

/** 模型级表达式的原件。billing_expr 可能已被换成某个分组的覆盖，回落必须用这份。 */
function baseBillingExpr(model: PricingModel): string | undefined {
  return model.base_billing_expr ?? model.billing_expr
}

/** 返回该分组实际生效的表达式：先找分组覆盖，再回落到模型级。与后端 GetBillingExprForGroup 同序。 */
export function resolveBillingExprForGroup(
  model: PricingModel,
  selectedGroup?: string
): string | undefined {
  if (selectedGroup && selectedGroup !== FILTER_ALL) {
    const override = model.group_billing_expr?.[selectedGroup]
    if (override && override.trim()) return override
  }
  return baseBillingExpr(model)
}

/**
 * 把列表里每个模型的 billing_expr 换成选中分组实际生效的那条。
 * 没有任何模型需要替换时原样返回入参，避免下游 memo 被无谓地击穿。
 */
export function withGroupBillingExpr(
  models: PricingModel[],
  selectedGroup?: string
): PricingModel[] {
  if (!models.length || !selectedGroup || selectedGroup === FILTER_ALL) {
    return models
  }

  let changed = false
  const resolved = models.map((model) => {
    const expr = resolveBillingExprForGroup(model, selectedGroup)
    if (!expr || expr === model.billing_expr) return model
    changed = true
    return {
      ...model,
      base_billing_expr: baseBillingExpr(model),
      billing_expr: expr,
    }
  })
  return changed ? resolved : models
}

/** 该模型在哪些分组上偏离了模型级表达式，用于在详情里说明"价格因分组而异"。 */
export function groupsWithBillingExprOverride(model: PricingModel): string[] {
  const overrides = model.group_billing_expr
  if (!overrides) return []
  const enabled = Array.isArray(model.enable_groups) ? model.enable_groups : []
  const base = baseBillingExpr(model)
  return Object.keys(overrides)
    .filter((group) => {
      const expr = overrides[group]
      if (!expr || !expr.trim()) return false
      if (expr === base) return false
      return enabled.length === 0 || enabled.includes(group)
    })
    .sort((a, b) => a.localeCompare(b))
}
