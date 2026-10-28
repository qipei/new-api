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
import { useTranslation } from 'react-i18next'

import { formatCurrencyFromUSD } from '@/lib/currency'

import { usePricingData } from '../hooks/use-pricing-data'
import {
  MATRIX_AUDIO_LABEL_KEYS,
  MATRIX_MODE_SECTIONS,
  formatMatrixTierDimension,
  isDefaultMatrixTier,
  matrixUnitLabel,
} from '../lib/group-price-blocks'
import type { PricingModel, VideoPriceTier } from '../types'

// CUSTOM: 音视频和图片价格表的官方价（fork 扩展）。
// 按生成模式分组展示各档官方价，各分组折算后的价格在下方的分组卡片里；
// 币种跟随系统"货币显示"设置；全空维度的默认档只参与后台兜底计费，不对外展示。
// 例外是只有默认档的价格表（例如音频的按秒、按字计价）：默认档就是唯一价格，
// 按计价单位展示一行。

export function VideoPriceSection(props: {
  model: PricingModel
  priceRate: number
  usdExchangeRate: number
  showRechargePrice: boolean
}) {
  const { t } = useTranslation()
  const { videoPricing } = usePricingData()

  const table = videoPricing[props.model.model_name]
  if (!table || !Array.isArray(table.tiers) || table.tiers.length === 0) {
    return null
  }
  const dimensionalTiers = table.tiers.filter(
    (tier) => !isDefaultMatrixTier(tier)
  )
  const isFlatTable = dimensionalTiers.length === 0
  const visibleTiers = isFlatTable
    ? table.tiers.filter(isDefaultMatrixTier).slice(0, 1)
    : dimensionalTiers
  const hasInputImagePrice = Number(table.input_image_price) > 0
  const hasInputTokenPrice = Number(table.input_token_price) > 0
  // 输入视频单价只在配置了时展示（0 为免费）：没配置时与输出同价，不另列一行。
  const hasInputVideoPrice =
    typeof table.input_video_price === 'number' && table.input_video_price >= 0
  if (
    visibleTiers.length === 0 &&
    !hasInputImagePrice &&
    !hasInputTokenPrice &&
    !hasInputVideoPrice
  ) {
    return null
  }

  const unitLabel = matrixUnitLabel(table.unit, t)
  let sectionTitleKey = 'Video Price'
  if (table.unit === 'per_image') sectionTitleKey = 'Image Price'
  if (isFlatTable) sectionTitleKey = 'Unit price'
  const showAudioColumn = table.unit !== 'per_image' && !isFlatTable
  let dimensionHeaderKey = 'Resolution'
  if (table.unit === 'per_image') dimensionHeaderKey = 'Resolution / pixels'
  if (isFlatTable) dimensionHeaderKey = 'Billing unit'

  const formatPrice = (usd: number): string => {
    if (!Number.isFinite(usd)) return '-'
    const value = props.showRechargePrice
      ? (usd * props.priceRate) / props.usdExchangeRate
      : usd
    return formatCurrencyFromUSD(value, {
      digitsLarge: 4,
      digitsSmall: 4,
      abbreviate: false,
    })
  }

  const tiersByMode = new Map<string, VideoPriceTier[]>()
  for (const tier of visibleTiers) {
    const mode = tier.mode ?? ''
    const list = tiersByMode.get(mode) ?? []
    list.push(tier)
    tiersByMode.set(mode, list)
  }

  const renderRow = (tier: VideoPriceTier) => {
    const rowKey = `${tier.mode ?? ''}|${tier.resolution ?? ''}|${tier.audio ?? ''}|${tier.min_pixels ?? ''}|${tier.max_pixels ?? ''}|${tier.price}`
    return (
      <tr key={rowKey} className='border-border/50 border-t'>
        <td className='py-1.5 pr-3'>
          {isFlatTable ? unitLabel : formatMatrixTierDimension(tier, t)}
        </td>
        {showAudioColumn ? (
          <td className='text-muted-foreground py-1.5 pr-3'>
            {tier.audio
              ? t(MATRIX_AUDIO_LABEL_KEYS[tier.audio] ?? tier.audio)
              : '—'}
          </td>
        ) : null}
        <td className='py-1.5 text-right font-mono tabular-nums'>
          {formatPrice(tier.price)}
        </td>
      </tr>
    )
  }

  const inputRows = [
    hasInputImagePrice
      ? {
          key: 'image',
          label: t('Input image price'),
          unit:
            Number(table.free_input_images) > 0
              ? t('per input image, first {{free}} free', {
                  free: Number(table.free_input_images),
                })
              : t('per input image'),
          price: Number(table.input_image_price),
        }
      : null,
    hasInputVideoPrice
      ? {
          key: 'video',
          label: t('Input video price'),
          unit: t('per second of input video'),
          price: Number(table.input_video_price),
        }
      : null,
    hasInputTokenPrice
      ? {
          key: 'token',
          label: t('Input token price'),
          unit: t('per 1M tokens'),
          price: Number(table.input_token_price),
        }
      : null,
  ].filter((row) => row !== null)

  return (
    <section>
      <div className='mb-2 flex items-baseline gap-2'>
        <span className='text-sm font-medium'>{t(sectionTitleKey)}</span>
        <span className='text-muted-foreground text-xs'>
          ({t('Official price')} · {unitLabel}
          {table.unit === 'per_second' && Number(table.min_seconds) > 0
            ? ` · ${t('Minimum {{seconds}} seconds per request', { seconds: Number(table.min_seconds) })}`
            : ''}
          )
        </span>
      </div>
      <div className='space-y-3'>
        {inputRows.length > 0 ? (
          <div className='bg-muted/20 overflow-x-auto rounded-lg border px-3 py-2.5'>
            <div className='text-muted-foreground mb-1 text-xs font-medium'>
              {t('Input price')}
            </div>
            <table className='w-full min-w-[280px] text-sm'>
              <tbody>
                {inputRows.map((row) => (
                  <tr key={row.key} className='border-border/50 border-t'>
                    <td className='py-1.5 pr-3'>{row.label}</td>
                    <td className='text-muted-foreground py-1.5 pr-3'>
                      {row.unit}
                    </td>
                    <td className='py-1.5 text-right font-mono tabular-nums'>
                      {row.price === 0
                        ? t('Free of charge')
                        : formatPrice(row.price)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        {MATRIX_MODE_SECTIONS.map((section) => {
          const tiers = tiersByMode.get(section.mode)
          if (!tiers || tiers.length === 0) return null
          return (
            <div
              key={section.mode || 'general'}
              className='bg-muted/20 overflow-x-auto rounded-lg border px-3 py-2.5'
            >
              <div className='text-muted-foreground mb-1 text-xs font-medium'>
                {t(section.labelKey)}
              </div>
              <table className='w-full min-w-[280px] text-sm'>
                <thead>
                  <tr className='text-muted-foreground text-left text-xs'>
                    <th className='py-1 pr-3 font-normal'>
                      {t(dimensionHeaderKey)}
                    </th>
                    {showAudioColumn ? (
                      <th className='py-1 pr-3 font-normal'>
                        {t('Audio track')}
                      </th>
                    ) : null}
                    <th className='py-1 text-right font-normal'>
                      {t('Official price')}
                      {table.unit === 'per_image' ? ` ${t('/image')}` : null}
                    </th>
                  </tr>
                </thead>
                <tbody>{tiers.map(renderRow)}</tbody>
              </table>
            </div>
          )
        })}
      </div>
    </section>
  )
}
