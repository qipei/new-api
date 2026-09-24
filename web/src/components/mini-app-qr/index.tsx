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
import { useQuery } from '@tanstack/react-query'
import { Download, Maximize2, Smartphone } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'

interface MiniAppQRProps {
  placement?: 'home' | 'pricing'
}

export function MiniAppQR(props: MiniAppQRProps) {
  const { t } = useTranslation()
  const [failedImage, setFailedImage] = useState('')
  const query = useQuery({
    queryKey: ['miniapp-qr-code'],
    queryFn: async () => {
      const response = await api.get<{
        success: boolean
        data: { enabled: boolean; image_url: string }
      }>('/api/miniapp/qr-code', {
        skipBusinessError: true,
        skipErrorHandler: true,
      })
      if (!response.data.success) {
        throw new Error('Mini program QR code unavailable')
      }
      return response.data.data
    },
    retry: false,
  })
  const imageURL = query.data?.image_url
  if (
    query.isError ||
    !query.data?.enabled ||
    !imageURL ||
    imageURL === failedImage
  ) {
    return null
  }
  const isPricing = props.placement === 'pricing'

  return (
    <Dialog
      title={t('WeChat Mini Program')}
      description={t(
        'Scan with WeChat, or save the image and open it in WeChat to recognize the QR code.'
      )}
      contentClassName='sm:max-w-sm'
      trigger={
        <button
          type='button'
          aria-label={t('Enlarge mini program QR code')}
          className={cn(
            'group flex w-full min-w-0 items-center justify-between gap-4 rounded-2xl border p-4 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-amber-400 focus-visible:ring-offset-2',
            isPricing
              ? 'border-border/70 bg-background/80 hover:border-amber-400/60 xl:mx-auto xl:max-w-[208px] xl:flex-col xl:gap-3 xl:text-center'
              : 'border-[#e3e1d8] bg-white hover:border-[#ffc800] dark:border-white/10 dark:bg-[#191919] dark:hover:border-[#ffc800]/60'
          )}
        >
          <span
            className={cn(
              'flex min-w-0 flex-1 flex-col items-start gap-2',
              isPricing && 'xl:items-center'
            )}
          >
            <span className='inline-flex items-center gap-1.5 text-[11px] font-medium text-emerald-700 dark:text-emerald-400'>
              <Smartphone className='size-3.5 shrink-0' aria-hidden='true' />
              {t('WeChat Mini Program')}
            </span>
            <span className='text-base leading-snug font-semibold'>
              {t('Scan with WeChat')}
            </span>
            <span className='text-muted-foreground text-xs leading-relaxed'>
              {t('Balance, usage and daily check-ins')}
            </span>
            <span className='mt-1 inline-flex items-center gap-1 text-[11px] text-amber-800 dark:text-amber-300'>
              <Maximize2 className='size-3' aria-hidden='true' />
              {t('Enlarge mini program QR code')}
            </span>
          </span>
          <img
            src={imageURL}
            onError={() => setFailedImage(imageURL)}
            alt={t('Mini program QR code')}
            width={344}
            height={344}
            decoding='async'
            className={cn(
              'h-auto w-[100px] shrink-0 rounded-lg bg-white object-contain sm:w-[112px]',
              isPricing && 'xl:order-first'
            )}
          />
        </button>
      }
      footer={
        <a
          href={imageURL}
          target='_blank'
          rel='noopener noreferrer'
          download
          className='inline-flex w-full items-center justify-center gap-2 rounded-lg bg-[#ffc800] px-4 py-2.5 text-sm font-medium text-[#141414] hover:bg-[#efbb00] focus-visible:ring-2 focus-visible:ring-amber-400 focus-visible:ring-offset-2 focus-visible:outline-none'
        >
          <Download className='size-4' aria-hidden='true' />
          {t('Save QR code')}
        </a>
      }
    >
      <img
        src={imageURL}
        onError={() => setFailedImage(imageURL)}
        alt={t('Mini program QR code')}
        width={344}
        height={344}
        className='mx-auto h-auto w-full max-w-[320px] rounded-xl bg-white object-contain'
      />
    </Dialog>
  )
}
