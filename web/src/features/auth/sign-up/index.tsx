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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'

import { AuthLayout } from '../auth-layout'
import { SignUpForm } from './components/sign-up-form'

export function SignUp() {
  const { t } = useTranslation()
  const { status, registrationOpen, loading } = useStatus()
  const phoneLoginEnabled = Boolean(status?.phone_login_enabled)

  if (loading) {
    return (
      <AuthLayout variant='entry'>
        <p role='status'>{t('Loading...')}</p>
      </AuthLayout>
    )
  }

  // 注册关闭时直接说明情况，而不是展示一个提交后必然被后端拒绝的表单。
  // 手机号登录不受注册开关约束（新号码验证后自动建号），此时引导用户去用手机号登录/注册。
  if (!registrationOpen) {
    return (
      <AuthLayout variant='entry'>
        <div className='w-full space-y-6'>
          <h1 className='auth-entry-title'>
            {phoneLoginEnabled
              ? t('Sign up with your phone number')
              : t('Registration is closed')}
          </h1>
          <p className='text-muted-foreground text-sm'>
            {phoneLoginEnabled
              ? t(
                  'Sign in with your mobile number and an SMS code. New numbers get an account automatically after verification.'
                )
              : t(
                  'New account registration is currently closed. If you already have an account, please sign in.'
                )}
          </p>
          <Button className='w-full' render={<Link to='/sign-in' />}>
            {phoneLoginEnabled ? t('Sign in / Sign up') : t('Go to sign in')}
          </Button>
        </div>
      </AuthLayout>
    )
  }

  return (
    <AuthLayout variant='entry'>
      <div className='w-full space-y-6'>
        <h1 className='auth-entry-title'>{t('Create an account')}</h1>
        <SignUpForm />
        <p className='auth-entry-switch'>
          {t('Already have an account?')}{' '}
          <Link
            to='/sign-in'
            className='hover:text-primary font-medium underline underline-offset-4'
          >
            {t('Sign in')}
          </Link>
        </p>
      </div>
    </AuthLayout>
  )
}
