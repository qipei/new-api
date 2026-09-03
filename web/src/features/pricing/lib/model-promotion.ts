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
// CUSTOM: 模型广场读取限时活动（fork 扩展）。
//
// 后端把活动倍率乘进了 HandleGroupRatio 返回的 GroupRatio，所以模型广场这边也
// 必须把它乘进展示用的倍率——否则页面显示原价、实际按活动价扣，两边对不上。

import type { ModelPromotionInfo, PricingModel } from '../types'

/**
 * 该分组当前生效的活动。多条同时命中时取倍率最低的，和后端
 * ActivePromotionAt 的取法保持一致。
 */
export function activePromotionForGroup(
  model: Pick<PricingModel, 'promotions'>,
  group: string
): ModelPromotionInfo | null {
  const promotions = model.promotions
  if (!promotions?.length) return null

  let best: ModelPromotionInfo | null = null
  for (const promotion of promotions) {
    const scoped = promotion.groups?.length
      ? promotion.groups.includes(group)
      : true
    if (!scoped) continue
    if (!Number.isFinite(promotion.ratio) || promotion.ratio <= 0) continue
    if (!best || promotion.ratio < best.ratio) best = promotion
  }
  return best
}

/** 分组倍率 × 活动倍率。没有活动时原样返回。 */
export function effectiveGroupRatio(
  model: Pick<PricingModel, 'promotions'>,
  group: string,
  groupRatio: number
): number {
  const promotion = activePromotionForGroup(model, group)
  return promotion ? groupRatio * promotion.ratio : groupRatio
}

/** 该模型是否有任一分组正在活动中，用于卡片角标。 */
export function hasAnyActivePromotion(
  model: Pick<PricingModel, 'promotions'>
): boolean {
  return (model.promotions?.length ?? 0) > 0
}

/** 当前生效的最低折扣。ratio 是有效倍率，小于 1 才算有折扣。 */
export interface BestDiscount {
  /** 有效倍率：分组倍率 × 活动倍率 */
  ratio: number
  group: string
  promotion: ModelPromotionInfo | null
}

/**
 * 该模型在所有可用分组里能拿到的最低折扣。
 *
 * 折扣有两个来源，必须一起看：分组倍率本身小于 1，或者 default 这类倍率为 1 的
 * 分组上挂了限时活动。只看其中一个都会漏——列表和卡片上要展示的是"这个模型现在
 * 最低能到几折"，用户不关心是靠哪种方式拿到的。
 */
export function bestDiscount(
  model: Pick<PricingModel, 'promotions' | 'enable_groups' | 'group_ratio'>
): BestDiscount | null {
  const groups = Array.isArray(model.enable_groups) ? model.enable_groups : []
  const ratios = model.group_ratio || {}

  let best: BestDiscount | null = null
  for (const group of groups) {
    const base = ratios[group]
    if (!Number.isFinite(base) || base <= 0) continue
    const promotion = activePromotionForGroup(model, group)
    const ratio = promotion ? base * promotion.ratio : base
    if (!Number.isFinite(ratio) || ratio <= 0) continue
    if (!best || ratio < best.ratio) best = { ratio, group, promotion }
  }
  if (!best || best.ratio >= 1) return null
  return best
}

/**
 * 把倍率写成中文习惯的"折"：0.83 → 8.3折，0.5 → 5折，0.25 → 2.5折。
 * 保留一位小数，整数不带小数点。
 */
export function formatDiscountLabel(ratio: number): string {
  const tenths = Math.round(ratio * 100) / 10
  return `${Number(tenths.toFixed(1))}折`
}
