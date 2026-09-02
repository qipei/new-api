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
// CUSTOM: 限时活动的解析/序列化/校验（fork 扩展）。
//
// 活动是乘在最终价格上的一个倍率，不改模型的计费方式：常规按倍率计费的模型和
// 动态计费的模型都适用，活动一过自动失效，不需要还原。

import { parseJsonRecord } from './group-billing-expr-core'

export const MODEL_PROMOTIONS_OPTION_KEY = 'billing_setting.model_promotions'

export const PROMOTION_DEFAULT_TZ = 'Asia/Shanghai'

export interface ModelPromotion {
  name: string
  start: string
  end: string
  tz?: string
  ratio: number
  /** 为空表示该模型所有分组都参与 */
  groups?: string[]
}

/** 模型名 -> 活动列表 */
export type ModelPromotionMap = Record<string, ModelPromotion[]>

const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/

export function parsePromotions(raw: string): ModelPromotionMap {
  const parsed = parseJsonRecord<unknown>(raw)
  const out: ModelPromotionMap = {}
  for (const [model, value] of Object.entries(parsed)) {
    if (!Array.isArray(value)) continue
    const list = value.filter(
      (item): item is ModelPromotion =>
        !!item && typeof item === 'object' && !Array.isArray(item)
    )
    if (list.length > 0) out[model] = list
  }
  return out
}

/** 丢掉没有活动的模型，避免存一堆空数组。 */
export function serializePromotions(map: ModelPromotionMap): string {
  const cleaned: ModelPromotionMap = {}
  for (const [model, promotions] of Object.entries(map)) {
    if (!model.trim() || promotions.length === 0) continue
    // 显式取字段而不是 spread：界面给每条活动带了个只用于 React key 的客户端
    // uid，spread 会把它一起写进库。
    cleaned[model] = promotions.map((promotion) => ({
      name: promotion.name,
      start: promotion.start,
      end: promotion.end,
      tz: promotion.tz,
      ratio: promotion.ratio,
      groups: promotion.groups?.length ? promotion.groups : undefined,
    }))
  }
  return JSON.stringify(cleaned)
}

/**
 * 校验规则和后端 ValidateModelPromotionsJSON 保持一致，好让错误在点保存之前
 * 就出现在输入框旁边，而不是提交后从接口弹一句红字。
 */
export function validatePromotion(promotion: ModelPromotion): string | null {
  if (!promotion.name.trim()) {
    return 'Promotion name is required.'
  }
  if (!DATE_PATTERN.test(promotion.start)) {
    return 'Start date must be YYYY-MM-DD.'
  }
  if (!DATE_PATTERN.test(promotion.end)) {
    return 'End date must be YYYY-MM-DD.'
  }
  if (promotion.end < promotion.start) {
    return 'The end date must not be earlier than the start date.'
  }
  if (
    !Number.isFinite(promotion.ratio) ||
    promotion.ratio <= 0 ||
    promotion.ratio > 1
  ) {
    return 'The promotion multiplier must be greater than 0 and at most 1.'
  }
  return null
}

export type PromotionPhase = 'upcoming' | 'live' | 'ended'

/** today 传 YYYY-MM-DD，由调用方按活动时区算好。 */
export function promotionPhase(
  promotion: ModelPromotion,
  today: string
): PromotionPhase {
  if (today < promotion.start) return 'upcoming'
  if (today > promotion.end) return 'ended'
  return 'live'
}

export function todayInZone(tz: string, now = new Date()): string {
  try {
    return new Intl.DateTimeFormat('en-CA', {
      timeZone: tz || PROMOTION_DEFAULT_TZ,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(now)
  } catch {
    // 时区串写错时退回本地日期，宁可算错一天也不要整块界面炸掉。
    return new Intl.DateTimeFormat('en-CA').format(now)
  }
}

/**
 * 活动倍率和分组倍率是相乘的，2.5折 分组叠 5折 活动就是 1.25折。运营配的时候
 * 必须看得见最低会到几折——我们只展示、不拦截，真要做大促时硬限制会挡路。
 */
export function effectiveDiscounts(
  promotion: ModelPromotion,
  groupRatios: Record<string, number>,
  enabledGroups: string[]
): { group: string; groupRatio: number; effective: number }[] {
  const scope = promotion.groups?.length ? promotion.groups : enabledGroups
  return scope
    .map((group) => {
      const groupRatio = groupRatios[group]
      if (!Number.isFinite(groupRatio)) return null
      return {
        group,
        groupRatio,
        effective: groupRatio * promotion.ratio,
      }
    })
    .filter((row): row is NonNullable<typeof row> => row !== null)
    .sort((a, b) => a.effective - b.effective)
}
