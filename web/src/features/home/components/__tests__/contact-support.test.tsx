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

import { ContactSupportContent } from '../default-home'

vi.mock('@/lib/lobe-icon', () => ({ getLobeIcon: () => null }))

describe('homepage contact support', () => {
  test('shows the support phone number as a dial link below the QR code', async () => {
    const i18n = i18next.createInstance()
    await i18n.use(initReactI18next).init({
      lng: 'zh',
      resources: { zh: { translation: { Phone: '电话' } } },
    })

    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <ContactSupportContent contactQr='/contact-wechat-qr.png' />
      </I18nextProvider>
    )

    assert.match(markup, /src="\/contact-wechat-qr\.png"/)
    assert.match(markup, /href="tel:15332462764"/)
    assert.match(markup, />电话: 15332462764</)
  })
})
