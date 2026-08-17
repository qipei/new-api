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

import { renderToStaticMarkup } from 'react-dom/server'

import { PublicBrand } from '../public-brand'

describe('PublicBrand', () => {
  test('uses the designed stacked lockup before the header is scrolled', () => {
    const markup = renderToStaticMarkup(
      <PublicBrand
        name='token01.net'
        logo='/token01-arcade.gif'
        loading={false}
        logoLoaded
        compact={false}
        subtitle='豆比特'
      />
    )

    assert.match(markup, /size-10/)
    assert.match(markup, /rounded-\[10px\]/)
    assert.match(markup, />token<span/)
    assert.match(markup, /text-\[#e2a600\]">01/)
    assert.match(markup, /<\/span>\.net/)
    assert.match(markup, /豆比特/)
    assert.match(markup, /max-h-4/)
  })

  test('switches to the Chinese brand name when the header is compact', () => {
    const markup = renderToStaticMarkup(
      <PublicBrand
        name='token01.net'
        logo='/token01-arcade.gif'
        loading={false}
        logoLoaded
        compact
        subtitle='豆比特'
      />
    )

    assert.match(markup, /size-8/)
    assert.match(markup, /max-h-0/)
    assert.doesNotMatch(markup, />token<span/)
    assert.match(markup, /豆比特/)
  })
})
