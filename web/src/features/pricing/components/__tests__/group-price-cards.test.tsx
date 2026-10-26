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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from '@tanstack/react-router'
import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { describe, expect, test } from 'vitest'

import type { PerformanceMetricsData } from '@/features/performance-metrics/types'

import type { PricingModel, VideoPriceTable } from '../../types'
import { GroupPriceCards } from '../group-price-cards'

const i18n = createInstance()
// 与 src/i18n/config.ts 一致：键里允许出现冒号
await i18n.use(initReactI18next).init({ lng: 'en', nsSeparator: false })

const router = createRouter({
  routeTree: createRootRoute(),
  history: createMemoryHistory(),
})

/** 覆盖当天的活动，避免测试随日期失效。 */
const runningPromotion = {
  name: 'Flash sale',
  start: '2000-01-01',
  end: '2099-12-31',
  ratio: 0.5,
  groups: ['vip'],
}

function baseModel(overrides: Partial<PricingModel> = {}): PricingModel {
  return {
    id: 1,
    model_name: 'group-card-model',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 2,
    model_price: 0,
    enable_groups: ['default', 'vip'],
    group_ratio: { default: 1, vip: 0.8 },
    ...overrides,
  } as PricingModel
}

function render(
  model: PricingModel,
  options: { matrixTable?: VideoPriceTable; perf?: PerformanceMetricsData } = {}
): string {
  const queryClient = new QueryClient()
  queryClient.setQueryData(['status'], { price: 1, usd_exchange_rate: 1 })
  if (options.perf) {
    queryClient.setQueryData(['perf-metrics', model.model_name], options.perf)
  }
  return renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <RouterContextProvider router={router}>
        <I18nextProvider i18n={i18n}>
          <GroupPriceCards
            model={model}
            groupRatio={model.group_ratio ?? {}}
            usableGroup={{
              default: { desc: 'Default group', ratio: 1 },
              vip: { desc: 'VIP', ratio: 0.8 },
            }}
            matrixTable={options.matrixTable}
            priceRate={1}
            usdExchangeRate={1}
            tokenUnit='M'
            showRechargePrice={false}
          />
        </I18nextProvider>
      </RouterContextProvider>
    </QueryClientProvider>
  )
}

/** 取某个分组卡片的片段：从分组名出现处到下一张卡片之前。 */
function cardOf(markup: string, group: string): string {
  const start = markup.indexOf(`title="${group}"`)
  expect(start).toBeGreaterThan(-1)
  const next = markup.indexOf('Group:', start)
  return markup.slice(start, next === -1 ? undefined : next)
}

describe('GroupPriceCards', () => {
  test('matrix prices apply group ratio and running promotion, pivot audio into columns', () => {
    const markup = render(baseModel({ promotions: [runningPromotion] }), {
      matrixTable: {
        unit: 'per_second',
        tiers: [
          { price: 9.99 },
          { mode: 't2v', resolution: '720p', audio: 'on', price: 0.2 },
          { mode: 't2v', resolution: '720p', audio: 'off', price: 0.1 },
          { mode: 't2v', resolution: '1080p', audio: 'on', price: 0.4 },
          // 不区分音轨的档位是通配，有声、无声两列都命中它
          { mode: 't2v', resolution: '4k', price: 2 },
        ],
        input_image_price: 0.02,
      },
    })

    const vip = cardOf(markup, 'vip')
    // vip: 0.8 × 活动 0.5 = 0.4
    expect(vip).toContain('×0.4')
    expect(vip).toMatch(/line-through[^>]*>×0\.8</)
    expect(vip).toContain('Flash sale')
    expect(vip).toContain('Save 60% vs official')
    expect(vip).toContain('$0.08')
    expect(vip).toContain('$0.04')
    expect(vip).toContain('$0.16')
    expect(vip).toContain('$0.008')
    expect(vip).toContain('Not supported')
    expect(vip.split('$0.8<').length - 1).toBe(2)
    expect(vip).toContain('With audio')
    expect(vip).toContain('Without audio')

    const defaultCard = cardOf(markup, 'default')
    expect(defaultCard).toContain('×1')
    expect(defaultCard).toContain('$0.2')
    expect(defaultCard).not.toContain('Flash sale')
    expect(markup).not.toContain('$9.99')
  })

  // 免费张数随输入图单价一起显示；没配置时只写「每张输入图」，不暗示有免费额度。
  test('input image price shows the configured free images', () => {
    const tiers = [{ price: 0.5 }]
    const withFree = render(baseModel(), {
      matrixTable: {
        unit: 'per_second',
        tiers,
        input_image_price: 0.2,
        free_input_images: 5,
      },
    })
    expect(cardOf(withFree, 'default')).toContain(
      'per input image, first 5 free'
    )

    const withoutFree = render(baseModel(), {
      matrixTable: { unit: 'per_second', tiers, input_image_price: 0.2 },
    })
    expect(cardOf(withoutFree, 'default')).toContain('per input image')
    expect(withoutFree).not.toContain('free')
  })

  test('a matrix with only the default tier shows it as the single unit price', () => {
    const markup = render(baseModel({ quota_type: 1 }), {
      matrixTable: { unit: 'per_10k_chars', tiers: [{ price: 3 }] },
    })

    const vip = cardOf(markup, 'vip')
    expect(vip).toContain('per 10K characters')
    expect(vip).toContain('Unit price')
    expect(vip).toContain('$2.4')
    expect(cardOf(markup, 'default')).toContain('$3')
  })

  // 档位排除了某分组：该分组显示「不支持」并提示可用档位，不退到更宽泛的档位；其他分组照常。
  test('excluded tiers show as not supported only for the excluded group', () => {
    const markup = render(baseModel(), {
      matrixTable: {
        unit: 'per_second',
        tiers: [
          { price: 9.99 },
          { mode: 't2v', resolution: '768p', price: 0.1 },
          {
            mode: 't2v',
            resolution: '1080p',
            price: 0.2,
            excluded_groups: ['default'],
          },
        ],
      },
    })

    const defaultCard = cardOf(markup, 'default')
    expect(defaultCard).toContain('This group only supports 768p')
    expect(defaultCard).toContain('Not supported')
    expect(defaultCard).not.toContain('$0.2<')

    const vip = cardOf(markup, 'vip')
    expect(vip).not.toContain('only supports')
    expect(vip).not.toContain('Not supported')
    expect(vip).toContain('$0.16')
  })

  test('token prices list input, output and cache for each group', () => {
    const markup = render(
      baseModel({ model_ratio: 1.5, completion_ratio: 4, cache_ratio: 0.1 })
    )

    // model_ratio 1.5 → $3 / 1M 输入；vip 0.8 → $2.4，输出 ×4 → $9.6，缓存 ×0.1 → $0.24
    const vip = cardOf(markup, 'vip')
    expect(vip).toContain('per 1M tokens')
    // 折扣前的官方价划线展示，并标出省多少
    expect(vip).toMatch(/line-through">\$3</)
    expect(vip).toContain('Save 20% vs official')
    expect(cardOf(markup, 'default')).not.toContain('vs official')
    expect(vip).toContain('$2.4')
    expect(vip).toContain('$9.6')
    expect(vip).toContain('$0.24')
    expect(cardOf(markup, 'default')).toContain('$3')
  })

  test('tiered expressions render one column per tier with each group expression', () => {
    const markup = render(
      baseModel({
        billing_mode: 'tiered_expr',
        billing_expr:
          'len <= 200000 ? tier("short", p * 2 + c * 8) : tier("long", p * 4 + c * 12)',
        group_billing_expr: { vip: 'tier("flat", p * 1 + c * 2)' },
      })
    )

    const defaultCard = cardOf(markup, 'default')
    expect(defaultCard).toContain('short')
    expect(defaultCard).toContain('long')
    expect(defaultCard).toContain('$12')

    // vip 用自己的单档表达式，按列表展示，价格再乘 0.8
    const vip = cardOf(markup, 'vip')
    expect(vip).not.toContain('short')
    expect(vip).toContain('$0.8')
    expect(vip).toContain('$1.6')
  })

  test('per-request models show one price per group', () => {
    const markup = render(baseModel({ quota_type: 1, model_price: 0.5 }))
    expect(cardOf(markup, 'vip')).toContain('$0.4')
    expect(cardOf(markup, 'default')).toContain('$0.5')
    expect(markup).toContain('Per request')
  })

  test('status follows the 24h success rate and recent latency uses the last hour', () => {
    const now = Math.floor(Date.now() / 1000)
    const bucket = now - (now % 3600)
    const markup = render(baseModel(), {
      perf: {
        success: true,
        data: {
          model_name: 'group-card-model',
          groups: [
            {
              group: 'vip',
              avg_ttft_ms: 0,
              avg_latency_ms: 300_000,
              success_rate: 85.7,
              avg_tps: 0,
              series: [
                {
                  ts: bucket - 7200,
                  avg_ttft_ms: 0,
                  avg_latency_ms: 999_000,
                  success_rate: 50,
                  avg_tps: 0,
                },
                {
                  ts: bucket,
                  avg_ttft_ms: 0,
                  avg_latency_ms: 412_000,
                  success_rate: 100,
                  avg_tps: 0,
                },
              ],
            },
          ],
        },
      },
    })

    const vip = cardOf(markup, 'vip')
    expect(vip).toContain('Busy')
    expect(vip).toContain('85.7%')
    expect(vip).toContain('6m 52s')
    expect(vip).toContain('create_group=vip')

    const defaultCard = cardOf(markup, 'default')
    expect(defaultCard).toContain('No data')
    expect(markup).toContain('1 Busy')
    expect(markup).toContain('1 No data')
  })
})
