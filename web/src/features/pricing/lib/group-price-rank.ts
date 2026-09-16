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
// CUSTOM: 模型广场"当前最便宜分组"的判定（fork 扩展）。
//
// 后端比价路由（service/group_price_rank.go）按「表达式成本 × 有效分组倍率」给
// 分组排序，而价格页此前只比分组倍率。两个分组倍率相同、表达式不同时（例如同一
// 个模型在两个分组下接的是两个价格完全不同的上游），页面会把最贵的那条表达式当
// 成"最低价"展示出来。这里把展示口径对齐到同一个乘式。
//
// 比较口径：把输入、输出、缓存读各记 1 个 token 代进表达式，算出来就是这三项**当
// 前时段**单价的和，再乘有效分组倍率（含限时活动）。不数真实 token 是有意的——分
// 组之间比高低不需要真实用量，同一个模型各分组的差别只在系数和倍率上；而分时表达
// 式（闲时/忙时）必须按此刻求值，否则两个时段会一直按同一档比。
//
// 代价要说清楚：长度分档的表达式在这个口径下一律落到低档（len 记 1），两个分组的
// 档位曲线如果交叉，长上下文请求的实际最优分组可能不是这里选出来的那个。所以这是
// 排序口径，不承诺每一个请求都最优。
import { FILTER_ALL } from '../constants'
import type { PricingModel } from '../types'
import { splitBillingExprAndRequestRules } from './billing-expr'
import { resolveBillingExprForGroup } from './group-billing-expr'
import { getConfiguredGroupRatio } from './model-helpers'
import { effectiveGroupRatio } from './model-promotion'
import { evalExprLocally } from './tier-expr'

/** 各维度都记 0，只有输入/输出/缓存读记 1，见文件头的口径说明。 */
const UNIT_TOKENS = {
  cacheReadTokens: 1,
  cacheCreateTokens: 0,
  cacheCreate1hTokens: 0,
  imageTokens: 0,
  imageOutputTokens: 0,
  audioInputTokens: 0,
  audioOutputTokens: 0,
}

/**
 * 当前时段的「输入 + 输出 + 缓存读」单价之和。
 *
 * 求值失败返回 null，调用方退回只比倍率——一条算不出来的表达式不该凭空排到最前
 * 面（与后端 exprUnitCost 的失败处理同义）。请求规则（||| 之后那段）先切掉：页面
 * 上没有请求可读，留着只会让整条表达式求值失败。
 */
function currentUnitPrice(expr: string | undefined): number | null {
  if (!expr || !expr.trim()) return null
  const { billingExpr } = splitBillingExprAndRequestRules(expr)
  const { cost, error } = evalExprLocally(billingExpr, 1, 1, UNIT_TOKENS)
  if (error || !Number.isFinite(cost)) return null
  return cost
}

function modelGroups(model: PricingModel): string[] {
  return Array.isArray(model.enable_groups) ? model.enable_groups : []
}

/**
 * 该分组当前的展示价格，用于分组之间比高低。数值本身没有货币含义，只在同一个
 * 模型的各分组之间可比。
 *
 * 按倍率计费的模型没有表达式可比，比较就退化成纯比有效倍率——和改动之前的行为
 * 一致。
 */
function groupDisplayCost(model: PricingModel, group: string): number {
  const ratio = effectiveGroupRatio(
    model,
    group,
    getConfiguredGroupRatio(model.group_ratio || {}, group)
  )
  if (!Number.isFinite(ratio) || ratio < 0) return Number.POSITIVE_INFINITY

  if (model.billing_mode !== 'tiered_expr') return ratio

  const unitPrice = currentUnitPrice(resolveBillingExprForGroup(model, group))
  // 算不出价的分组排到最后。不能退回只比倍率：倍率是个 1 附近的数，而其它分组的
  // 分数是"单价 × 倍率"，量纲差着几个数量级，一混就等于让这条坏配置稳稳排第一。
  if (unitPrice === null) return Number.POSITIVE_INFINITY
  return unitPrice * ratio
}

/**
 * 该模型当前应当展示哪个分组的价格：选中了分组就是它，否则是最便宜的那个。
 *
 * 同价时按分组名排序，和后端 RankGroupsByPrice 一样——否则 enable_groups 的顺序
 * 一变，页面上的价格就会跟着换一个分组，无从对账。
 */
export function resolveDisplayGroup(
  model: PricingModel,
  selectedGroup?: string
): string | undefined {
  const groups = modelGroups(model)
  if (
    selectedGroup &&
    selectedGroup !== FILTER_ALL &&
    groups.includes(selectedGroup)
  ) {
    return selectedGroup
  }

  // 与后端 RankGroupsByPrice 一致按字节序打平，不用 localeCompare：那会让中文
  // 分组名在两端排出不同顺序。
  const byName = [...groups].sort((a, b) => {
    if (a === b) return 0
    return a < b ? -1 : 1
  })

  // 全都算不出价时留下名字最小的那个：页面总得显示一个分组，而这个选择必须稳定。
  let best = byName[0]
  let bestCost = best === undefined ? 0 : groupDisplayCost(model, best)
  for (const group of byName.slice(1)) {
    const cost = groupDisplayCost(model, group)
    if (cost < bestCost) {
      best = group
      bestCost = cost
    }
  }
  return best
}

/**
 * 展示价格该乘的分组倍率：所选（或最便宜）分组的倍率 × 进行中的限时活动倍率。
 *
 * 活动必须乘进来。后端把活动倍率乘进了实际计费的 GroupRatio，页面不乘就会显示
 * 原价而按活动价扣费；而且比价时算了活动、展示时不算，还会让选出来的"最便宜分组"
 * 配上一个更贵的数字。
 */
export function displayGroupRatio(
  model: PricingModel,
  selectedGroup?: string
): number {
  const group = resolveDisplayGroup(model, selectedGroup)
  if (!group) return 1
  return effectiveGroupRatio(
    model,
    group,
    getConfiguredGroupRatio(model.group_ratio || {}, group)
  )
}

/**
 * 把列表里每个模型的 billing_expr 换成它该展示的那个分组生效的那条，下游十几处
 * 读 billing_expr 的渲染代码就不必各自感知分组。
 *
 * 只在表达式真的变了时才复制模型对象：下游一堆 memo 以模型数组的引用为依赖，没
 * 有替换却返回新数组会把它们全部击穿。展示分组本身不往模型上写——resolveDisplay-
 * Group 是模型的纯函数（替换后 base_billing_expr 仍留着模型级原件，各分组照样能
 * 算），谁要用谁自己算，省掉一份会和筛选状态不同步的冗余字段。
 */
export function withDisplayGroupPricing(
  models: PricingModel[],
  selectedGroup?: string
): PricingModel[] {
  if (!models.length) return models

  let changed = false
  const resolved = models.map((model) => {
    const group = resolveDisplayGroup(model, selectedGroup)
    if (!group) return model
    const expr = resolveBillingExprForGroup(model, group)
    if (!expr || expr === model.billing_expr) return model
    changed = true
    return {
      ...model,
      base_billing_expr: model.base_billing_expr ?? model.billing_expr,
      billing_expr: expr,
    }
  })
  return changed ? resolved : models
}
