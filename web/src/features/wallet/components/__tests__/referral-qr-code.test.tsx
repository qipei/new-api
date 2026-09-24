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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { DEFAULT_LOGO } from '@/lib/constants'

import { AffiliateRewardsCard } from '../affiliate-rewards-card'

const referralLink = 'https://example.com/sign-up?aff=PG3A'
function renderCard(link = referralLink) {
  return render(
    <AffiliateRewardsCard
      user={null}
      affiliateLink={link}
      onTransfer={() => {}}
      onShowCommissions={() => {}}
    />
  )
}

afterEach(() => vi.unstubAllGlobals())

describe('referral QR code', () => {
  it('labels the referral code and offers an accessible download button with the default logo', () => {
    renderCard()
    expect(screen.getByText('My referral code')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Download QR code' })
    ).toBeEnabled()
    const qr = screen.getByRole('img', { name: 'My referral code' })
    expect(qr.querySelector('image')).toHaveAttribute('href', DEFAULT_LOGO)
  })

  it('does not generate or download a code before the referral link is available', () => {
    renderCard('')
    expect(
      screen.queryByRole('img', { name: 'My referral code' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Download QR code' })
    ).toBeDisabled()
  })

  it('downloads a PNG with its logo embedded rather than an external image reference', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        blob: async () => new Blob(['logo'], { type: 'image/gif' }),
      })
    )
    let imageSource = ''
    vi.stubGlobal(
      'Image',
      class extends EventTarget {
        set src(value: string) {
          imageSource = value
          queueMicrotask(() => this.dispatchEvent(new Event('load')))
        }
      }
    )
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
      drawImage: vi.fn(),
    } as unknown as CanvasRenderingContext2D)
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue(
      'data:image/png;base64,cG5n'
    )
    const downloads: { filename: string; href: string }[] = []
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(
      function (this: HTMLAnchorElement) {
        downloads.push({ filename: this.download, href: this.href })
      }
    )
    renderCard()
    fireEvent.click(screen.getByRole('button', { name: 'Download QR code' }))
    await waitFor(() =>
      expect(downloads).toEqual([
        {
          filename: 'my-referral-code.png',
          href: 'data:image/png;base64,cG5n',
        },
      ])
    )
    const exported = decodeURIComponent(
      imageSource.split(',').slice(1).join(',')
    )
    expect(exported).toContain('data:image/gif;base64,')
    expect(exported).not.toContain(DEFAULT_LOGO)
    expect(
      screen.getByRole('button', { name: 'Download QR code' })
    ).toBeEnabled()
  })

  it('reports a failed download and allows retry instead of downloading a broken code', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }))
    const error = vi.spyOn(toast, 'error')
    const download = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => {})
    renderCard()
    fireEvent.click(screen.getByRole('button', { name: 'Download QR code' }))
    await waitFor(() =>
      expect(error).toHaveBeenCalledWith(
        'Failed to save QR code. Please try again.'
      )
    )
    expect(download).not.toHaveBeenCalled()
    expect(
      screen.getByRole('button', { name: 'Download QR code' })
    ).toBeEnabled()
  })
})
