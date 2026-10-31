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
// CUSTOM: 模型详情里按分组展示的价格（fork 扩展）。
//
// 四种计价方式（音视频和图片价格表、阶梯表达式、按 token、按次）先统一整理成
// 价格块，卡片渲染和「复制价格」读的是同一份数据，两边不会对不上。
// 价格都已乘上该分组的有效倍率（含限时活动），与后端实际扣费一致。
import type { TFunction } from 'i18next'

import { formatCurrencyFromUSD } from '@/lib/currency'

import type {
  OffPeakWindow,
  PriceType,
  PricingModel,
  TokenUnit,
  VideoPriceTable,
  VideoPriceTier,
} from '../types'
import { getDynamicPriceEntries, getDynamicPricingTiers } from './dynamic-price'
import { resolveBillingExprForGroup } from './group-billing-expr'
import { tableForGroup, tierForGroup } from './matrix-group-price'
import { isTokenBasedModel } from './model-helpers'
import { formatFixedPrice, formatGroupPrice } from './price'

export const MATRIX_MODE_SECTIONS: Array<{ mode: string; labelKey: string }> = [
  { mode: '', labelKey: 'General tiers' },
  { mode: 't2v', labelKey: 'Text to video' },
  { mode: 'i2v', labelKey: 'Image to video' },
  { mode: 'v2v', labelKey: 'Video to video' },
  { mode: 't2i', labelKey: 'Text to image' },
  { mode: 'i2i', labelKey: 'Image to image' },
]

export const MATRIX_AUDIO_LABEL_KEYS: Record<string, string> = {
  on: 'With audio',
  off: 'Without audio',
}

/** 全空维度的默认档：只参与后台兜底计费，价格表只有它一档时它就是唯一价格。 */
export function isDefaultMatrixTier(tier: VideoPriceTier): boolean {
  return (
    !tier.mode &&
    !tier.resolution &&
    !tier.audio &&
    !tier.min_pixels &&
    !tier.max_pixels
  )
}

function excludesGroup(tier: VideoPriceTier, group: string): boolean {
  return tier.excluded_groups?.includes(group) ?? false
}

/**
 * 分组卡片左侧的档位限制提示；该分组没有被任何档位排除时返回 null。
 * 与后端取价一致：某个档位排除了该分组，就不能退到更宽泛的档位使用。
 */
export function groupTierLimitNote(
  table: VideoPriceTable | undefined,
  group: string,
  t: TFunction
): string | null {
  const tiers = table?.tiers ?? []
  if (!tiers.some((tier) => excludesGroup(tier, group))) return null
  const dimensional = tiers.filter((tier) => !isDefaultMatrixTier(tier))
  if (dimensional.length === 0) {
    return t('This group does not support this model')
  }
  const dimensions = [
    ...new Set(dimensional.map((tier) => formatMatrixTierDimension(tier, t))),
  ]
  const supported = dimensions.filter((dimension) =>
    dimensional.some(
      (tier) =>
        formatMatrixTierDimension(tier, t) === dimension &&
        !excludesGroup(tier, group)
    )
  )
  if (supported.length === 0) {
    return t('This group does not support this model')
  }
  if (supported.length < dimensions.length) {
    return t('This group only supports {{items}}', {
      items: supported.join(' / '),
    })
  }
  return t('Some tiers are not available in this group')
}

function withTimeZone(range: string, tz?: string): string {
  return tz && tz !== 'Asia/Shanghai' ? `${range} ${tz}` : range
}

/** 闲时时段的展示文本，例如「00:00~07:50」；非北京时间的时段带上时区。 */
export function formatOffPeakWindows(windows: OffPeakWindow[]): string {
  return windows
    .map((window) => withTimeZone(`${window.start}~${window.end}`, window.tz))
    .join('、')
}

/**
 * 高峰时段 = 一天里闲时时段以外的时间，例如闲时 00:00~07:50 时高峰是 07:50~24:00。
 * 有多个不同的闲时时段时说不清补集，显示「其他时段」。
 */
export function formatPeakWindows(
  windows: OffPeakWindow[],
  t: TFunction
): string {
  if (windows.length !== 1) return t('Other hours')
  const [window] = windows
  const end = window.start === '00:00' ? '24:00' : window.start
  return withTimeZone(`${window.end}~${end}`, window.tz)
}

/** 模型所有分组的闲时时段（去重）；为空表示没有渠道配置闲时时段。 */
export function modelOffPeakWindows(model: PricingModel): OffPeakWindow[] {
  const seen = new Map<string, OffPeakWindow>()
  for (const windows of Object.values(model.group_off_peak ?? {})) {
    for (const window of windows) {
      seen.set(`${window.start}|${window.end}|${window.tz ?? ''}`, window)
    }
  }
  return [...seen.values()]
}

/** 档位配了有效闲时价时返回闲时价，否则返回 undefined。 */
export function tierOffPeakPrice(tier: VideoPriceTier): number | undefined {
  const price = tier.off_peak_price
  return typeof price === 'number' && Number.isFinite(price) && price > 0
    ? price
    : undefined
}

export function matrixUnitLabel(unit: string, t: TFunction): string {
  if (unit === 'per_million_tokens') return t('per 1M tokens')
  if (unit === 'per_image') return t('per image')
  if (unit === 'per_10k_chars') return t('per 10K characters')
  return t('per second')
}

export function formatMatrixTierDimension(
  tier: VideoPriceTier,
  t: TFunction
): string {
  if (tier.resolution) return tier.resolution
  const minPixels = Number(tier.min_pixels)
  const maxPixels = Number(tier.max_pixels)
  if (minPixels > 0 && maxPixels > 0) {
    return `${minPixels.toLocaleString()}–${maxPixels.toLocaleString()} ${t('pixels')}`
  }
  if (minPixels > 0) {
    return `>${(minPixels - 1).toLocaleString()} ${t('pixels')}`
  }
  if (maxPixels > 0) return `≤${maxPixels.toLocaleString()} ${t('pixels')}`
  return '—'
}

export type GroupPriceListRow = {
  label: string
  value: string
  /** 倍率为 1 时的价格，用来展示折扣前的官方价。 */
  official?: string
  /** 该行自己的计价单位（同一块里单位不一致时用）。 */
  unit?: string
}
export type GroupPriceTableRow = { label: string; cells: Array<string | null> }

export type GroupPriceBlock =
  | {
      kind: 'list'
      title?: string
      unit?: string
      rows: GroupPriceListRow[]
    }
  | {
      kind: 'table'
      title?: string
      unit?: string
      corner: string
      columns: string[]
      /** null 表示该组合不提供，界面显示「不支持」。 */
      rows: GroupPriceTableRow[]
    }
  | { kind: 'note'; text: string }

export type GroupPriceOptions = {
  model: PricingModel
  group: string
  /** 有效倍率：分组倍率 × 限时活动倍率。 */
  ratio: number
  matrixTable?: VideoPriceTable
  tokenUnit: TokenUnit
  showRechargePrice: boolean
  priceRate: number
  usdExchangeRate: number
  t: TFunction
}

const TOKEN_PRICE_TYPES: Array<{
  labelKey: string
  type: PriceType
  available: (model: PricingModel) => boolean
}> = [
  { labelKey: 'Input', type: 'input', available: () => true },
  { labelKey: 'Output', type: 'output', available: () => true },
  {
    labelKey: 'Cache',
    type: 'cache',
    available: (model) => model.cache_ratio != null,
  },
  {
    labelKey: 'Cache Write',
    type: 'create_cache',
    available: (model) => model.create_cache_ratio != null,
  },
  {
    labelKey: 'Image',
    type: 'image',
    available: (model) => model.image_ratio != null,
  },
  {
    labelKey: 'Audio In',
    type: 'audio_input',
    available: (model) => model.audio_ratio != null,
  },
  {
    labelKey: 'Audio Out',
    type: 'audio_output',
    available: (model) =>
      model.audio_ratio != null && model.audio_completion_ratio != null,
  },
]

export function buildGroupPriceBlocks(
  options: GroupPriceOptions
): GroupPriceBlock[] {
  const { model, group, ratio, t } = options
  if (options.matrixTable?.tiers?.length) {
    return buildMatrixBlocks(options.matrixTable, options)
  }

  const tokenUnitLabel =
    options.tokenUnit === 'K' ? t('per 1K tokens') : t('per 1M tokens')

  if (model.billing_mode === 'tiered_expr' && model.billing_expr) {
    // 每个分组可能有自己的表达式，必须按各自的档位算。
    const expr = resolveBillingExprForGroup(model, group)
    const tiers = getDynamicPricingTiers({ ...model, billing_expr: expr })
    if (tiers.length === 0) {
      return [
        {
          kind: 'note',
          text: t(
            'Group prices cannot be expanded because this expression is not a standard tiered pricing expression.'
          ),
        },
      ]
    }
    const entryOptions = {
      tokenUnit: options.tokenUnit,
      showRechargePrice: options.showRechargePrice,
      priceRate: options.priceRate,
      usdExchangeRate: options.usdExchangeRate,
      groupRatioMultiplier: ratio,
    }
    const entriesByTier = tiers.map((tier) =>
      getDynamicPriceEntries(tier, entryOptions)
    )
    if (tiers.length === 1) {
      const officialEntries = getDynamicPriceEntries(tiers[0], {
        ...entryOptions,
        groupRatioMultiplier: 1,
      })
      return [
        {
          kind: 'list',
          unit: tokenUnitLabel,
          rows: entriesByTier[0].map((entry) => ({
            label: t(entry.shortLabel),
            value: entry.formatted,
            official: officialEntries.find((item) => item.field === entry.field)
              ?.formatted,
          })),
        },
      ]
    }
    // 行取所有档位用到的计价项并集，某一档没有该项时显示不支持。
    const fields = new Map<string, string>()
    for (const entry of entriesByTier.flat()) {
      if (!fields.has(entry.field)) fields.set(entry.field, entry.shortLabel)
    }
    return [
      {
        kind: 'table',
        unit: tokenUnitLabel,
        corner: '',
        columns: tiers.map((tier) => tier.label || t('Default')),
        rows: [...fields].map(([field, shortLabel]) => ({
          label: t(shortLabel),
          cells: entriesByTier.map(
            (entries) =>
              entries.find((entry) => entry.field === field)?.formatted ?? null
          ),
        })),
      },
    ]
  }

  const groupRatioMap = { [group]: ratio }
  const officialRatioMap = { [group]: 1 }
  if (!isTokenBasedModel(model)) {
    const fixedPrice = (ratioMap: Record<string, number>) =>
      formatFixedPrice(
        model,
        group,
        options.showRechargePrice,
        options.priceRate,
        options.usdExchangeRate,
        ratioMap
      )
    return [
      {
        kind: 'list',
        rows: [
          {
            label: t('Per request'),
            value: fixedPrice(groupRatioMap),
            official: fixedPrice(officialRatioMap),
          },
        ],
      },
    ]
  }

  const tokenPrice = (type: PriceType, ratioMap: Record<string, number>) =>
    formatGroupPrice(
      model,
      group,
      type,
      options.tokenUnit,
      options.showRechargePrice,
      options.priceRate,
      options.usdExchangeRate,
      ratioMap
    )

  return [
    {
      kind: 'list',
      unit: tokenUnitLabel,
      rows: TOKEN_PRICE_TYPES.filter((item) => item.available(model)).map(
        (item) => ({
          label: t(item.labelKey),
          value: tokenPrice(item.type, groupRatioMap),
          official: tokenPrice(item.type, officialRatioMap),
        })
      ),
    },
  ]
}

function buildMatrixBlocks(
  matrixTable: VideoPriceTable,
  options: GroupPriceOptions
): GroupPriceBlock[] {
  const { t, ratio } = options
  // 输入图、输入视频单价可以按分组单独配置，卡片展示该分组实际适用的。
  const table = tableForGroup(matrixTable, options.group)
  const formatUSD = (usd: number, multiplier = ratio): string => {
    const value = options.showRechargePrice
      ? (usd * options.priceRate) / options.usdExchangeRate
      : usd
    return formatCurrencyFromUSD(value * multiplier, {
      digitsLarge: 4,
      digitsSmall: 4,
      abbreviate: false,
    })
  }
  const unit = matrixUnitLabel(table.unit, t)
  const tiers = (table.tiers ?? []).map((tier) =>
    tierForGroup(tier, options.group)
  )
  const dimensional = tiers.filter((tier) => !isDefaultMatrixTier(tier))
  const blocks: GroupPriceBlock[] = []
  // 本分组有渠道配了闲时时段、且价格表有闲时价时，价格分成高峰与空闲两列。
  // 没填闲时价的档位在闲时仍按原价计费，空闲列显示原价。
  const offPeakWindows = options.model.group_off_peak?.[options.group] ?? []
  const showOffPeak =
    offPeakWindows.length > 0 &&
    tiers.some(
      (tier) =>
        tierOffPeakPrice(tier) !== undefined &&
        !excludesGroup(tier, options.group)
    )
  const peakLabel = t('Peak price ({{window}})', {
    window: formatPeakWindows(offPeakWindows, t),
  })
  const offPeakLabel = t('Off-peak price ({{window}})', {
    window: formatOffPeakWindows(offPeakWindows),
  })

  if (dimensional.length === 0) {
    // 只有默认档的价格表（音频按秒、按字等），默认档就是唯一价格。
    const flat = tiers.find(isDefaultMatrixTier)
    if (flat && excludesGroup(flat, options.group)) {
      blocks.push({
        kind: 'list',
        unit,
        rows: [{ label: t('Unit price'), value: t('Not supported') }],
      })
    } else if (flat && showOffPeak) {
      const offPeakPrice = tierOffPeakPrice(flat) ?? flat.price
      blocks.push({
        kind: 'list',
        unit,
        rows: [
          {
            label: peakLabel,
            value: formatUSD(flat.price),
            official: formatUSD(flat.price, 1),
          },
          {
            label: offPeakLabel,
            value: formatUSD(offPeakPrice),
            official: formatUSD(offPeakPrice, 1),
          },
        ],
      })
    } else if (flat) {
      blocks.push({
        kind: 'list',
        unit,
        rows: [
          {
            label: t('Unit price'),
            value: formatUSD(flat.price),
            official: formatUSD(flat.price, 1),
          },
        ],
      })
    }
  }

  const modeCount = new Set(dimensional.map((tier) => tier.mode ?? '')).size
  for (const section of MATRIX_MODE_SECTIONS) {
    const modeTiers = dimensional.filter(
      (tier) => (tier.mode ?? '') === section.mode
    )
    if (modeTiers.length === 0) continue

    // 有声/无声转成列，同一分辨率的两种价格放在一行里对比。不区分音轨的档位
    // 是通配：后端按「有指定音轨的档位优先，否则命中不区分音轨的档位」取价。
    const audioColumns = modeTiers.some((tier) => tier.audio)
      ? ['on', 'off']
      : ['']
    const periods = showOffPeak
      ? (['peak', 'off_peak'] as const)
      : ([''] as const)
    const columns = audioColumns.flatMap((audio) =>
      periods.map((period) => ({ audio, period }))
    )
    const dimensions = [
      ...new Set(modeTiers.map((tier) => formatMatrixTierDimension(tier, t))),
    ]
    let corner = t('Resolution')
    if (table.unit === 'per_image') corner = t('Resolution / pixels')

    blocks.push({
      kind: 'table',
      title: modeCount > 1 || section.mode ? t(section.labelKey) : undefined,
      unit,
      corner,
      columns: columns.map(({ audio, period }) => {
        let label = t('Price')
        if (period === 'peak') label = peakLabel
        if (period === 'off_peak') label = offPeakLabel
        if (!audio) return label
        const audioLabel = t(MATRIX_AUDIO_LABEL_KEYS[audio])
        return period ? `${audioLabel} · ${label}` : audioLabel
      }),
      rows: dimensions.map((dimension) => ({
        label: dimension,
        cells: columns.map(({ audio, period }) => {
          const sameDimension = modeTiers.filter(
            (item) => formatMatrixTierDimension(item, t) === dimension
          )
          const tier =
            sameDimension.find((item) => (item.audio ?? '') === audio) ??
            sameDimension.find((item) => !item.audio)
          // 命中的档位排除了该分组时，后端直接拒绝，不会退到更宽泛的档位。
          if (!tier || excludesGroup(tier, options.group)) return null
          if (period === 'off_peak') {
            return formatUSD(tierOffPeakPrice(tier) ?? tier.price)
          }
          return formatUSD(tier.price)
        }),
      })),
    })
  }

  const inputRows: GroupPriceListRow[] = []
  const inputImagePrice = Number(table.input_image_price)
  if (inputImagePrice > 0) {
    inputRows.push({
      label: t('Input image price'),
      unit:
        Number(table.free_input_images) > 0
          ? t('per input image, first {{free}} free', {
              free: Number(table.free_input_images),
            })
          : t('per input image'),
      value: formatUSD(inputImagePrice),
      official: formatUSD(inputImagePrice, 1),
    })
  }
  // 输入视频单价只在单独收费时展示：没配置时与输出同价，配置为 0 时不收费，都不另列一行
  // （与输入图单价为 0 时一致）。
  const inputVideoPrice = Number(table.input_video_price)
  if (inputVideoPrice > 0) {
    inputRows.push({
      label: t('Input video price'),
      unit: t('per second of input video'),
      value: formatUSD(inputVideoPrice),
      official: formatUSD(inputVideoPrice, 1),
    })
  }
  const inputTokenPrice = Number(table.input_token_price)
  if (inputTokenPrice > 0) {
    inputRows.push({
      label: t('Input token price'),
      unit: t('per 1M tokens'),
      value: formatUSD(inputTokenPrice),
      official: formatUSD(inputTokenPrice, 1),
    })
  }
  if (inputRows.length > 0) {
    blocks.push({ kind: 'list', title: t('Input price'), rows: inputRows })
  }
  // 最低计费时长：上游单次有最低收费时，短请求按这个时长计费。
  if (table.unit === 'per_second' && Number(table.min_seconds) > 0) {
    blocks.push({
      kind: 'list',
      rows: [
        {
          label: t('Minimum billable duration'),
          value: t('{{seconds}} seconds per request', {
            seconds: Number(table.min_seconds),
          }),
        },
      ],
    })
  }
  return blocks
}

/** 「复制价格」的纯文本：首行是分组，后面逐块列出价格。 */
export function groupPriceText(
  heading: string,
  blocks: GroupPriceBlock[],
  t: TFunction
): string {
  const lines = [heading]
  for (const block of blocks) {
    if (block.kind === 'note') {
      lines.push(block.text)
      continue
    }
    const header = [block.title, block.unit].filter(Boolean).join(' · ')
    if (header) lines.push(`[${header}]`)
    if (block.kind === 'list') {
      for (const row of block.rows) {
        const unit = row.unit ? ` ${row.unit}` : ''
        const official =
          row.official && row.official !== row.value
            ? ` (${t('Official price')} ${row.official})`
            : ''
        lines.push(`${row.label}: ${row.value}${unit}${official}`)
      }
      continue
    }
    for (const row of block.rows) {
      const cells = row.cells.map(
        (cell, index) => `${block.columns[index]} ${cell ?? t('Not supported')}`
      )
      lines.push(`${row.label}: ${cells.join(', ')}`)
    }
  }
  return lines.join('\n')
}
