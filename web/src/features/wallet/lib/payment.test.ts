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

import { PAYMENT_TYPES } from '../constants'
import {
  dispatchSelectedPayment,
  isAlipayDirectPayment,
  isStripePayment,
  isWaffoPayment,
  isWaffoPancakePayment,
  isWechatDirectPayment,
} from './payment'

describe('payment type classification', () => {
  test('keeps Waffo and Waffo Pancake on their dedicated flows', () => {
    expect(isWaffoPayment(PAYMENT_TYPES.WAFFO)).toBe(true)
    expect(isWaffoPayment(PAYMENT_TYPES.WAFFO_PANCAKE)).toBe(false)
    expect(isWaffoPancakePayment(PAYMENT_TYPES.WAFFO_PANCAKE)).toBe(true)
    expect(isWaffoPancakePayment(PAYMENT_TYPES.WAFFO)).toBe(false)
    expect(isStripePayment(PAYMENT_TYPES.STRIPE)).toBe(true)
  })

  test('separates the official gateways from the Epay aggregator', () => {
    expect(isAlipayDirectPayment(PAYMENT_TYPES.ALIPAY_DIRECT)).toBe(true)
    expect(isAlipayDirectPayment(PAYMENT_TYPES.ALIPAY)).toBe(false)
    expect(isWechatDirectPayment(PAYMENT_TYPES.WECHAT_DIRECT)).toBe(true)
    expect(isWechatDirectPayment(PAYMENT_TYPES.WECHAT)).toBe(false)
  })
})

describe('payment dispatch', () => {
  test('keeps the selected Waffo method index through confirmation', async () => {
    const calls: string[] = []
    const success = await dispatchSelectedPayment(
      { name: 'Waffo Card', type: PAYMENT_TYPES.WAFFO },
      120,
      3,
      {
        regular: async () => {
          calls.push('regular')
          return false
        },
        waffo: async (amount, index) => {
          calls.push(`waffo:${amount}:${index}`)
          return true
        },
        waffoPancake: async () => {
          calls.push('pancake')
          return false
        },
        alipayDirect: async () => {
          calls.push('alipay-direct')
          return false
        },
        wechatDirect: async () => {
          calls.push('wechat-direct')
          return false
        },
      }
    )

    expect(success).toBe(true)
    expect(calls).toEqual(['waffo:120:3'])
  })

  test('does not create a Waffo order without a selected method index', async () => {
    let called = false
    const success = await dispatchSelectedPayment(
      { name: 'Waffo Card', type: PAYMENT_TYPES.WAFFO },
      120,
      null,
      {
        regular: async () => false,
        waffo: async () => {
          called = true
          return true
        },
        waffoPancake: async () => false,
        alipayDirect: async () => false,
        wechatDirect: async () => false,
      }
    )

    expect(success).toBe(false)
    expect(called).toBe(false)
  })

  // Epay and the official gateways can be enabled at the same time. Routing a
  // direct payment through `regular` would post it to the Epay endpoint, where
  // it fails the payment-method check and no order is ever created upstream.
  test('routes the official gateways away from the Epay processor', async () => {
    const calls: string[] = []
    const processors = {
      regular: async (_amount: number, type: string) => {
        calls.push(`regular:${type}`)
        return false
      },
      waffo: async () => false,
      waffoPancake: async () => false,
      alipayDirect: async (amount: number) => {
        calls.push(`alipay-direct:${amount}`)
        return true
      },
      wechatDirect: async (amount: number) => {
        calls.push(`wechat-direct:${amount}`)
        return true
      },
    }

    await dispatchSelectedPayment(
      { name: '支付宝（官方）', type: PAYMENT_TYPES.ALIPAY_DIRECT },
      50,
      null,
      processors
    )
    await dispatchSelectedPayment(
      { name: '微信支付（官方）', type: PAYMENT_TYPES.WECHAT_DIRECT },
      80,
      null,
      processors
    )
    await dispatchSelectedPayment(
      { name: '支付宝', type: PAYMENT_TYPES.ALIPAY },
      20,
      null,
      processors
    )

    expect(calls).toEqual([
      'alipay-direct:50',
      'wechat-direct:80',
      `regular:${PAYMENT_TYPES.ALIPAY}`,
    ])
  })
})
