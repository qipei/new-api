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
import i18next from 'i18next'
import type React from 'react'
import { beforeAll, describe, expect, test } from 'vitest'

import { formatLogQuota } from '@/lib/format'

import { LogCostDisplay } from '../log-cost-display'

function renderCost(
  props: React.ComponentProps<typeof LogCostDisplay>
): ReturnType<typeof render> {
  return render(<LogCostDisplay {...props} />)
}

function normalizedText(value: string | null): string {
  return (value ?? '').replaceAll(/\s/g, '')
}

describe('log cost display', () => {
  beforeAll(() => {
    i18next.addResourceBundle('en', 'translation', {
      Subscription: 'Subscription',
      'Deducted by subscription': 'Deducted by subscription',
      'Includes tool-call surcharge': 'Includes tool-call surcharge',
      'Upstream cost higher': 'Upstream cost higher',
      Upstream: 'Upstream',
      'Upstream cost': 'Upstream cost',
      'Platform charged': 'Platform charged',
      Margin: 'Margin',
    })
  })

  test('keeps the regular cost visible and adds an accessible surcharge marker', () => {
    const rendered = renderCost({
      quota: 12500,
      other: {
        tool_surcharges: [{ name: 'lookup_customer', count: 1, price: 5 }],
      },
    })

    expect(
      normalizedText(rendered.container.textContent).includes(
        normalizedText(formatLogQuota(12500))
      )
    ).toBe(true)
    const marker = screen.getByRole('img', {
      name: 'Includes tool-call surcharge',
    })
    expect(marker).toHaveAttribute('data-tool-surcharge-indicator', 'true')
    expect(marker).toHaveAttribute('tabindex', '0')
  })

  test('preserves the subscription badge and adds the same legacy surcharge marker', () => {
    renderCost({
      quota: 5000,
      other: {
        billing_source: 'subscription',
        web_search: true,
        web_search_call_count: 1,
        web_search_price: 10,
      },
    })

    expect(screen.getByText('Subscription')).toBeInTheDocument()
    expect(
      screen.getByRole('img', { name: 'Includes tool-call surcharge' })
    ).toHaveAttribute('data-tool-surcharge-indicator', 'true')
  })

  test('always shows the upstream cost to admins and flags it only when it is higher', () => {
    const losing = {
      log_id: 7,
      upstream_quota: 154357,
      upstream_quota_per_unit: 500000,
      upstream_price: 6.82,
      platform_quota: 922255,
      platform_quota_per_unit: 500000,
      platform_price: 1,
      normalized_upstream_quota: 1052715,
      upstream_amount: 2.10543,
      platform_amount: 1.84451,
      exceeds_platform: true,
    }
    const rendered = renderCost({
      quota: 922255,
      other: {},
      upstreamCost: losing,
      isAdmin: true,
    })

    expect(screen.getByLabelText(/^Upstream /)).toHaveAttribute(
      'data-upstream-cost-exceeds',
      'true'
    )

    rendered.rerender(
      <LogCostDisplay
        quota={922255}
        other={{}}
        isAdmin
        upstreamCost={{
          ...losing,
          normalized_upstream_quota: 68200,
          upstream_amount: 0.1364,
          exceeds_platform: false,
        }}
      />
    )

    expect(screen.getByLabelText(/^Upstream /)).toHaveAttribute(
      'data-upstream-cost-exceeds',
      'false'
    )
  })

  test('never exposes upstream cost or margin to non-admins', () => {
    renderCost({
      quota: 922255,
      other: {},
      isAdmin: false,
      upstreamCost: {
        log_id: 7,
        upstream_quota: 154357,
        upstream_quota_per_unit: 500000,
        upstream_price: 6.82,
        platform_quota: 922255,
        platform_quota_per_unit: 500000,
        platform_price: 1,
        normalized_upstream_quota: 1052715,
        upstream_amount: 2.10543,
        platform_amount: 1.84451,
        exceeds_platform: true,
      },
    })

    expect(screen.queryByLabelText(/^Upstream /)).toBeNull()
  })
})
