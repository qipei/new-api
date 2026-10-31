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
// CUSTOM: 价格表的分组价（fork 扩展），与后端 setting/video_billing 的 group_prices 一致：
// 配了分组价的分组按它代替该档原价（仍乘分组倍率），且不使用闲时价。卡片单价、分组价格块、
// 最便宜分组和最低折扣都按这里的口径算，四处才对得上。
import type { PricingModel, VideoPriceTable, VideoPriceTier } from '../types'

/**
 * 该分组实际适用的档位，与后端取价一致：配了有效分组价时用它代替原价，并且不再按
 * 闲时价计。
 */
export function tierForGroup(
  tier: VideoPriceTier,
  group: string
): VideoPriceTier {
  const price = tier.group_prices?.[group]
  if (typeof price !== 'number' || !Number.isFinite(price) || price <= 0) {
    return tier
  }
  return { ...tier, price, off_peak_price: undefined }
}

/**
 * 该分组实际适用的价格表输入价，与后端 ModelPriceTable.ForGroup 一致：配了分组输入价时
 * 用它代替输入图、输入视频单价，没填的项沿用价格表的；负数等无效值忽略。
 */
export function tableForGroup(
  table: VideoPriceTable,
  group: string
): VideoPriceTable {
  const prices = table.group_input_prices?.[group]
  if (!prices) return table
  const valid = (price: number | undefined): price is number =>
    typeof price === 'number' && Number.isFinite(price) && price >= 0
  return {
    ...table,
    input_image_price: valid(prices.input_image_price)
      ? prices.input_image_price
      : table.input_image_price,
    input_video_price: valid(prices.input_video_price)
      ? prices.input_video_price
      : table.input_video_price,
  }
}

function cheapestTierPrice(tiers: VideoPriceTier[]): number {
  let minimum = Number.POSITIVE_INFINITY
  for (const tier of tiers) {
    const price = Number(tier.price)
    if (Number.isFinite(price) && price > 0 && price < minimum) minimum = price
  }
  return minimum
}

/**
 * 分组价让该分组相对原价便宜（或贵）了多少：该分组能用的最便宜一档（按分组价）÷ 价格表
 * 最便宜一档的原价，再乘分组倍率就是该分组相对原价的有效折扣。没配分组价时返回
 * undefined（各分组之间只差分组倍率）；该分组一档都用不了时返回 Infinity。
 */
export function groupMatrixPriceFactor(
  model: Pick<PricingModel, 'matrix_price_table'>,
  group: string
): number | undefined {
  const tiers = model.matrix_price_table?.tiers ?? []
  if (!tiers.some((tier) => tier.group_prices)) return undefined
  const base = cheapestTierPrice(tiers)
  const available = tiers
    .filter((tier) => !tier.excluded_groups?.includes(group))
    .map((tier) => tierForGroup(tier, group))
  const groupMinimum = cheapestTierPrice(available)
  if (!Number.isFinite(base) || !Number.isFinite(groupMinimum)) {
    return Number.POSITIVE_INFINITY
  }
  return groupMinimum / base
}
