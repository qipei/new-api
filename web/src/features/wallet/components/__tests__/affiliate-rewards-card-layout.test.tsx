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

import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'

import { AffiliateRewardsCard } from '../affiliate-rewards-card'

const i18n = createInstance()
await i18n.use(initReactI18next).init({ lng: 'en' })

describe('AffiliateRewardsCard layout', () => {
  test('places summary, statistics, and actions in separate rows', () => {
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <AffiliateRewardsCard
          user={null}
          affiliateLink='https://example.com/sign-up?aff=test'
          onTransfer={() => {}}
          onShowCommissions={() => {}}
        />
      </I18nextProvider>
    )

    const summaryIndex = markup.indexOf('data-slot="affiliate-rewards-summary"')
    const statsIndex = markup.indexOf('data-slot="affiliate-rewards-stats"')
    const actionsIndex = markup.indexOf('data-slot="affiliate-rewards-actions"')
    const qrIndex = markup.indexOf('data-slot="affiliate-rewards-qr"')
    const commissionIndex = markup.indexOf('Commission Details')
    const linkRowIndex = markup.indexOf(
      'data-slot="affiliate-rewards-link-row"'
    )

    assert.ok(summaryIndex >= 0)
    assert.ok(statsIndex > summaryIndex)
    assert.ok(commissionIndex > statsIndex)
    assert.ok(commissionIndex < actionsIndex)
    assert.ok(actionsIndex > statsIndex)
    assert.ok(linkRowIndex > actionsIndex)
    assert.equal(markup.match(/max-w-2xl/g)?.length, 2)
    assert.ok(qrIndex > actionsIndex)
    assert.ok(markup.includes('Invitation QR code'))
  })
})
