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

import { Badge } from '@/components/ui/badge'
import { formatCurrencyFromUSD } from '@/lib/currency'

import { usePricingData } from '../hooks/use-pricing-data'
import type { PricingModel, VideoPriceTier } from '../types'

// CUSTOM: 视频价格矩阵展示（fork 扩展）。
// 展示 官方价（矩阵原价）/ 平台价（× 分组倍率）/ 折扣，按生成模式分组，
// 币种跟随系统"货币显示"设置；全空维度的默认档只参与后台兜底计费，不对外展示。

const MODE_SECTION_KEYS: Array<{ mode: string; labelKey: string }> = [
  { mode: '', labelKey: 'General tiers' },
  { mode: 't2v', labelKey: 'Text to video' },
  { mode: 'i2v', labelKey: 'Image to video' },
  { mode: 'v2v', labelKey: 'Video to video' },
]

const AUDIO_LABEL_KEYS: Record<string, string> = {
  on: 'With audio',
  off: 'Without audio',
}

function isDefaultTier(tier: VideoPriceTier): boolean {
  return !tier.mode && !tier.resolution && !tier.audio
}

export function VideoPriceSection(props: {
  model: PricingModel
  usableGroup: Record<string, { desc: string; ratio: number }>
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
  const visibleTiers = table.tiers.filter((tier) => !isDefaultTier(tier))
  if (visibleTiers.length === 0) return null

  const unitLabel =
    table.unit === 'per_million_tokens' ? t('per 1M tokens') : t('per second')

  const groupRatios = Object.values(props.usableGroup)
    .map((group) => group.ratio)
    .filter((ratio) => ratio > 0)
  const bestRatio = Math.min(1, ...groupRatios)
  const hasDiscount = bestRatio < 1

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
    const rowKey = `${tier.mode ?? ''}|${tier.resolution ?? ''}|${tier.audio ?? ''}|${tier.price}`
    return (
      <tr key={rowKey} className='border-border/50 border-t'>
        <td className='py-1.5 pr-3'>{tier.resolution ? tier.resolution : '—'}</td>
        <td className='text-muted-foreground py-1.5 pr-3'>
          {tier.audio ? t(AUDIO_LABEL_KEYS[tier.audio] ?? tier.audio) : '—'}
        </td>
        <td className='py-1.5 pr-3 text-right font-mono tabular-nums'>
          {hasDiscount ? (
            <span className='text-muted-foreground line-through'>
              {formatPrice(tier.price)}
            </span>
          ) : (
            formatPrice(tier.price)
          )}
        </td>
        {hasDiscount && (
          <td className='text-foreground py-1.5 pr-3 text-right font-mono font-semibold tabular-nums'>
            {formatPrice(tier.price * bestRatio)}
          </td>
        )}
        {hasDiscount && (
          <td className='py-1.5 text-right'>
            <Badge variant='secondary' className='text-xs'>
              -{Math.round((1 - bestRatio) * 100)}%
            </Badge>
          </td>
        )}
      </tr>
    )
  }

  return (
    <section>
      <div className='mb-2 flex items-baseline gap-2'>
        <span className='text-sm font-medium'>{t('Video Price')}</span>
        <span className='text-muted-foreground text-xs'>({unitLabel})</span>
      </div>
      <div className='space-y-3'>
        {MODE_SECTION_KEYS.map((section) => {
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
              <table className='w-full min-w-[360px] text-sm'>
                <thead>
                  <tr className='text-muted-foreground text-left text-xs'>
                    <th className='py-1 pr-3 font-normal'>{t('Resolution')}</th>
                    <th className='py-1 pr-3 font-normal'>
                      {t('Audio track')}
                    </th>
                    <th className='py-1 pr-3 text-right font-normal'>
                      {t('Official price')}
                    </th>
                    {hasDiscount && (
                      <th className='py-1 pr-3 text-right font-normal'>
                        {t('Your price')}
                      </th>
                    )}
                    {hasDiscount && (
                      <th className='py-1 text-right font-normal'>
                        {t('Discount')}
                      </th>
                    )}
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
