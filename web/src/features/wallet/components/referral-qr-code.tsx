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
import { Download } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { DEFAULT_LOGO } from '@/lib/constants'

export function ReferralQRCode(props: { value: string }) {
  const { t } = useTranslation()
  const svgRef = useRef<SVGSVGElement>(null)
  const [saving, setSaving] = useState(false)

  async function download() {
    if (!svgRef.current || !props.value || saving) return
    setSaving(true)
    try {
      // Embed the logo so the saved image has no external resource dependency.
      // Clone before awaiting to keep the QR and link from the same render.
      const svg = svgRef.current.cloneNode(true) as SVGSVGElement
      const response = await fetch(DEFAULT_LOGO)
      if (!response.ok) throw new Error('Logo unavailable')
      const logo = await response.blob()
      const logoData = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader()
        reader.addEventListener('load', () => resolve(String(reader.result)), {
          once: true,
        })
        reader.addEventListener('error', () => reject(reader.error), {
          once: true,
        })
        reader.readAsDataURL(logo)
      })
      svg.querySelector('image')?.setAttribute('href', logoData)
      svg.setAttribute('width', '1024')
      svg.setAttribute('height', '1024')
      const source = new XMLSerializer().serializeToString(svg)
      const image = new Image()
      await new Promise<void>((resolve, reject) => {
        image.addEventListener('load', () => resolve(), { once: true })
        image.addEventListener(
          'error',
          () => reject(new Error('QR image unavailable')),
          { once: true }
        )
        image.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(source)}`
      })
      const canvas = document.createElement('canvas')
      canvas.width = canvas.height = 1024
      const context = canvas.getContext('2d')
      if (!context) throw new Error('Canvas unavailable')
      context.drawImage(image, 0, 0, 1024, 1024)
      const link = document.createElement('a')
      link.download = 'my-referral-code.png'
      link.href = canvas.toDataURL('image/png')
      document.body.appendChild(link)
      link.click()
      link.remove()
    } catch {
      toast.error(t('Failed to save QR code. Please try again.'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div
      data-slot='affiliate-rewards-qr'
      className='flex flex-col items-center justify-center gap-2 lg:justify-self-end'
    >
      {props.value && (
        <div
          className='rounded-lg bg-white p-1'
          title={t('Scan to open your referral sign-up link')}
        >
          <QRCodeSVG
            ref={svgRef}
            value={props.value}
            size={144}
            level='H'
            marginSize={4}
            bgColor='#ffffff'
            fgColor='#000000'
            role='img'
            aria-label={t('My referral code')}
            imageSettings={{
              src: DEFAULT_LOGO,
              width: 22,
              height: 22,
              excavate: true,
            }}
          />
        </div>
      )}
      <span className='text-muted-foreground text-xs font-medium'>
        {t('My referral code')}
      </span>
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={download}
        disabled={!props.value || saving}
        aria-busy={saving}
        aria-label={t('Download QR code')}
      >
        <Download aria-hidden='true' />
        {saving ? t('Saving...') : t('Download QR code')}
      </Button>
    </div>
  )
}
