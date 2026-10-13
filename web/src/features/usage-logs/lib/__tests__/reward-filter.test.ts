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
import { describe, expect, test } from 'vitest'
import { LOG_TYPE_FILTERS } from '../../constants'
import { buildApiParams } from '../utils'

describe('reward log filtering', () => {
  test.each(['100', '101'])('keeps reward type %s in the backend query', (type) => {
    expect(LOG_TYPE_FILTERS.some((filter) => filter.value === type)).toBe(true)
    const params = buildApiParams({
      page: 1,
      pageSize: 20,
      searchParams: { type: [type] },
      columnFilters: [],
      isAdmin: false,
    })
    expect(params.type).toBe(Number(type))
  })
})
