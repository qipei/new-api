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
import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import type { SMSCodePayload } from '../../types'
import { useSMSVerification } from '../use-sms-verification'

afterEach(() => {
  vi.useRealTimers()
  delete window.TencentCaptcha
})

describe('SMS verification requests', () => {
  test('captcha retry waits for a fresh Turnstile token', async () => {
    window.TencentCaptcha = class {
      constructor(
        _app: string,
        private callback: (result: {
          ret: number
          ticket: string
          randstr: string
        }) => void
      ) {}
      show() {
        this.callback({
          ret: 0,
          ticket: 'captcha-ticket',
          randstr: 'captcha-random',
        })
      }
    }
    const send = vi.fn(async (payload: SMSCodePayload) =>
      payload.captcha_ticket
        ? { success: true, message: '', data: { resend_after: 60 } }
        : {
            success: true,
            message: '',
            data: { require_captcha: true, captcha_app_id: '123' },
          }
    )
    const consumed = vi.fn()
    const { result, rerender } = renderHook(
      ({ token }) =>
        useSMSVerification({
          send,
          turnstileToken: token,
          onTurnstileConsumed: consumed,
        }),
      { initialProps: { token: 'first-token' } }
    )
    let pending: Promise<boolean>
    act(() => {
      pending = result.current.sendCode('13800138000')
    })
    await waitFor(() => expect(consumed).toHaveBeenCalledOnce())
    expect(send).toHaveBeenCalledTimes(1)
    rerender({ token: 'fresh-token' })
    await act(async () => {
      expect(await pending).toBe(true)
    })
    expect(send.mock.calls[1][0]).toMatchObject({
      turnstile: 'fresh-token',
      captcha_ticket: 'captcha-ticket',
    })
    expect(consumed).toHaveBeenCalledTimes(2)
  })

  test('failed requests consume Turnstile tokens and allow retry', async () => {
    const consumed = vi.fn()
    const send = vi.fn().mockRejectedValue(new Error('offline'))
    const { result } = renderHook(() =>
      useSMSVerification({
        send,
        turnstileToken: 'token',
        onTurnstileConsumed: consumed,
      })
    )
    await act(async () => {
      expect(await result.current.sendCode('13800138000')).toBe(false)
    })
    expect(consumed).toHaveBeenCalledOnce()
    expect(result.current.isSending).toBe(false)
  })

  test('zero resend interval does not start a default countdown', async () => {
    const send = vi.fn().mockResolvedValue({
      success: true,
      message: '',
      data: { resend_after: 0 },
    })
    const { result } = renderHook(() => useSMSVerification({ send }))
    await act(async () => {
      expect(await result.current.sendCode('13800138000')).toBe(true)
    })
    expect(result.current.isActive).toBe(false)
  })
})

test('rate-limit challenge opens captcha, waits, then retries with its ticket', async () => {
  vi.useFakeTimers()
  window.TencentCaptcha = class {
    constructor(
      _app: string,
      private callback: (result: {
        ret: number
        ticket: string
        randstr: string
      }) => void
    ) {}
    show() {
      this.callback({ ret: 0, ticket: 'verified-ticket', randstr: 'random' })
    }
  }
  const send = vi
    .fn()
    .mockRejectedValueOnce({
      isAxiosError: true,
      response: {
        status: 429,
        headers: { 'retry-after': '18' },
        data: {
          success: false,
          message: 'Please wait',
          data: {
            require_captcha: true,
            captcha_app_id: '195901070',
            resend_after: 18,
          },
        },
      },
    })
    .mockResolvedValueOnce({
      success: true,
      message: '',
      data: { resend_after: 60 },
    })
  const { result } = renderHook(() => useSMSVerification({ send }))
  let pending: Promise<boolean>
  await act(async () => {
    pending = result.current.sendCode('13800138000')
    await Promise.resolve()
  })
  expect(send).toHaveBeenCalledTimes(1)
  expect(result.current.isActive).toBe(true)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(17000)
  })
  expect(send).toHaveBeenCalledTimes(1)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000)
    expect(await pending).toBe(true)
  })
  expect(send.mock.calls[1][0]).toMatchObject({
    captcha_ticket: 'verified-ticket',
    captcha_randstr: 'random',
  })
})

test('leaving the form cancels a pending cooldown retry', async () => {
  vi.useFakeTimers()
  window.TencentCaptcha = class {
    constructor(
      _app: string,
      private callback: (result: {
        ret: number
        ticket: string
        randstr: string
      }) => void
    ) {}
    show() {
      this.callback({ ret: 0, ticket: 'ticket', randstr: 'random' })
    }
  }
  const send = vi
    .fn()
    .mockResolvedValue({
      success: false,
      message: '',
      data: {
        require_captcha: true,
        captcha_app_id: '195901070',
        resend_after: 18,
      },
    })
  const { result, unmount } = renderHook(() => useSMSVerification({ send }))
  let pending: Promise<boolean>
  await act(async () => {
    pending = result.current.sendCode('13800138000')
    await Promise.resolve()
  })
  unmount()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(18000)
    expect(await pending).toBe(false)
  })
  expect(send).toHaveBeenCalledTimes(1)
})

test('cooldown errors start the countdown even when no SMS was sent', async () => {
  const send = vi
    .fn()
    .mockResolvedValue({
      success: false,
      message: 'Wait 18 seconds',
      data: { resend_after: 18 },
    })
  const { result } = renderHook(() => useSMSVerification({ send }))
  await act(async () => {
    expect(await result.current.sendCode('13800138000')).toBe(false)
  })
  expect(result.current.secondsLeft).toBe(18)
  expect(result.current.isActive).toBe(true)
})
