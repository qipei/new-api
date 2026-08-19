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

import { renderToStaticMarkup } from 'react-dom/server'
import { describe, test } from 'vitest'

import { HeaderLogo } from '../header-logo'

describe('HeaderLogo', () => {
  test('renders the system logo with rounded corners instead of a circle', () => {
    const markup = renderToStaticMarkup(
      <HeaderLogo
        src='/token01-arcade.gif'
        loading={false}
        logoLoaded
        className='rounded-lg'
      />
    )

    assert.match(markup, /rounded-\[6px\]/)
    assert.doesNotMatch(markup, /rounded-full/)
    assert.doesNotMatch(markup, /rounded-lg/)
  })
})
