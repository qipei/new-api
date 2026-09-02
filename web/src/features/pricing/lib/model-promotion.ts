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
