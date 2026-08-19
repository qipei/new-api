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
import assert from 'node:assert/strict'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { describe, test, vi } from 'vitest'

import type { PricingData, PricingModel } from '../../types'
import { MatrixTokenPriceSummary } from '../matrix-token-price-summary'
import { ModelCard } from '../model-card'
import { ModelDetailsContent } from '../model-details'
import { VideoPriceSection } from '../video-price-section'

vi.mock('@/lib/lobe-icon', () => ({ getLobeIcon: () => null }))
vi.mock('../model-details-performance', () => ({
  ModelDetailsPerformance: () => null,
}))

const i18n = createInstance()
await i18n.use(initReactI18next).init({ lng: 'en' })

const model: PricingModel = {
  id: 1,
  model_name: 'image-matrix-model',
  quota_type: 1,
  model_ratio: 1,
  completion_ratio: 1,
  model_price: 99,
  matrix_price_unit: 'per_image',
  enable_groups: [],
}

function createPricingData(tiers: PricingData['video_pricing']): PricingData {
  return {
    success: true,
    data: [model],
    vendors: [],
    group_ratio: {},
    usable_group: {},
    supported_endpoint: {},
    auto_groups: [],
    video_pricing: tiers,
  }
}

function renderWithPricing(
  pricing: PricingData,
  content: React.ReactNode
): string {
  const queryClient = new QueryClient()
  queryClient.setQueryData(['status'], {
    price: 1,
    usd_exchange_rate: 1,
  })
  queryClient.setQueryData(['pricing'], pricing)

  return renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>{content}</I18nextProvider>
    </QueryClientProvider>
  )
}

describe('model marketplace matrix pricing', () => {
  test('shows video token pricing instead of input and output prices on matrix cards', () => {
    const tokenMatrixModel = {
      ...model,
      model_name: 'video-token-matrix-model',
      quota_type: 0,
      model_ratio: 5,
      completion_ratio: 2,
      matrix_price_unit: 'per_million_tokens',
      matrix_price_table: {
        unit: 'per_million_tokens',
        tiers: [
          { price: 999 },
          { mode: 't2v', resolution: '720p', price: 10 },
          { mode: 'i2v', resolution: '1080p', price: 20 },
        ],
        input_token_price: 2,
      },
      enable_groups: ['discount'],
      group_ratio: { discount: 0.5 },
    } satisfies PricingModel

    const markup = renderWithPricing(
      createPricingData({}),
      <ModelCard model={tokenMatrixModel} onClick={() => undefined} />
    )

    assert.match(markup, />Video generation\s/)
    assert.match(markup, />\/ 1M video tokens</)
    assert.match(markup, />Prompt\s/)
    assert.doesNotMatch(markup, />Input</)
    assert.doesNotMatch(markup, />Output</)
    assert.doesNotMatch(markup, /999/)
  })

  test('keeps regular token model input and output prices without a matrix', () => {
    const tokenModel = {
      ...model,
      model_name: 'regular-token-model',
      quota_type: 0,
      model_ratio: 5,
      completion_ratio: 2,
      matrix_price_unit: undefined,
    } satisfies PricingModel

    const markup = renderWithPricing(
      createPricingData({}),
      <ModelCard model={tokenModel} onClick={() => undefined} />
    )

    assert.match(markup, />Input\s/)
    assert.match(markup, />Output\s/)
    assert.doesNotMatch(markup, />Video generation</)
  })

  test('shows the visible matrix range and prompt price in table layout', () => {
    const tokenMatrixModel = {
      ...model,
      quota_type: 0,
      matrix_price_unit: 'per_million_tokens',
      matrix_price_table: {
        unit: 'per_million_tokens',
        tiers: [
          { price: 999 },
          { mode: 't2v', resolution: '720p', price: 10 },
          { mode: 'i2v', resolution: '1080p', price: 20 },
        ],
        input_token_price: 2,
      },
    } satisfies PricingModel

    const markup = renderWithPricing(
      createPricingData({}),
      <MatrixTokenPriceSummary model={tokenMatrixModel} layout='table' />
    )

    assert.match(markup, /\$10–\$20/)
    assert.match(markup, /Tiered pricing</)
    assert.match(markup, />Prompt \$2 \/ 1M video tokens</)
    assert.doesNotMatch(markup, /999/)
  })

  test('hides the per-request base price when an image matrix is configured', () => {
    const pricing = createPricingData({
      [model.model_name]: {
        unit: 'per_image',
        tiers: [
          { price: 9.99 },
          { mode: 't2i', max_pixels: 2610000, price: 0.3 },
          { mode: 't2i', min_pixels: 2610001, price: 0.6 },
        ],
      },
    })

    const markup = renderWithPricing(
      pricing,
      <ModelDetailsContent
        model={model}
        groupRatio={{}}
        usableGroup={{}}
        endpointMap={{}}
        autoGroups={[]}
        priceRate={1}
        usdExchangeRate={1}
        tokenUnit='M'
      />
    )

    assert.doesNotMatch(markup, />Base Price</)
    assert.doesNotMatch(markup, />Per Request</)
    assert.match(markup, />Per image</)
    assert.match(markup, />Image Price</)
  })

  test('places image input prices first, omits audio, and shows every enabled group', () => {
    const groupedModel: PricingModel = {
      ...model,
      enable_groups: ['standard', 'priority'],
    }
    const pricing = createPricingData({
      [model.model_name]: {
        unit: 'per_image',
        tiers: [
          { price: 9.99 },
          { mode: 't2i', max_pixels: 2610000, price: 0.3 },
          { mode: 't2i', min_pixels: 2610001, price: 0.6 },
        ],
        input_image_price: 0.25,
        input_token_price: 0.7,
      },
    })

    const markup = renderWithPricing(
      pricing,
      <VideoPriceSection
        model={groupedModel}
        groupRatio={{ standard: 1.5, priority: 0.5 }}
        usableGroup={{
          standard: { desc: 'Standard', ratio: 1.5 },
          priority: { desc: 'Priority', ratio: 0.5 },
        }}
        priceRate={1}
        usdExchangeRate={1}
        showRechargePrice={false}
      />
    )

    assert.match(markup, />Input image price</)
    assert.match(markup, />per input image</)
    assert.match(markup, />Input token price</)
    assert.match(markup, />per 1M tokens</)
    assert.ok(markup.indexOf('Input price') < markup.indexOf('Text to image'))
    assert.doesNotMatch(markup, />Audio track</)
    assert.match(markup, /≤2,610,000 pixels/)
    assert.match(markup, /&gt;2,610,000 pixels/)
    assert.match(markup, />Official price \/image</)
    assert.match(markup, /Priority/)
    assert.match(markup, /×0\.5/)
    assert.match(markup, /Standard/)
    assert.match(markup, /×1\.5/)
    assert.doesNotMatch(markup, /9\.99/)
  })

  test('keeps the audio column for video matrix pricing', () => {
    const pricing = createPricingData({
      [model.model_name]: {
        unit: 'per_second',
        tiers: [
          { price: 9.99 },
          {
            mode: 't2v',
            resolution: '1080p',
            audio: 'on',
            price: 0.3,
          },
        ],
      },
    })

    const markup = renderWithPricing(
      pricing,
      <VideoPriceSection
        model={model}
        groupRatio={{}}
        usableGroup={{}}
        priceRate={1}
        usdExchangeRate={1}
        showRechargePrice={false}
      />
    )

    assert.match(markup, />Audio track</)
    assert.match(markup, />With audio</)
    assert.doesNotMatch(markup, />Official price \/image</)
  })
})
