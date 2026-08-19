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

import i18next from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { describe, test, vi } from 'vitest'

import { ProductTools } from '../default-home'

vi.mock('@/lib/lobe-icon', () => ({ getLobeIcon: () => null }))

describe('homepage product tool links', () => {
  test('opens each product landing page from its complete card', async () => {
    const i18n = i18next.createInstance()
    await i18n.use(initReactI18next).init({
      lng: 'en',
      resources: { en: { translation: {} } },
    })
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <ProductTools />
      </I18nextProvider>
    )

    assert.match(
      markup,
      /href="https:\/\/files\.token01\.net\/tools\/dj\/index\.html"/
    )
    assert.match(
      markup,
      /href="https:\/\/files\.token01\.net\/tools\/dkz\/index\.html"/
    )
    assert.equal(markup.match(/target="_blank"/g)?.length, 2)
    assert.equal(markup.match(/rel="noopener noreferrer"/g)?.length, 2)
  })
})
