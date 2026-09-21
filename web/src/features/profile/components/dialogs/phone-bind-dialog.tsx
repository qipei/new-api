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
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { bindPhone, sendBindPhoneCode } from '@/features/auth/api'
import { useSMSVerification } from '@/features/auth/hooks/use-sms-verification'

interface PhoneBindDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentPhone?: string
  onSuccess: () => void
}

export function PhoneBindDialog(props: PhoneBindDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [phone, setPhone] = useState('')
  const [code, setCode] = useState('')
  const {
    isSending,
    secondsLeft,
    isActive: isCountingDown,
    sendCode,
  } = useSMSVerification({ send: sendBindPhoneCode })

  const handleBind = async () => {
    if (!phone || !code) {
      toast.error(t('Please enter the mobile number and verification code'))
      return
    }

    try {
      setLoading(true)
      const response = await bindPhone({
        phone: phone.replaceAll(/\D/g, ''),
        code: code.trim(),
      })
      if (!response.success) {
        toast.error(response.message || t('Failed to bind the mobile number'))
        return
      }
      toast.success(t('Mobile number bound successfully'))
      props.onOpenChange(false)
      props.onSuccess()
      setPhone('')
      setCode('')
    } catch {
      toast.error(t('Failed to bind the mobile number'))
    } finally {
      setLoading(false)
    }
  }

  const handleOpenChange = (open: boolean) => {
    if (loading) return
    props.onOpenChange(open)
    if (!open) {
      setPhone('')
      setCode('')
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Bind Mobile Number')}
      description={
        props.currentPhone
          ? t('Current number: {{phone}}. Enter a new number to change.', {
              phone: props.currentPhone,
            })
          : t('Bind a mobile number so you can sign in with an SMS code.')
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
          <Button
            type='button'
            onClick={handleBind}
            disabled={loading || !phone || !code}
          >
            {loading && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {loading ? t('Binding...') : t('Bind Mobile Number')}
          </Button>
        </>
      }
    >
      <div className='space-y-4 py-4'>
        <div className='space-y-2'>
          <Label htmlFor='phone'>{t('Mobile number')}</Label>
          <Input
            id='phone'
            inputMode='numeric'
            autoComplete='tel'
            maxLength={11}
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            placeholder={t('Enter your mobile number')}
            disabled={loading}
          />
        </div>

        <div className='space-y-2'>
          <Label htmlFor='phone-code'>{t('Verification Code')}</Label>
          <div className='flex gap-2'>
            <Input
              id='phone-code'
              inputMode='numeric'
              autoComplete='one-time-code'
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder={t('Enter code')}
              disabled={loading}
              maxLength={8}
            />
            <Button
              type='button'
              variant='outline'
              onClick={() => sendCode(phone)}
              disabled={isSending || isCountingDown || !phone}
            >
              {isCountingDown ? `${secondsLeft}s` : t('Send')}
            </Button>
          </div>
        </div>
      </div>
    </Dialog>
  )
}
