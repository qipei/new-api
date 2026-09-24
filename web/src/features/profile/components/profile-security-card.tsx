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
import { Shield, Key, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import { useDialogs } from '@/hooks/use-dialog'
import { useStatus } from '@/hooks/use-status'

import type { UserProfile } from '../types'
import { AccessTokenDialog } from './dialogs/access-token-dialog'
import { ChangePasswordDialog } from './dialogs/change-password-dialog'
import { DeleteAccountDialog } from './dialogs/delete-account-dialog'
import { SetPasswordDialog } from './dialogs/set-password-dialog'

// ============================================================================
// Profile Security Card Component
// ============================================================================

interface ProfileSecurityCardProps {
  profile: UserProfile | null
  loading: boolean
  onProfileUpdate: () => void
}

type DialogKey = 'password' | 'set-password' | 'token' | 'delete'

export function ProfileSecurityCard({
  profile,
  loading,
  onProfileUpdate,
}: ProfileSecurityCardProps) {
  const { t } = useTranslation()
  const dialogs = useDialogs<DialogKey>()
  const { status } = useStatus()

  if (loading) {
    return (
      <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
        <CardHeader className='border-b p-3 !pb-3 sm:p-5 sm:!pb-5'>
          <Skeleton className='h-6 w-32' />
          <Skeleton className='mt-2 h-4 w-48' />
        </CardHeader>
        <CardContent className='space-y-3 p-3 sm:p-5'>
          {['password', 'token', 'delete'].map((key) => (
            <Skeleton key={key} className='h-16 w-full' />
          ))}
        </CardContent>
      </Card>
    )
  }

  if (!profile) return null

  // 手机号或第三方登录创建的账号没有密码，改为提供「设置登录密码」；
  // 密码登录被管理员关闭时，设置了也无法使用，不展示该入口。
  const hasPassword = profile.has_password !== false
  const passwordLoginEnabled = status?.password_login_enabled !== false
  const passwordAction = hasPassword
    ? {
        icon: Shield,
        title: t('Change Password'),
        description: t('Update your password to keep your account secure'),
        action: () => dialogs.open('password'),
        variant: 'default' as const,
      }
    : {
        icon: Shield,
        title: t('Set Login Password'),
        description: t(
          'Set a password to also sign in with your username or email'
        ),
        action: () => dialogs.open('set-password'),
        variant: 'default' as const,
      }

  const securityActions = [
    ...(hasPassword || passwordLoginEnabled ? [passwordAction] : []),
    {
      icon: Key,
      title: t('Access Token'),
      description: t('Generate and manage your API access token'),
      action: () => dialogs.open('token'),
      variant: 'default' as const,
    },
    {
      icon: Trash2,
      title: t('Delete Account'),
      description: t('Permanently delete your account and all data'),
      action: () => dialogs.open('delete'),
      variant: 'destructive' as const,
    },
  ]

  return (
    <>
      <TitledCard
        title={t('Security')}
        description={t('Manage your security settings and account access')}
        icon={<Shield className='h-4 w-4' />}
        iconTone='success'
        disableHoverEffect
      >
        <div className='grid grid-cols-1 gap-2.5 sm:gap-3 md:grid-cols-3'>
          {securityActions.map((item) => (
            <button
              key={item.title}
              type='button'
              onClick={item.action}
              className={`flex items-center gap-3 rounded-lg border p-3 text-left md:flex-col md:gap-2 md:p-4 md:text-center ${
                item.variant === 'destructive' ? 'border-destructive/30' : ''
              }`}
            >
              <IconBadge tone='neutral' size='sm'>
                <item.icon />
              </IconBadge>
              <div className='min-w-0 md:contents'>
                <p className='text-sm font-medium'>{item.title}</p>
                <p className='text-muted-foreground line-clamp-1 text-xs md:line-clamp-none'>
                  {item.description}
                </p>
              </div>
            </button>
          ))}
        </div>
      </TitledCard>

      {/* Dialogs */}
      <ChangePasswordDialog
        open={dialogs.isOpen('password')}
        onOpenChange={(open) =>
          open ? dialogs.open('password') : dialogs.close('password')
        }
        username={profile.username}
      />

      <SetPasswordDialog
        open={dialogs.isOpen('set-password')}
        onOpenChange={(open) =>
          open ? dialogs.open('set-password') : dialogs.close('set-password')
        }
        username={profile.username}
        email={profile.email}
        onSuccess={onProfileUpdate}
      />

      <AccessTokenDialog
        open={dialogs.isOpen('token')}
        onOpenChange={(open) =>
          open ? dialogs.open('token') : dialogs.close('token')
        }
      />

      <DeleteAccountDialog
        open={dialogs.isOpen('delete')}
        onOpenChange={(open) =>
          open ? dialogs.open('delete') : dialogs.close('delete')
        }
        username={profile.username}
      />
    </>
  )
}
