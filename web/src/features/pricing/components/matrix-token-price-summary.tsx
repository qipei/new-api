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

import {
  getMatrixTokenPriceSummary,
  getMatrixUnitPriceSummary,
} from '../lib/price'
import type { PricingModel } from '../types'

type MatrixTokenPriceSummaryProps = {
  model: PricingModel
  layout: 'card' | 'table'
  showRechargePrice?: boolean
  priceRate?: number
  usdExchangeRate?: number
  selectedGroup?: string
}

function PriceRange(props: { minimum: string; maximum?: string }) {
  return (
    <span className='text-foreground font-mono font-semibold tabular-nums'>
      {props.minimum}
      {props.maximum ? `–${props.maximum}` : null}
    </span>
  )
}

export function MatrixTokenPriceSummary(props: MatrixTokenPriceSummaryProps) {
  const { t } = useTranslation()
  const summary = getMatrixTokenPriceSummary(
    props.model,
    props.showRechargePrice,
    props.priceRate,
    props.usdExchangeRate,
    props.selectedGroup
  )

  if (!summary) {
    return (
      <span className='text-muted-foreground text-xs'>
        {t('Matrix pricing')}
      </span>
    )
  }

  if (props.layout === 'card') {
    return (
      <>
        <span className='text-muted-foreground whitespace-nowrap'>
          {t('Video generation')}{' '}
          <PriceRange minimum={summary.minimum} maximum={summary.maximum} />{' '}
          <span className='text-muted-foreground/60 text-xs'>
            / {t('1M video tokens')}
          </span>
        </span>
        {summary.prompt ? (
          <span className='text-muted-foreground whitespace-nowrap'>
            {t('Prompt')} <PriceRange minimum={summary.prompt} />{' '}
            <span className='text-muted-foreground/60 text-xs'>
              / {t('1M video tokens')}
            </span>
          </span>
        ) : null}
      </>
    )
  }

  return (
    <div className='max-w-full min-w-0'>
      <div className='text-sm'>
        <PriceRange minimum={summary.minimum} maximum={summary.maximum} />
      </div>
      <div className='text-muted-foreground/50 text-[10px]'>
        / {t('1M video tokens')}
        {summary.maximum ? ` · ${t('Tiered pricing')}` : null}
      </div>
      {summary.prompt ? (
        <div className='text-muted-foreground mt-0.5 text-[10px]'>
          {t('Prompt')} {summary.prompt} / {t('1M video tokens')}
        </div>
      ) : null}
    </div>
  )
}

const MATRIX_UNIT_LABEL_KEYS: Record<string, string> = {
  per_second: 'second',
  per_10k_chars: '10K characters',
  per_image: 'image',
}

/**
 * Unit price for per-second, per-image and per-10K-character matrix tables: the
 * cheapest tier available to the displayed group. Returns null when the model
 * is not priced that way.
 */
export function MatrixUnitPrice(props: MatrixTokenPriceSummaryProps) {
  const { t } = useTranslation()
  const summary = getMatrixUnitPriceSummary(
    props.model,
    props.showRechargePrice,
    props.priceRate,
    props.usdExchangeRate,
    props.selectedGroup
  )
  if (!summary) return null
  const unitLabel = t(MATRIX_UNIT_LABEL_KEYS[summary.unit])

  // 卡片与按次计费的价格同一种写法：价格加粗，后面跟计价单位。
  if (props.layout === 'card') {
    return (
      <span className='text-muted-foreground whitespace-nowrap'>
        <PriceRange minimum={summary.price} /> / {unitLabel}
      </span>
    )
  }

  return (
    <div className='max-w-full min-w-0'>
      <div className='text-sm'>
        <PriceRange minimum={summary.price} />
      </div>
      <div className='text-muted-foreground/50 text-[10px]'>/ {unitLabel}</div>
    </div>
  )
}
