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
import i18next from 'i18next'
import { useCallback, useState } from 'react'
import { toast } from 'sonner'

import type { WechatQrOrder } from '@/components/wechat-qr-dialog'

import {
  getTopupOrderStatus,
  isApiSuccess,
  requestAlipayDirectPayment,
  requestWechatDirectPayment,
} from '../api'

function readStringField(data: unknown, field: string): string | null {
  if (!data || typeof data !== 'object') {
    return null
  }
  const value = (data as Record<string, unknown>)[field]
  return typeof value === 'string' && value.trim() ? value : null
}

function readNumberField(data: unknown, field: string): number | null {
  if (!data || typeof data !== 'object') {
    return null
  }
  const value = (data as Record<string, unknown>)[field]
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

/**
 * Reject non-navigable schemes (e.g. javascript:, data:) and relative URLs.
 * Only http/https are allowed for backend-provided redirect targets.
 */
function isSafeHttpUrl(value: string): boolean {
  try {
    const url = new URL(value.trim())
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

function getErrorMessage(message: string | undefined, data: unknown): string {
  if (typeof data === 'string' && data.trim()) {
    return data
  }
  return message || i18next.t('Payment request failed')
}

/**
 * Hook for the two official direct-connect gateways.
 *
 * Alipay redirects in the same tab rather than window.open: the user-gesture
 * context is lost across the await, so popups get blocked.
 *
 * WeChat Native returns a code_url instead, which the caller renders as a QR
 * code. WeChat dropped support for long-press and album recognition, so
 * scanning with the in-app scanner is the only way to pay.
 */
export function useDirectPay() {
  const [processing, setProcessing] = useState(false)
  const [wechatOrder, setWechatOrder] = useState<WechatQrOrder | null>(null)

  const processAlipayDirectPayment = useCallback(
    async (topupAmount: number) => {
      setProcessing(true)
      try {
        const response = await requestAlipayDirectPayment({
          amount: Math.floor(topupAmount),
        })
        if (!isApiSuccess(response)) {
          toast.error(getErrorMessage(response.message, response.data))
          return false
        }

        const payUrl = readStringField(response.data, 'pay_url')
        if (!payUrl || !isSafeHttpUrl(payUrl)) {
          toast.error(i18next.t('Invalid payment redirect URL'))
          return false
        }

        toast.success(i18next.t('Redirecting to payment page...'))
        window.location.href = payUrl
        return true
      } catch {
        toast.error(i18next.t('Payment request failed'))
        return false
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  const processWechatDirectPayment = useCallback(
    async (topupAmount: number) => {
      setProcessing(true)
      try {
        const response = await requestWechatDirectPayment({
          amount: Math.floor(topupAmount),
        })
        if (!isApiSuccess(response)) {
          toast.error(getErrorMessage(response.message, response.data))
          return false
        }

        const codeUrl = readStringField(response.data, 'code_url')
        const tradeNo = readStringField(response.data, 'trade_no')
        if (!codeUrl || !tradeNo) {
          toast.error(i18next.t('Payment request failed'))
          return false
        }

        setWechatOrder({
          codeUrl,
          tradeNo,
          expiresAt: readNumberField(response.data, 'expires_at') ?? 0,
        })
        return true
      } catch {
        toast.error(i18next.t('Payment request failed'))
        return false
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  const closeWechatOrder = useCallback(() => setWechatOrder(null), [])

  const pollTopupOrderStatus = useCallback(async (tradeNo: string) => {
    const response = await getTopupOrderStatus(tradeNo)
    if (!isApiSuccess(response)) {
      return false
    }
    return readStringField(response.data, 'status') === 'success'
  }, [])

  return {
    processing,
    wechatOrder,
    processAlipayDirectPayment,
    processWechatDirectPayment,
    closeWechatOrder,
    pollTopupOrderStatus,
  }
}
