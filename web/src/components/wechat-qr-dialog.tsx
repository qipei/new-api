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
import { CheckCircle2, Loader2, TimerOff } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import * as React from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

/**
 * How often to ask our own backend whether the order settled. The callback
 * normally lands first; this is the user-visible confirmation path.
 */
const POLL_INTERVAL_MS = 3000

/** Pending WeChat Native order rendered by the dialog. */
export interface WechatQrOrder {
  codeUrl: string
  tradeNo: string
  /** Unix seconds. */
  expiresAt: number
}

type QrPhase = 'waiting' | 'paid' | 'expired'

function formatCountdown(seconds: number): string {
  const safe = Math.max(0, seconds)
  const minutes = Math.floor(safe / 60)
  const rest = safe % 60
  return `${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}`
}

interface WechatQrDialogProps {
  order: WechatQrOrder | null
  amount: number
  onClose: () => void
  onPaid: () => void
  /**
   * Resolves to true once the order has settled. Top-ups and subscription
   * orders live in different tables, so each caller supplies its own poller.
   */
  pollStatus: (tradeNo: string) => Promise<boolean>
}

/**
 * WeChat Native checkout: renders the code_url as a QR code and polls until the
 * order settles or the window closes.
 *
 * WeChat no longer supports long-press or album recognition of payment QR
 * codes, so the copy tells users to scan with the in-app scanner rather than
 * offering a fallback that would not work.
 */
export function WechatQrDialog(props: WechatQrDialogProps) {
  const { t } = useTranslation()
  const [phase, setPhase] = React.useState<QrPhase>('waiting')
  const [secondsLeft, setSecondsLeft] = React.useState(0)

  const order = props.order
  const tradeNo = order?.tradeNo
  const expiresAt = order?.expiresAt ?? 0
  const onPaid = props.onPaid
  const pollStatus = props.pollStatus

  // A new order resets the dialog; without this a second attempt would open
  // straight into the previous order's paid or expired state.
  React.useEffect(() => {
    if (tradeNo) {
      setPhase('waiting')
    }
  }, [tradeNo])

  React.useEffect(() => {
    if (!tradeNo || phase !== 'waiting') {
      return
    }

    let cancelled = false

    const tick = () => {
      const remaining = expiresAt - Math.floor(Date.now() / 1000)
      setSecondsLeft(remaining)
      if (expiresAt > 0 && remaining <= 0 && !cancelled) {
        setPhase('expired')
      }
    }
    tick()
    const countdown = window.setInterval(tick, 1000)

    const poll = window.setInterval(async () => {
      try {
        const settled = await pollStatus(tradeNo)
        if (!cancelled && settled) {
          setPhase('paid')
          onPaid()
        }
      } catch {
        // Transient failures are expected while the tab is backgrounded or the
        // network blips; the next tick retries.
      }
    }, POLL_INTERVAL_MS)

    return () => {
      cancelled = true
      window.clearInterval(countdown)
      window.clearInterval(poll)
    }
  }, [tradeNo, expiresAt, phase, onPaid, pollStatus])

  if (!order) return null

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('WeChat Pay')}
      description={t('Scan the QR code with WeChat to complete the payment.')}
      contentClassName='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-[400px]'
      contentHeight='auto'
      footer={
        <Button variant='outline' onClick={props.onClose}>
          {phase === 'paid' ? t('Done') : t('Cancel')}
        </Button>
      }
    >
      <div className='flex flex-col items-center gap-4 py-2'>
        <div className='text-2xl font-semibold tabular-nums'>
          ¥{props.amount.toFixed(2)}
        </div>

        {phase === 'waiting' && (
          <>
            <div className='rounded-lg border bg-white p-3'>
              <QRCodeSVG value={order.codeUrl} size={192} level='M' />
            </div>
            <div className='text-muted-foreground flex items-center gap-2 text-sm'>
              <Loader2 className='h-3.5 w-3.5 animate-spin' />
              {t('Waiting for payment')}
              {order.expiresAt > 0 && (
                <span className='tabular-nums'>
                  {formatCountdown(secondsLeft)}
                </span>
              )}
            </div>
            <Alert>
              <AlertDescription>
                {t(
                  'Open WeChat and use Scan. Long-press and album recognition are not supported for payment codes.'
                )}
              </AlertDescription>
            </Alert>
          </>
        )}

        {phase === 'paid' && (
          <div className='flex flex-col items-center gap-2 py-8'>
            <CheckCircle2 className='h-12 w-12 text-emerald-500' />
            <p className='font-medium'>{t('Payment received')}</p>
          </div>
        )}

        {phase === 'expired' && (
          <div className='flex flex-col items-center gap-2 py-8'>
            <TimerOff className='text-muted-foreground h-12 w-12' />
            <p className='font-medium'>{t('This QR code has expired')}</p>
            <p className='text-muted-foreground max-w-[18rem] text-center text-sm'>
              {t(
                'Close this window and start a new top-up. Unpaid orders are not shown in WeChat, so they cannot be resumed there.'
              )}
            </p>
          </div>
        )}

        <p className='text-muted-foreground font-mono text-xs'>
          {order.tradeNo}
        </p>
      </div>
    </Dialog>
  )
}
