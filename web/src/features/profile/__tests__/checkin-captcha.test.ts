import { toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { performCheckinWithCaptcha } from '../api'

const originalAdapter = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = originalAdapter
  delete window.TencentCaptcha
  vi.restoreAllMocks()
})

function installCaptcha(ret = 0) {
  const show = vi.fn()
  window.TencentCaptcha = class {
    constructor(
      appId: string,
      private callback: (result: {
        ret: number
        ticket: string
        randstr: string
      }) => void
    ) {
      expect(appId).toBe('123')
    }
    show() {
      show()
      this.callback({ ret, ticket: 'ticket', randstr: 'random' })
    }
  }
  return show
}

const status = {
  success: true,
  data: {
    enabled: true,
    captcha_provider: 'tencent',
    captcha_app_id: '123',
    stats: { checked_in_today: false },
  },
}
const challenge = {
  success: false,
  message: 'verification required',
  data: {
    require_captcha: true,
    captcha_provider: 'tencent',
    captcha_app_id: '123',
  },
}

test('fresh Tencent status opens verification directly and submits one ticket without error toast', async () => {
  const show = installCaptcha()
  const errors = vi.spyOn(toast, 'error')
  const calls: Array<{ method?: string; body: unknown }> = []
  api.defaults.adapter = async (config) => {
    const body = config.data ? JSON.parse(config.data as string) : undefined
    calls.push({ method: config.method, body })
    return {
      data:
        config.method === 'get'
          ? status
          : { success: true, data: { quota_awarded: 4800 } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  expect((await performCheckinWithCaptcha())?.success).toBe(true)
  expect(calls).toEqual([
    { method: 'get', body: undefined },
    {
      method: 'post',
      body: {
        captcha_client: 'web',
        captcha_ticket: 'ticket',
        captcha_randstr: 'random',
      },
    },
  ])
  expect(show).toHaveBeenCalledOnce()
  expect(errors).not.toHaveBeenCalled()
})

test('cancelled verification does not submit a check-in request', async () => {
  installCaptcha(2)
  const methods: string[] = []
  api.defaults.adapter = async (config) => {
    methods.push(config.method ?? '')
    return { data: status, status: 200, statusText: 'OK', headers: {}, config }
  }
  expect(await performCheckinWithCaptcha()).toBeNull()
  expect(methods).toEqual(['get'])
})

test('already checked-in fresh status skips verification and awarding despite a stale page', async () => {
  const show = installCaptcha()
  const methods: string[] = []
  api.defaults.adapter = async (config) => {
    methods.push(config.method ?? '')
    return {
      data: {
        ...status,
        data: { ...status.data, stats: { checked_in_today: true } },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  expect(await performCheckinWithCaptcha()).toMatchObject({
    success: false,
    code: 'CHECKIN_ALREADY_DONE',
  })
  expect(show).not.toHaveBeenCalled()
  expect(methods).toEqual(['get'])
})

test('rejected ticket is returned to the card once without global toast or verification loop', async () => {
  const show = installCaptcha()
  const errors = vi.spyOn(toast, 'error')
  const methods: string[] = []
  api.defaults.adapter = async (config) => {
    methods.push(config.method ?? '')
    return {
      data: config.method === 'get' ? status : challenge,
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  expect((await performCheckinWithCaptcha())?.success).toBe(false)
  expect(methods).toEqual(['get', 'post'])
  expect(show).toHaveBeenCalledOnce()
  expect(errors).not.toHaveBeenCalled()
})

test('provider changed after preflight can request verification without a global error toast', async () => {
  installCaptcha()
  const errors = vi.spyOn(toast, 'error')
  let posts = 0
  api.defaults.adapter = async (config) => {
    if (config.method === 'post') posts++
    let data
    if (config.method === 'get') {
      data = { ...status, data: { ...status.data, captcha_provider: 'none' } }
    } else if (posts === 1) {
      data = challenge
    } else {
      data = { success: true, data: { quota_awarded: 1 } }
    }
    return {
      data,
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  expect((await performCheckinWithCaptcha())?.success).toBe(true)
  expect(posts).toBe(2)
  expect(errors).not.toHaveBeenCalled()
})
