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
import { Share2 } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'

import type { UserWalletData } from '../types'

interface AffiliateRewardsCardProps {
  user: UserWalletData | null
  affiliateLink: string
  onTransfer: () => void
  onShowCommissions: () => void
  complianceConfirmed?: boolean
  loading?: boolean
}

export function AffiliateRewardsCard({
  user,
  affiliateLink,
  onTransfer,
  onShowCommissions,
  complianceConfirmed = true,
  loading,
}: AffiliateRewardsCardProps) {
  const { t } = useTranslation()
  if (loading) {
    return (
      <Card data-card-hover='false' className='bg-muted/20 py-0'>
        <CardContent className='grid gap-4 p-4 sm:p-5 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center'>
          <div className='space-y-4'>
            <div>
              <Skeleton className='h-5 w-32' />
              <Skeleton className='mt-2 h-4 w-80 max-w-full' />
            </div>
            <Skeleton className='h-16 w-full max-w-lg rounded-xl' />
            <Skeleton className='h-10 w-full rounded-lg' />
          </div>
          <Skeleton className='mx-auto size-[100px] rounded-lg' />
        </CardContent>
      </Card>
    )
  }

  const hasRewards = (user?.aff_quota ?? 0) > 0

  return (
    <Card data-card-hover='false' className='bg-muted/20 py-0'>
      <CardContent className='grid gap-4 p-4 sm:p-5 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center'>
        <div className='min-w-0 space-y-4'>
          <div
            data-slot='affiliate-rewards-summary'
            className='flex min-w-0 items-start gap-3'
          >
            <IconBadge tone='chart-3'>
              <Share2 />
            </IconBadge>
            <div className='min-w-0'>
              <h3 className='text-sm font-semibold'>{t('Referral Program')}</h3>
              <p className='text-muted-foreground mt-1 max-w-3xl text-xs leading-relaxed'>
                {t(
                  'Invite users via your referral link to earn sign-up rewards and commission on their paid top-ups. Transfer accumulated rewards to your balance anytime.'
                )}
              </p>
            </div>
          </div>

          <div className='grid w-full max-w-2xl gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'>
            <div
              data-slot='affiliate-rewards-stats'
              className='bg-background/50 grid w-full grid-cols-3 gap-2 rounded-xl border p-3 text-center'
            >
              {[
                [t('Pending'), formatQuota(user?.aff_quota ?? 0)],
                [t('Total Earned'), formatQuota(user?.aff_history_quota ?? 0)],
                [t('Invites'), String(user?.aff_count ?? 0)],
              ].map(([label, value]) => (
                <div key={label} className='min-w-0 px-1'>
                  <div className='text-muted-foreground truncate text-[10px] font-medium tracking-wider uppercase'>
                    {label}
                  </div>
                  <div className='mt-1 truncate text-sm font-semibold tabular-nums'>
                    {value}
                  </div>
                </div>
              ))}
            </div>
            <div className='flex flex-wrap items-center justify-end gap-2'>
              <Button
                variant='outline'
                onClick={onShowCommissions}
                className='h-9 shrink-0 px-3'
                size='sm'
              >
                {t('Commission Details')}
              </Button>
              {hasRewards ? (
                <Button
                  onClick={onTransfer}
                  disabled={!complianceConfirmed}
                  className='h-9 shrink-0 px-3'
                  size='sm'
                >
                  {t('Transfer to Balance')}
                </Button>
              ) : null}
            </div>
          </div>

          <div
            data-slot='affiliate-rewards-actions'
            className='w-full max-w-2xl space-y-2'
          >
            <div
              data-slot='affiliate-rewards-link-row'
              className='grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2'
            >
              <Input
                value={affiliateLink}
                readOnly
                className='border-muted bg-background/70 h-9 min-w-0 font-mono text-xs'
              />
              <CopyButton
                value={affiliateLink}
                variant='outline'
                className='bg-background size-9 shrink-0'
                iconClassName='size-4'
                tooltip={t('Copy referral link')}
                aria-label={t('Copy referral link')}
              />
            </div>
          </div>

          {!complianceConfirmed ? (
            <p className='text-muted-foreground text-xs'>
              {t(
                'Referral reward transfer is disabled until the administrator confirms compliance terms.'
              )}
            </p>
          ) : null}
        </div>

        <div
          data-slot='affiliate-rewards-qr'
          className='flex flex-col items-center justify-center gap-1.5 lg:justify-self-end'
        >
          <div
            className='rounded-lg bg-white p-1.5'
            title={t('Scan to open your referral sign-up link')}
          >
            <QRCodeSVG value={affiliateLink} size={100} />
          </div>
          <span className='text-muted-foreground text-xs font-medium'>
            {t('Invitation QR code')}
          </span>
        </div>
      </CardContent>
    </Card>
  )
}
