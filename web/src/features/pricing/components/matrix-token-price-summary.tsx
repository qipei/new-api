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

import { getMatrixTokenPriceSummary } from '../lib/price'
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
