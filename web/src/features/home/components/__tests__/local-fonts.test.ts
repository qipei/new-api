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
import fs from 'node:fs/promises'
import path from 'node:path'

import { describe, test } from 'vitest'

describe('homepage local fonts', () => {
  test('bundles the designed fonts without Google Fonts requests', async () => {
    const [indexHtml, styles] = await Promise.all([
      fs.readFile(path.resolve(process.cwd(), 'index.html'), 'utf8'),
      fs.readFile(path.resolve(process.cwd(), 'src/styles/index.css'), 'utf8'),
    ])

    assert.doesNotMatch(indexHtml, /fonts\.googleapis\.com/)
    assert.doesNotMatch(indexHtml, /fonts\.gstatic\.com/)
    assert.match(styles, /@fontsource-variable\/noto-sans-sc/)
    assert.match(styles, /@fontsource\/ibm-plex-mono\/latin-400\.css/)
    assert.match(styles, /@fontsource\/ibm-plex-mono\/latin-500\.css/)
    assert.match(styles, /@fontsource\/ibm-plex-mono\/latin-600\.css/)
  })
})
