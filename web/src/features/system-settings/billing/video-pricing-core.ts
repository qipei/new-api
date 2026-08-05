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

// CUSTOM: 视频定价矩阵编辑器的解析/序列化/校验逻辑，
// 与后端 setting/video_billing 的 JSON 结构（video_billing.price_tables）保持一致。

export type VideoPriceTierPayload = {
  mode?: string
  resolution?: string
  audio?: string
  price: number
}

export type VideoPriceTablePayload = {
  base_price: number
  tiers?: VideoPriceTierPayload[]
}

export type EditableVideoTier = {
  uid: string
  mode: string
  resolution: string
  audio: string
  price: string
}

let editableTierSeq = 0

export function nextEditableTierUid(): string {
  editableTierSeq += 1
  return `tier-${editableTierSeq}`
}

export type EditableVideoTable = {
  model: string
  basePrice: string
  tiers: EditableVideoTier[]
}

export const VIDEO_MODES = ['', 't2v', 'i2v', 'v2v'] as const
export const VIDEO_AUDIO_DIMENSIONS = ['', 'on', 'off'] as const

export function parseVideoPricingTables(raw: string): EditableVideoTable[] {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw || '{}')
  } catch {
    return []
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return []
  }
  const tables: EditableVideoTable[] = []
  for (const [model, value] of Object.entries(
    parsed as Record<string, unknown>
  )) {
    if (value === null || typeof value !== 'object') continue
    const table = value as Partial<VideoPriceTablePayload>
    const tiers = Array.isArray(table.tiers) ? table.tiers : []
    tables.push({
      model,
      basePrice:
        typeof table.base_price === 'number' ? String(table.base_price) : '',
      tiers: tiers.map((tier) => ({
        uid: nextEditableTierUid(),
        mode: typeof tier.mode === 'string' ? tier.mode : '',
        resolution: typeof tier.resolution === 'string' ? tier.resolution : '',
        audio: typeof tier.audio === 'string' ? tier.audio : '',
        price: typeof tier.price === 'number' ? String(tier.price) : '',
      })),
    })
  }
  tables.sort((a, b) => a.model.localeCompare(b.model))
  return tables
}

export function serializeVideoPricingTables(
  tables: EditableVideoTable[]
): string {
  const payload: Record<string, VideoPriceTablePayload> = {}
  for (const table of tables) {
    const model = table.model.trim()
    if (!model) continue
    const entry: VideoPriceTablePayload = {
      base_price: Number(table.basePrice),
    }
    const tiers: VideoPriceTierPayload[] = []
    for (const tier of table.tiers) {
      const payloadTier: VideoPriceTierPayload = { price: Number(tier.price) }
      if (tier.mode) payloadTier.mode = tier.mode
      if (tier.resolution.trim()) {
        payloadTier.resolution = tier.resolution.trim().toLowerCase()
      }
      if (tier.audio) payloadTier.audio = tier.audio
      tiers.push(payloadTier)
    }
    if (tiers.length > 0) entry.tiers = tiers
    payload[model] = entry
  }
  return JSON.stringify(payload)
}

export type VideoPricingIssue = {
  model: string
  reason: 'base_price' | 'tier_price' | 'duplicate_tier'
}

export function validateVideoPricingTables(
  tables: EditableVideoTable[]
): VideoPricingIssue[] {
  const issues: VideoPricingIssue[] = []
  for (const table of tables) {
    const model = table.model.trim()
    if (!model) continue
    const basePrice = Number(table.basePrice)
    if (!Number.isFinite(basePrice) || basePrice <= 0) {
      issues.push({ model, reason: 'base_price' })
    }
    const seen = new Set<string>()
    for (const tier of table.tiers) {
      const price = Number(tier.price)
      if (!Number.isFinite(price) || price <= 0) {
        issues.push({ model, reason: 'tier_price' })
      }
      const key = `${tier.mode}|${tier.resolution.trim().toLowerCase()}|${tier.audio}`
      if (seen.has(key)) {
        issues.push({ model, reason: 'duplicate_tier' })
      }
      seen.add(key)
    }
  }
  return issues
}
