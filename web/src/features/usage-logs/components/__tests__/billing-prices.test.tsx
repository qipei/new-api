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
import { render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import { usageLogSchema } from '../../data/schema'
import { DetailsDialog } from '../dialogs/details-dialog'

beforeEach(() => {
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState())
})

describe('historical billing details', () => {
  it('shows promotional unit prices in the details and tier table and explains the recorded discount', () => {
    const log = usageLogSchema.parse({
      id: 1,
      user_id: 1,
      created_at: 1,
      type: 2,
      content: '',
      other: JSON.stringify({
        billing_mode: 'tiered_expr',
        expr_b64: btoa('tier("busy", p * 2 + c * 8)'),
        matched_tier: 'busy',
        group_ratio: 0.4,
        user_group_ratio: 0.8,
        promotion_ratio: 0.5,
        promotion_name: 'Summer sale',
      }),
    })
    render(
      <DetailsDialog log={log} isAdmin={false} open onOpenChange={() => {}} />
    )
    expect(screen.getByText('$0.8/M')).toBeInTheDocument()
    expect(screen.getByText('$3.2/M')).toBeInTheDocument()
    const table = screen.getByRole('table')
    expect(within(table).getByText('$0.8000')).toBeInTheDocument()
    expect(within(table).getByText('$3.2000')).toBeInTheDocument()
    expect(screen.getByText(/Summer sale/)).toBeInTheDocument()
    expect(screen.getByText('0.4000x')).toBeInTheDocument()
  })

  it.each([
    [{ model_price: 2 }, '$0.8'],
    [{ model_ratio: 1, completion_ratio: 4 }, '$0.8/M'],
  ])('uses the recorded ratio for standard billing %o', (pricing, price) => {
    const log = usageLogSchema.parse({
      id: 1,
      user_id: 1,
      created_at: 1,
      type: 2,
      content: '',
      other: JSON.stringify({ ...pricing, group_ratio: 0.4 }),
    })
    render(
      <DetailsDialog log={log} isAdmin={false} open onOpenChange={() => {}} />
    )
    expect(screen.getByText(price)).toBeInTheDocument()
  })

  it('keeps zero unit prices visible for a free group', () => {
    const log = usageLogSchema.parse({
      id: 1,
      user_id: 1,
      created_at: 1,
      type: 2,
      content: '',
      other: JSON.stringify({
        billing_mode: 'tiered_expr',
        expr_b64: btoa('tier("busy", p * 2 + c * 8)'),
        matched_tier: 'busy',
        group_ratio: 0,
      }),
    })
    render(
      <DetailsDialog log={log} isAdmin={false} open onOpenChange={() => {}} />
    )
    expect(screen.getAllByText('$0/M')).toHaveLength(2)
    expect(
      within(screen.getByRole('table')).getAllByText('$0.0000')
    ).toHaveLength(2)
  })

  it.each([
    [
      'Web Search',
      { web_search: true, web_search_call_count: 2, web_search_price: 10 },
      '2x ($4)',
    ],
    [
      'File Search',
      { file_search: true, file_search_call_count: 3, file_search_price: 5 },
      '3x ($2)',
    ],
    [
      'Image Generation',
      { image_generation_call: true, image_generation_call_price: 0.1 },
      '$0.04',
    ],
    [
      'Audio Input Price',
      { audio_input_seperate_price: true, audio_input_price: 3 },
      '$1.2',
    ],
  ])(
    'includes the recorded promotion in %s without applying token-only request rules',
    (label, pricing, expected) => {
      const log = usageLogSchema.parse({
        id: 1,
        user_id: 1,
        created_at: 1,
        type: 2,
        content: '',
        other: JSON.stringify({
          ...pricing,
          billing_mode: 'tiered_expr',
          expr_b64: btoa(
            '(tier("base", p * 2 + c * 8)) * (param("fast") == true ? 2 : 1)'
          ),
          matched_tier: 'base',
          request_rules: [
            { cond: 'param("fast") == true', multiplier: 2, matched: true },
          ],
          group_ratio: 0.4,
          user_group_ratio: 0.8,
          promotion_ratio: 0.5,
        }),
      })
      render(
        <DetailsDialog log={log} isAdmin={false} open onOpenChange={() => {}} />
      )
      expect(screen.getByText(label)).toBeInTheDocument()
      expect(screen.getByText(expected)).toBeInTheDocument()
    }
  )

  it('keeps the charged total and matched tier when historical rule matches are missing', () => {
    const log = usageLogSchema.parse({
      id: 1,
      user_id: 1,
      created_at: 1,
      type: 2,
      content: '',
      quota: 500000,
      other: JSON.stringify({
        billing_mode: 'tiered_expr',
        expr_b64: btoa(
          '(tier("busy", p * 2 + c * 8)) * (param("fast") == true ? 2 : 1)'
        ),
        matched_tier: 'busy',
        group_ratio: 1,
      }),
    })
    render(
      <DetailsDialog log={log} isAdmin={false} open onOpenChange={() => {}} />
    )
    expect(screen.getByText('busy')).toBeInTheDocument()
    expect(screen.getByText('$1')).toBeInTheDocument()
    expect(screen.queryByText('$2/M')).not.toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })
})
