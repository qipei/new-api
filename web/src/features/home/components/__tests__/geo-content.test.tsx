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
import { describe, test } from 'node:test'

import { SITE_SEO_TITLE } from '@/lib/site-seo'

describe('homepage GEO content', () => {
  test('keeps FAQ schema without adding a visible FAQ section to initial HTML', async () => {
    const indexHtml = await fs.readFile(
      new URL('../../../../../index.html', import.meta.url),
      'utf8'
    )

    const title = indexHtml
      .match(/<title>([\s\S]*?)<\/title>/)?.[1]
      .replaceAll(/\s+/g, ' ')
      .trim()
    assert.equal(title, SITE_SEO_TITLE)
    assert.match(indexHtml, /name="keywords"/)
    assert.match(indexHtml, /seedance,minimax,kimi,qwen,千问,codex/)
    assert.match(indexHtml, /"@type": "Organization"/)
    assert.match(indexHtml, /"@type": "FAQPage"/)
    assert.match(indexHtml, /豆比特 token01大模型Token中转站/)
    assert.equal(indexHtml.match(/"@type": "Question"/g)?.length, 6)
    assert.doesNotMatch(indexHtml, /大模型 Token 中转常见问题/)
  })
})
