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

/**
 * Tencent Cloud Captcha is loaded on demand: the script is only fetched the
 * first time the backend actually asks for a captcha, so installations that
 * never enable SMS anti-abuse do not pay for a third-party script on every
 * page load.
 */
const TENCENT_CAPTCHA_SCRIPT_URL =
  'https://turing.captcha.qcloud.com/TJCaptcha.js'

export interface TencentCaptchaTicket {
  ticket: string
  randstr: string
}

interface TencentCaptchaCallbackResult {
  ret: number
  ticket?: string
  randstr?: string
  errorCode?: number
  errorMessage?: string
}

type TencentCaptchaInstance = {
  show: () => void
  destroy?: () => void
}

type TencentCaptchaConstructor = new (
  appId: string,
  callback: (result: TencentCaptchaCallbackResult) => void,
  options?: Record<string, unknown>
) => TencentCaptchaInstance

declare global {
  interface Window {
    TencentCaptcha?: TencentCaptchaConstructor
  }
}

let scriptPromise: Promise<void> | null = null

function loadCaptchaScript(): Promise<void> {
  if (window.TencentCaptcha) return Promise.resolve()
  if (scriptPromise) return scriptPromise

  scriptPromise = new Promise<void>((resolve, reject) => {
    const existing = document.querySelector<HTMLScriptElement>(
      `script[src="${TENCENT_CAPTCHA_SCRIPT_URL}"]`
    )
    const script = existing ?? document.createElement('script')
    script.addEventListener('load', () => resolve())
    script.addEventListener('error', () => {
      // Let a later attempt retry the download instead of caching the failure.
      scriptPromise = null
      script.remove()
      reject(new Error(i18next.t('Security verification is not available')))
    })
    if (!existing) {
      script.src = TENCENT_CAPTCHA_SCRIPT_URL
      script.async = true
      document.head.appendChild(script)
    }
  })

  return scriptPromise
}

/**
 * Opens the captcha dialog and resolves with the ticket to hand back to the
 * server. Resolves with null when the user closes the dialog without solving
 * it, so callers can quietly abort instead of showing an error.
 */
export async function openTencentCaptcha(
  appId: string
): Promise<TencentCaptchaTicket | null> {
  await loadCaptchaScript()
  const Captcha = window.TencentCaptcha
  if (!Captcha) {
    throw new Error(i18next.t('Security verification is not available'))
  }

  return new Promise<TencentCaptchaTicket | null>((resolve, reject) => {
    const captcha = new Captcha(appId, (result) => {
      if (result.errorCode || result.ticket?.startsWith('trerror_')) {
        reject(
          new Error(i18next.t('Security verification failed, please retry'))
        )
        return
      }
      // ret === 0 means solved; anything else means the user closed the
      // dialog or the captcha itself errored out.
      if (result.ret === 0 && result.ticket && result.randstr) {
        resolve({ ticket: result.ticket, randstr: result.randstr })
        return
      }
      resolve(null)
    })
    captcha.show()
  })
}
