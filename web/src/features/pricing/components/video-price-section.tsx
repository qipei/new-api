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

import { usePricingData } from '../hooks/use-pricing-data'
import type { PricingModel, VideoPriceTier } from '../types'

// CUSTOM: 视频价格矩阵展示（fork 扩展）。
// 展示 官方价（矩阵原价）/ 平台价（× 分组倍率）/ 折扣，按生成模式分组。

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

function formatPrice(value: number): string {
  if (!Number.isFinite(value)) return '-'
  return value.toFixed(value < 1 ? 4 : 2).replace(/\.?0+$/, '')
}

export function VideoPriceSection(props: {
  model: PricingModel
  usableGroup: Record<string, { desc: string; ratio: number }>
}) {
  const { t } = useTranslation()
  const { videoPricing } = usePricingData()

  const table = videoPricing[props.model.model_name]
  if (!table || !(table.base_price > 0)) return null

  const groupRatios = Object.values(props.usableGroup)
    .map((group) => group.ratio)
    .filter((ratio) => ratio > 0)
  const bestRatio = Math.min(1, ...groupRatios)
  const hasDiscount = bestRatio < 1

  const baseTier: VideoPriceTier = { price: table.base_price }
  const tiersByMode = new Map<string, VideoPriceTier[]>()
  tiersByMode.set('', [baseTier])
  for (const tier of table.tiers ?? []) {
    const mode = tier.mode ?? ''
    const list = tiersByMode.get(mode) ?? []
    list.push(tier)
    tiersByMode.set(mode, list)
  }

  const renderRow = (tier: VideoPriceTier) => {
    const isBase = tier === baseTier
    const rowKey = `${tier.mode ?? ''}|${tier.resolution ?? ''}|${tier.audio ?? ''}|${tier.price}`
    return (
      <tr key={rowKey} className='border-border/50 border-t'>
        <td className='py-1.5 pr-3'>
          {isBase || !tier.resolution ? t('Base tier') : tier.resolution}
        </td>
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
      <div className='mb-2 text-sm font-medium'>{t('Video Price')}</div>
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
      <p className='text-muted-foreground/70 mt-2 text-xs'>
        {t(
          'Video prices share the unit of the base price configured for this model (per second or per million tokens).'
        )}
      </p>
    </section>
  )
}
