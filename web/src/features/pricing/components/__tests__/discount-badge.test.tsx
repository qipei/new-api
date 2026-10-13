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
import { render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import { DiscountBadge } from '../discount-badge'

describe('DiscountBadge', () => {
  it('displays a zero discount as free in Chinese', async () => {
    const i18n = createInstance()
    await i18n.init({ lng: 'zh', resources: { zh } })
    render(
      <I18nextProvider i18n={i18n}>
        <DiscountBadge
          discount={{ ratio: 0, group: 'default', promotion: null }}
        />
      </I18nextProvider>
    )
    expect(screen.getByText('免费')).toBeInTheDocument()
    expect(screen.queryByText('0折')).not.toBeInTheDocument()
  })
})
