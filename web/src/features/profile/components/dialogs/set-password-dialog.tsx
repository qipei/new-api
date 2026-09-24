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
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

import { setInitialPassword } from '../../api'

interface SetPasswordDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  username: string
  email?: string
  onSuccess: () => void
}

/**
 * First-time password setup for accounts registered by phone or third-party
 * login. Afterwards the user can also sign in with username (or bound email)
 * and password.
 */
export function SetPasswordDialog(props: SetPasswordDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')

  const resetForm = () => {
    setPassword('')
    setConfirmPassword('')
  }

  const handleOpenChange = (open: boolean) => {
    if (loading) return
    props.onOpenChange(open)
    if (!open) resetForm()
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (password.length < 8 || password.length > 20) {
      toast.error(t('Password must be 8 to 20 characters'))
      return
    }
    if (password !== confirmPassword) {
      toast.error(t('Passwords do not match'))
      return
    }

    try {
      setLoading(true)
      const response = await setInitialPassword(password)
      if (!response.success) {
        toast.error(response.message || t('Failed to set password'))
        return
      }
      toast.success(t('Login password set successfully'))
      props.onOpenChange(false)
      resetForm()
      props.onSuccess()
    } catch {
      toast.error(t('Failed to set password'))
    } finally {
      setLoading(false)
    }
  }

  const formId = 'set-password-form'

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Set Login Password')}
      description={
        props.email
          ? t(
              'After setting a password you can also sign in with {{username}} or {{email}} and this password.',
              { username: props.username, email: props.email }
            )
          : t(
              'After setting a password you can also sign in with {{username}} and this password.',
              { username: props.username }
            )
      }
      contentClassName='sm:max-w-md'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => handleOpenChange(false)}
            disabled={loading}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={formId} disabled={loading}>
            {loading && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {t('Set Login Password')}
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={handleSubmit} className='space-y-4'>
        <div className='space-y-2'>
          <Label htmlFor='initialPassword'>{t('New Password')}</Label>
          <PasswordInput
            id='initialPassword'
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={loading}
            required
            minLength={8}
            maxLength={20}
            autoComplete='new-password'
          />
          <p className='text-muted-foreground text-xs'>
            {t('Password must be 8 to 20 characters')}
          </p>
        </div>

        <div className='space-y-2'>
          <Label htmlFor='initialPasswordConfirm'>
            {t('Confirm New Password')}
          </Label>
          <PasswordInput
            id='initialPasswordConfirm'
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            disabled={loading}
            required
            autoComplete='new-password'
          />
        </div>
      </form>
    </Dialog>
  )
}
