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
import { describe, expect, test } from 'vitest'

import { usageLogSchema } from '../../data/schema'
import { DetailsDialog } from '../dialogs/details-dialog'

const log = usageLogSchema.parse({
  id: 1,
  user_id: 1,
  created_at: 1700000000,
  type: 2,
  content: '',
  model_name: 'public-model',
  other: JSON.stringify({
    is_model_mapped: true,
    upstream_model_name: 'private-upstream-model',
  }),
})

describe('log details model mapping visibility', () => {
  test('hides model mapping from ordinary users even when the log contains it', () => {
    render(
      <DetailsDialog log={log} isAdmin={false} open onOpenChange={() => {}} />
    )

    expect(screen.getByText('Billing Details')).toBeInTheDocument()
    expect(screen.queryByText('Model Mapping')).not.toBeInTheDocument()
    expect(screen.queryByText('private-upstream-model')).not.toBeInTheDocument()
  })

  test('keeps model mapping visible to administrators', () => {
    render(<DetailsDialog log={log} isAdmin open onOpenChange={() => {}} />)

    expect(screen.getByText('Model Mapping')).toBeInTheDocument()
    expect(screen.getByText('private-upstream-model')).toBeInTheDocument()
  })
})

describe('reward log details', () => {
  test('shows credited reward and hides ad audit identifiers from ordinary users', () => {
    const reward = usageLogSchema.parse({
      id: 9,
      user_id: 1,
      created_at: 1700000000,
      type: 101,
      content: 'Ad reward',
      quota: 100,
      other: JSON.stringify({
        reward_record_id: 'reward-record-9',
        credited_at: 1700000000,
        admin_info: {
          trans_id: 'private-ad-transaction',
          adpid: 'private-ad-placement',
        },
      }),
    })
    render(
      <DetailsDialog
        log={reward}
        isAdmin={false}
        open
        onOpenChange={() => {}}
      />
    )
    expect(screen.getByText('Reward Details')).toBeInTheDocument()
    expect(screen.getByText('Reward Amount')).toBeInTheDocument()
    expect(screen.getByText('reward-record-9')).toBeInTheDocument()
    expect(screen.getByText('Credited At')).toBeInTheDocument()
    expect(screen.queryByText('Billing Details')).not.toBeInTheDocument()
    expect(screen.queryByText('private-ad-transaction')).not.toBeInTheDocument()
    expect(screen.queryByText('private-ad-placement')).not.toBeInTheDocument()
  })
  test('shows transaction and placement audit identifiers to administrators', () => {
    const reward = usageLogSchema.parse({
      id: 9,
      user_id: 1,
      created_at: 1700000000,
      type: 101,
      content: 'Ad reward',
      quota: 100,
      other: JSON.stringify({
        admin_info: {
          trans_id: 'private-ad-transaction',
          adpid: 'private-ad-placement',
        },
      }),
    })
    render(<DetailsDialog log={reward} isAdmin open onOpenChange={() => {}} />)
    expect(screen.getByText('private-ad-transaction')).toBeInTheDocument()
    expect(screen.getByText('private-ad-placement')).toBeInTheDocument()
  })
})
