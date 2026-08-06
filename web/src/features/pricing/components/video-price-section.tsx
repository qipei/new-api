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
  getAvailableGroups,
  getConfiguredGroupRatio,
} from '../lib/model-helpers'
import type { PricingModel, VideoPriceTier } from '../types'

// CUSTOM: 视频价格矩阵展示（fork 扩展）。
// 按生成模式分组展示各档官方价，并按模型可用的每个分组展示折后价；
// 币种跟随系统"货币显示"设置；全空维度的默认档只参与后台兜底计费，不对外展示。

const MODE_SECTION_KEYS: Array<{ mode: string; labelKey: string }> = [
  { mode: '', labelKey: 'General tiers' },
  { mode: 't2v', labelKey: 'Text to video' },
  { mode: 'i2v', labelKey: 'Image to video' },
  { mode: 'v2v', labelKey: 'Video to video' },
  { mode: 't2i', labelKey: 'Text to image' },
  { mode: 'i2i', labelKey: 'Image to image' },
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
  groupRatio: Record<string, number>
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

  let unitLabel = t('per second')
  if (table.unit === 'per_million_tokens') unitLabel = t('per 1M tokens')
  if (table.unit === 'per_image') unitLabel = t('per image')
  const sectionTitleKey =
    table.unit === 'per_image' ? 'Image Price' : 'Video Price'

  // 多分组时每个分组一列，按倍率从低到高（折扣从优到差）排序，同倍率按名称稳定排序
  const groups = getAvailableGroups(props.model, props.usableGroup || {})
    .map((group) => ({
      key: group,
      label: props.usableGroup[group]?.desc || group,
      ratio: getConfiguredGroupRatio(props.groupRatio, group),
    }))
    .filter((group) => Number.isFinite(group.ratio))
    .sort((a, b) => a.ratio - b.ratio || a.key.localeCompare(b.key))

  const formatRatio = (ratio: number): string =>
    Number(ratio.toFixed(4)).toString()

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
        <td className='py-1.5 pr-3'>
          {tier.resolution ? tier.resolution : '—'}
        </td>
        <td className='text-muted-foreground py-1.5 pr-3'>
          {tier.audio ? t(AUDIO_LABEL_KEYS[tier.audio] ?? tier.audio) : '—'}
        </td>
        <td
          className={
            groups.length > 0
              ? 'text-muted-foreground py-1.5 pr-3 text-right font-mono tabular-nums'
              : 'py-1.5 pr-3 text-right font-mono tabular-nums'
          }
        >
          {groups.length > 0 ? (
            <span className='line-through'>{formatPrice(tier.price)}</span>
          ) : (
            formatPrice(tier.price)
          )}
        </td>
        {groups.map((group) => (
          <td
            key={group.key}
            className='text-foreground py-1.5 pl-3 text-right font-mono font-semibold tabular-nums'
          >
            {formatPrice(tier.price * group.ratio)}
          </td>
        ))}
      </tr>
    )
  }

  return (
    <section>
      <div className='mb-2 flex items-baseline gap-2'>
        <span className='text-sm font-medium'>{t(sectionTitleKey)}</span>
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
                    {groups.map((group) => (
                      <th
                        key={group.key}
                        className='py-1 pl-3 text-right font-normal whitespace-nowrap'
                      >
                        {group.label}
                        <span className='text-muted-foreground/60 ml-1'>
                          ×{formatRatio(group.ratio)}
                        </span>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>{tiers.map(renderRow)}</tbody>
              </table>
            </div>
          )
        })}
        {(Number(table.input_image_price) > 0 ||
          Number(table.input_token_price) > 0) && (
          <div className='bg-muted/20 overflow-x-auto rounded-lg border px-3 py-2.5'>
            <div className='text-muted-foreground mb-1 text-xs font-medium'>
              {t('Input price')}
            </div>
            <table className='w-full min-w-[360px] text-sm'>
              <tbody>
                {Number(table.input_image_price) > 0 && (
                  <tr className='border-border/50 border-t'>
                    <td className='py-1.5 pr-3'>{t('Input image price')}</td>
                    <td className='text-muted-foreground py-1.5 pr-3'>
                      {t('per input image')}
                    </td>
                    <td
                      className={
                        groups.length > 0
                          ? 'text-muted-foreground py-1.5 pr-3 text-right font-mono tabular-nums'
                          : 'py-1.5 pr-3 text-right font-mono tabular-nums'
                      }
                    >
                      {groups.length > 0 ? (
                        <span className='line-through'>
                          {formatPrice(Number(table.input_image_price))}
                        </span>
                      ) : (
                        formatPrice(Number(table.input_image_price))
                      )}
                    </td>
                    {groups.map((group) => (
                      <td
                        key={group.key}
                        className='text-foreground py-1.5 pl-3 text-right font-mono font-semibold tabular-nums'
                      >
                        {formatPrice(
                          Number(table.input_image_price) * group.ratio
                        )}
                      </td>
                    ))}
                  </tr>
                )}
                {Number(table.input_token_price) > 0 && (
                  <tr className='border-border/50 border-t'>
                    <td className='py-1.5 pr-3'>{t('Input token price')}</td>
                    <td className='text-muted-foreground py-1.5 pr-3'>
                      {t('per 1M tokens')}
                    </td>
                    <td
                      className={
                        groups.length > 0
                          ? 'text-muted-foreground py-1.5 pr-3 text-right font-mono tabular-nums'
                          : 'py-1.5 pr-3 text-right font-mono tabular-nums'
                      }
                    >
                      {groups.length > 0 ? (
                        <span className='line-through'>
                          {formatPrice(Number(table.input_token_price))}
                        </span>
                      ) : (
                        formatPrice(Number(table.input_token_price))
                      )}
                    </td>
                    {groups.map((group) => (
                      <td
                        key={group.key}
                        className='text-foreground py-1.5 pl-3 text-right font-mono font-semibold tabular-nums'
                      >
                        {formatPrice(
                          Number(table.input_token_price) * group.ratio
                        )}
                      </td>
                    ))}
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </section>
  )
}
