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
import { describe, test } from 'node:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'

import { IMAGE_RESOLUTION_TEMPLATES } from '../image-resolution-templates'
import { VideoPricingSection } from '../video-pricing-section'

const i18n = createInstance()
await i18n.use(initReactI18next).init({ lng: 'en' })

describe('VideoPricingSection', () => {
  test('offers visual and JSON editors for the same pricing configuration', () => {
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={new QueryClient()}>
        <I18nextProvider i18n={i18n}>
          <VideoPricingSection defaultValue='{}' />
        </I18nextProvider>
      </QueryClientProvider>
    )

    assert.match(markup, /data-slot="tabs-list"/)
    assert.match(markup, />Visual</)
    assert.match(markup, />JSON</)
  })

  test('shows pixel limits instead of audio controls for image pricing', () => {
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={new QueryClient()}>
        <I18nextProvider i18n={i18n}>
          <VideoPricingSection
            defaultValue={JSON.stringify({
              image: {
                unit: 'per_image',
                tiers: [
                  { price: 0.6 },
                  { max_pixels: 2610000, price: 0.3 },
                  { min_pixels: 2610001, price: 0.6 },
                ],
              },
            })}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    assert.match(markup, />Minimum pixels</)
    assert.match(markup, />Maximum pixels</)
    assert.doesNotMatch(markup, />Audio track</)
  })

  test('offers Alibaba templates and enables clearing configured resolution buckets', () => {
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={new QueryClient()}>
        <I18nextProvider i18n={i18n}>
          <VideoPricingSection
            defaultValue={JSON.stringify({
              image: {
                unit: 'per_image',
                tiers: [{ price: 0.3 }],
                resolution_buckets: [{ name: '1k', sizes: ['1024x1024'] }],
              },
            })}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    assert.match(markup, />Resolution bucket definitions</)
    assert.match(markup, />Apply resolution template</)
    assert.match(
      markup,
      /aria-label="image Clear Resolution bucket definitions"/
    )
    assert.doesNotMatch(
      markup,
      /disabled=""[^>]*aria-label="image Clear Resolution bucket definitions"/
    )
    assert.deepEqual(
      IMAGE_RESOLUTION_TEMPLATES['bailian-vidu-image'].buckets.map(
        (bucket) => bucket.name
      ),
      ['1k', '2k', '4k']
    )
    assert.equal(
      IMAGE_RESOLUTION_TEMPLATES['bailian-kling-v3-omni'].buckets.length,
      3
    )
  })
})
