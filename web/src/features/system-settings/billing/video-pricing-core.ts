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
// 语义：每档为绝对原价（USD），计价单位按模型选择（每秒 / 每百万 token），
// 计费只再乘分组倍率；必须有一个无维度的默认档兜底。

export type VideoPriceTierPayload = {
  mode?: string
  resolution?: string
  audio?: string
  min_pixels?: number
  max_pixels?: number
  price: number
  // CUSTOM: 不适用该档位的分组，为空表示所有分组都适用（fork 扩展）
  excluded_groups?: string[]
}

export type VideoPriceTablePayload = {
  unit: string
  tiers?: VideoPriceTierPayload[]
  resolution_buckets?: VideoResolutionBucketPayload[]
  input_image_price?: number
  input_token_price?: number
  // CUSTOM: 每次请求免费的输入图张数，不填为 0，每张都计费（fork 扩展）
  free_input_images?: number
  // CUSTOM: 输入视频每秒单价，仅按秒计费；不填与输出档位同价，0 为免费（fork 扩展）
  input_video_price?: number
}

export type VideoResolutionBucketPayload = {
  name: string
  sizes?: string[]
}

export type EditableVideoTier = {
  uid: string
  mode: string
  resolution: string
  audio: string
  minPixels: string
  maxPixels: string
  price: string
  excludedGroups: string[]
}

export type EditableVideoTable = {
  model: string
  unit: string
  tiers: EditableVideoTier[]
  inputImagePrice: string
  inputTokenPrice: string
  freeInputImages: string
  inputVideoPrice: string
  resolutionTemplate?: string
  resolutionBuckets: EditableResolutionBucket[]
}

export type EditableResolutionBucket = {
  uid: string
  name: string
  sizes: string
}

export type VideoPricingJsonDraftResult =
  | { tables: EditableVideoTable[]; error: null }
  | { tables: null; error: 'json' | 'shape' }

export const VIDEO_UNIT_PER_SECOND = 'per_second'
export const VIDEO_UNIT_PER_MILLION_TOKENS = 'per_million_tokens'
export const VIDEO_UNIT_PER_IMAGE = 'per_image'
// 按字计价（语音合成）：原价为 USD/万字符，只用默认档。
export const VIDEO_UNIT_PER_10K_CHARS = 'per_10k_chars'

/** 免费张数的合理上限，与后端单次结算可计费张数的上限一致。 */
export const MAX_FREE_INPUT_IMAGES = 100

let editableTierSeq = 0
let editableBucketSeq = 0

export function nextEditableTierUid(): string {
  editableTierSeq += 1
  return `tier-${editableTierSeq}`
}

export function nextEditableBucketUid(): string {
  editableBucketSeq += 1
  return `bucket-${editableBucketSeq}`
}

function normalizeConfiguredSize(size: string): string {
  return size.trim().toLowerCase().replaceAll('*', 'x')
}

function splitConfiguredSizes(sizes: string): string[] {
  return [
    ...new Set(
      sizes
        .split(/[\s,，]+/)
        .map(normalizeConfiguredSize)
        .filter(Boolean)
    ),
  ]
}

function canonicalConfiguredSize(size: string): string | null {
  const match = /^(\d+)x(\d+)$/.exec(normalizeConfiguredSize(size))
  if (!match) return null
  const width = Number(match[1])
  const height = Number(match[2])
  if (
    !Number.isSafeInteger(width) ||
    !Number.isSafeInteger(height) ||
    width <= 0 ||
    height <= 0
  ) {
    return null
  }
  return width <= height ? `${width}x${height}` : `${height}x${width}`
}

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
    const tiers = Array.isArray(table.tiers)
      ? table.tiers.filter(
          (tier): tier is VideoPriceTierPayload =>
            tier !== null && typeof tier === 'object' && !Array.isArray(tier)
        )
      : []
    const resolutionBuckets = Array.isArray(table.resolution_buckets)
      ? table.resolution_buckets.filter(
          (bucket): bucket is VideoResolutionBucketPayload =>
            bucket !== null &&
            typeof bucket === 'object' &&
            !Array.isArray(bucket)
        )
      : []
    tables.push({
      model,
      unit: typeof table.unit === 'string' ? table.unit : VIDEO_UNIT_PER_SECOND,
      inputImagePrice:
        typeof table.input_image_price === 'number'
          ? String(table.input_image_price)
          : '',
      inputTokenPrice:
        typeof table.input_token_price === 'number'
          ? String(table.input_token_price)
          : '',
      freeInputImages:
        typeof table.free_input_images === 'number'
          ? String(table.free_input_images)
          : '',
      inputVideoPrice:
        typeof table.input_video_price === 'number'
          ? String(table.input_video_price)
          : '',
      resolutionTemplate: '',
      resolutionBuckets: resolutionBuckets.map((bucket) => ({
        uid: nextEditableBucketUid(),
        name: typeof bucket.name === 'string' ? bucket.name : '',
        sizes: Array.isArray(bucket.sizes)
          ? bucket.sizes.filter((size) => typeof size === 'string').join(', ')
          : '',
      })),
      tiers: tiers.map((tier) => ({
        uid: nextEditableTierUid(),
        mode: typeof tier.mode === 'string' ? tier.mode : '',
        resolution: typeof tier.resolution === 'string' ? tier.resolution : '',
        audio: typeof tier.audio === 'string' ? tier.audio : '',
        minPixels:
          typeof tier.min_pixels === 'number' ? String(tier.min_pixels) : '',
        maxPixels:
          typeof tier.max_pixels === 'number' ? String(tier.max_pixels) : '',
        price: typeof tier.price === 'number' ? String(tier.price) : '',
        excludedGroups: Array.isArray(tier.excluded_groups)
          ? tier.excluded_groups.filter(
              (group): group is string => typeof group === 'string'
            )
          : [],
      })),
    })
  }
  tables.sort((a, b) => a.model.localeCompare(b.model))
  return tables
}

export function parseVideoPricingJsonDraft(
  raw: string
): VideoPricingJsonDraftResult {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw || '{}')
  } catch {
    return { tables: null, error: 'json' }
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return { tables: null, error: 'shape' }
  }
  for (const value of Object.values(parsed)) {
    if (value === null || typeof value !== 'object' || Array.isArray(value)) {
      return { tables: null, error: 'shape' }
    }
    const table = value as Partial<VideoPriceTablePayload>
    if (table.unit !== undefined && typeof table.unit !== 'string') {
      return { tables: null, error: 'shape' }
    }
    if (
      table.input_image_price !== undefined &&
      typeof table.input_image_price !== 'number'
    ) {
      return { tables: null, error: 'shape' }
    }
    if (
      table.input_token_price !== undefined &&
      typeof table.input_token_price !== 'number'
    ) {
      return { tables: null, error: 'shape' }
    }
    if (
      table.free_input_images !== undefined &&
      typeof table.free_input_images !== 'number'
    ) {
      return { tables: null, error: 'shape' }
    }
    if (
      table.input_video_price !== undefined &&
      typeof table.input_video_price !== 'number'
    ) {
      return { tables: null, error: 'shape' }
    }
    const tiers = table.tiers
    if (tiers !== undefined && !Array.isArray(tiers)) {
      return { tables: null, error: 'shape' }
    }
    if (
      Array.isArray(tiers) &&
      tiers.some(
        (tier) =>
          tier === null || typeof tier !== 'object' || Array.isArray(tier)
      )
    ) {
      return { tables: null, error: 'shape' }
    }
    const resolutionBuckets = table.resolution_buckets
    if (
      resolutionBuckets !== undefined &&
      (!Array.isArray(resolutionBuckets) ||
        resolutionBuckets.some(
          (bucket) =>
            bucket === null ||
            typeof bucket !== 'object' ||
            Array.isArray(bucket) ||
            typeof bucket.name !== 'string' ||
            (bucket.sizes !== undefined &&
              (!Array.isArray(bucket.sizes) ||
                bucket.sizes.some((size) => typeof size !== 'string')))
        ))
    ) {
      return { tables: null, error: 'shape' }
    }
    if (
      Array.isArray(tiers) &&
      tiers.some(
        (tier) =>
          typeof tier.price !== 'number' ||
          (tier.mode !== undefined && typeof tier.mode !== 'string') ||
          (tier.resolution !== undefined &&
            typeof tier.resolution !== 'string') ||
          (tier.audio !== undefined && typeof tier.audio !== 'string') ||
          (tier.min_pixels !== undefined &&
            typeof tier.min_pixels !== 'number') ||
          (tier.max_pixels !== undefined &&
            typeof tier.max_pixels !== 'number') ||
          (tier.excluded_groups !== undefined &&
            (!Array.isArray(tier.excluded_groups) ||
              tier.excluded_groups.some((group) => typeof group !== 'string')))
      )
    ) {
      return { tables: null, error: 'shape' }
    }
  }
  return {
    tables: parseVideoPricingTables(JSON.stringify(parsed)),
    error: null,
  }
}

export function formatVideoPricingJson(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw || '{}'), null, 2)
  } catch {
    return raw
  }
}

export function serializeVideoPricingTables(
  tables: EditableVideoTable[]
): string {
  const payload: Record<string, VideoPriceTablePayload> = {}
  const sortedTables = [...tables].sort((a, b) =>
    a.model.localeCompare(b.model)
  )
  for (const table of sortedTables) {
    const model = table.model.trim()
    if (!model) continue
    const entry: VideoPriceTablePayload = { unit: table.unit }
    // 输入图单价与归档表对按张和按秒都生效：视频模型的参考图同样产生上游成本，
    // 而通用的短边归档规则表达不了供应商自定义的档位（例如 Vidu 的 540P）。
    // 输入 token 单价目前只有按张计费在用。
    if (
      table.unit === VIDEO_UNIT_PER_IMAGE ||
      table.unit === VIDEO_UNIT_PER_SECOND
    ) {
      const inputImagePrice = Number(table.inputImagePrice)
      if (table.inputImagePrice.trim() && inputImagePrice > 0) {
        entry.input_image_price = inputImagePrice
        // 免费张数只扣减输入图单价，没有单价时不写入。
        const freeInputImages = Number(table.freeInputImages)
        if (
          table.freeInputImages.trim() &&
          Number.isInteger(freeInputImages) &&
          freeInputImages > 0
        ) {
          entry.free_input_images = freeInputImages
        }
      }
      // 输入视频单价只对按秒计费有意义；0 是「免费」，与不填（同输出档位单价）不同。
      const inputVideoPrice = Number(table.inputVideoPrice)
      if (
        table.unit === VIDEO_UNIT_PER_SECOND &&
        table.inputVideoPrice.trim() &&
        Number.isFinite(inputVideoPrice) &&
        inputVideoPrice >= 0
      ) {
        entry.input_video_price = inputVideoPrice
      }
      if (table.unit === VIDEO_UNIT_PER_IMAGE) {
        const inputTokenPrice = Number(table.inputTokenPrice)
        if (table.inputTokenPrice.trim() && inputTokenPrice > 0) {
          entry.input_token_price = inputTokenPrice
        }
      }
      const resolutionBuckets = table.resolutionBuckets
        .map((bucket): VideoResolutionBucketPayload | null => {
          const name = bucket.name.trim().toLowerCase()
          if (!name) return null
          const sizes = splitConfiguredSizes(bucket.sizes)
          return sizes.length > 0 ? { name, sizes } : { name }
        })
        .filter(
          (bucket): bucket is VideoResolutionBucketPayload => bucket !== null
        )
      if (resolutionBuckets.length > 0) {
        entry.resolution_buckets = resolutionBuckets
      }
    }
    const tiers: VideoPriceTierPayload[] = []
    for (const tier of table.tiers) {
      const payloadTier: VideoPriceTierPayload = { price: Number(tier.price) }
      if (tier.mode) payloadTier.mode = tier.mode
      if (tier.resolution.trim()) {
        payloadTier.resolution = tier.resolution.trim().toLowerCase()
      }
      if (table.unit === VIDEO_UNIT_PER_IMAGE) {
        if (tier.minPixels.trim()) {
          payloadTier.min_pixels = Number(tier.minPixels)
        }
        if (tier.maxPixels.trim()) {
          payloadTier.max_pixels = Number(tier.maxPixels)
        }
      } else if (tier.audio) {
        payloadTier.audio = tier.audio
      }
      const excludedGroups = [
        ...new Set(tier.excludedGroups.map((group) => group.trim())),
      ]
        .filter(Boolean)
        .sort()
      if (excludedGroups.length > 0) {
        payloadTier.excluded_groups = excludedGroups
      }
      tiers.push(payloadTier)
    }
    if (tiers.length > 0) entry.tiers = tiers
    payload[model] = entry
  }
  return JSON.stringify(payload)
}

export type VideoPricingIssue = {
  model: string
  reason:
    | 'unit'
    | 'tier_price'
    | 'duplicate_tier'
    | 'missing_default'
    | 'input_price'
    | 'free_input_images'
    | 'input_video_price'
    | 'pixel_range'
    | 'resolution_bucket'
    | 'duplicate_bucket_size'
}

export function validateVideoPricingTables(
  tables: EditableVideoTable[]
): VideoPricingIssue[] {
  const issues: VideoPricingIssue[] = []
  for (const table of tables) {
    const model = table.model.trim()
    if (!model) continue
    if (
      table.unit !== VIDEO_UNIT_PER_SECOND &&
      table.unit !== VIDEO_UNIT_PER_MILLION_TOKENS &&
      table.unit !== VIDEO_UNIT_PER_IMAGE &&
      table.unit !== VIDEO_UNIT_PER_10K_CHARS
    ) {
      issues.push({ model, reason: 'unit' })
    }
    for (const value of [table.inputImagePrice, table.inputTokenPrice]) {
      if (value.trim() && !(Number(value) > 0)) {
        issues.push({ model, reason: 'input_price' })
      }
    }
    if (table.inputVideoPrice.trim()) {
      const price = Number(table.inputVideoPrice)
      if (!Number.isFinite(price) || price < 0) {
        issues.push({ model, reason: 'input_video_price' })
      }
    }
    if (table.freeInputImages.trim()) {
      const free = Number(table.freeInputImages)
      if (!Number.isInteger(free) || free < 0 || free > MAX_FREE_INPUT_IMAGES) {
        issues.push({ model, reason: 'free_input_images' })
      }
    }
    if (table.unit === VIDEO_UNIT_PER_IMAGE) {
      const seenBucketNames = new Set<string>()
      const sizeOwners = new Map<string, string>()
      for (const bucket of table.resolutionBuckets) {
        const name = bucket.name.trim().toLowerCase()
        if (!name || seenBucketNames.has(name)) {
          issues.push({ model, reason: 'resolution_bucket' })
        }
        seenBucketNames.add(name)
        for (const size of splitConfiguredSizes(bucket.sizes)) {
          const canonical = canonicalConfiguredSize(size)
          if (!canonical) {
            issues.push({ model, reason: 'resolution_bucket' })
            continue
          }
          const owner = sizeOwners.get(canonical)
          if (owner && owner !== name) {
            issues.push({ model, reason: 'duplicate_bucket_size' })
          } else {
            sizeOwners.set(canonical, name)
          }
        }
      }
    }
    const seen = new Set<string>()
    let hasDefault = false
    for (const tier of table.tiers) {
      const price = Number(tier.price)
      if (!Number.isFinite(price) || price <= 0) {
        issues.push({ model, reason: 'tier_price' })
      }
      const resolution = tier.resolution.trim().toLowerCase()
      const audio = table.unit === VIDEO_UNIT_PER_IMAGE ? '' : tier.audio
      const minPixels =
        table.unit === VIDEO_UNIT_PER_IMAGE ? tier.minPixels.trim() : ''
      const maxPixels =
        table.unit === VIDEO_UNIT_PER_IMAGE ? tier.maxPixels.trim() : ''
      const parsedMinPixels = Number(minPixels)
      const parsedMaxPixels = Number(maxPixels)
      const hasInvalidPixelLimit = [
        [minPixels, parsedMinPixels],
        [maxPixels, parsedMaxPixels],
      ].some(
        ([raw, value]) =>
          raw !== '' && (!Number.isSafeInteger(value) || Number(value) <= 0)
      )
      if (
        table.unit === VIDEO_UNIT_PER_IMAGE &&
        (hasInvalidPixelLimit ||
          (minPixels !== '' &&
            maxPixels !== '' &&
            parsedMinPixels > parsedMaxPixels))
      ) {
        issues.push({ model, reason: 'pixel_range' })
      }
      if (!tier.mode && !resolution && !audio && !minPixels && !maxPixels) {
        hasDefault = true
      }
      const key = `${tier.mode}|${resolution}|${audio}|${minPixels}|${maxPixels}`
      if (seen.has(key)) {
        issues.push({ model, reason: 'duplicate_tier' })
      }
      seen.add(key)
    }
    if (!hasDefault) {
      issues.push({ model, reason: 'missing_default' })
    }
  }
  return issues
}
