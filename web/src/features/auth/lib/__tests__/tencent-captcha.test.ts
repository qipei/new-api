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
import { afterEach, expect, test, vi } from 'vitest'

import { openTencentCaptcha } from '../tencent-captcha'

afterEach(() => {
  delete window.TencentCaptcha
  document.head.querySelectorAll('script').forEach((script) => script.remove())
})

test('failed captcha script downloads can be retried', async () => {
  const first = openTencentCaptcha('123')
  const firstFailure = expect(first).rejects.toThrow()
  const failedScript = document.head.querySelector('script')
  if (!failedScript) throw new Error('Captcha script was not inserted')
  failedScript.dispatchEvent(new Event('error'))
  await firstFailure

  const second = openTencentCaptcha('123')
  const script = document.head.querySelector('script')
  expect(script).not.toBeNull()
  expect(script).not.toBe(failedScript)
  const show = vi.fn()
  window.TencentCaptcha = class {
    constructor(
      _app: string,
      private callback: (result: { ret: number }) => void
    ) {}
    show() {
      show()
      this.callback({ ret: 2 })
    }
  }
  if (!script) throw new Error('Captcha script retry was not inserted')
  script.dispatchEvent(new Event('load'))
  expect(await second).toBeNull()
  expect(show).toHaveBeenCalledOnce()
})

test('fallback tickets are not treated as completed verification', async () => {
  window.TencentCaptcha = class {
    constructor(
      _app: string,
      private callback: (result: {
        ret: number
        ticket: string
        randstr: string
        errorCode: number
      }) => void
    ) {}
    show() {
      this.callback({
        ret: 0,
        ticket: 'trerror_1001_app',
        randstr: 'random',
        errorCode: 1001,
      })
    }
  }
  await expect(openTencentCaptcha('195901070')).rejects.toThrow(
    'Security verification failed, please retry'
  )
})
