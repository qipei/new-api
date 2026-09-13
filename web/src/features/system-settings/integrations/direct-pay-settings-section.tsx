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
import { Check, Copy, ShieldAlert } from 'lucide-react'
import * as React from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

export interface DirectPaySettingsValues {
  AlipayEnabled: boolean
  AlipayAppID: string
  AlipayPrivateKey: string
  AlipayPublicKey: string
  AlipaySellerID: string
  AlipaySandbox: boolean
  WechatPayEnabled: boolean
  WechatPayAppID: string
  WechatPayMchID: string
  WechatPayCertSerialNo: string
  WechatPayPrivateKey: string
  WechatPayAPIv3Key: string
  WechatPayPublicKey: string
  WechatPayPublicKeyID: string
}

interface Props {
  values: DirectPaySettingsValues
  onValueChange: <K extends keyof DirectPaySettingsValues>(
    key: K,
    value: DirectPaySettingsValues[K]
  ) => void
  /** Origin used to derive the callback URLs shown for copying. */
  callbackOrigin: string
}

/** Fixed paths; the operator only maintains the origin in General settings. */
const ALIPAY_NOTIFY_PATH = '/api/alipay/notify'
const WECHAT_NOTIFY_PATH = '/api/wechat/notify'

function CallbackUrlField(props: { label: string; url: string; hint: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = React.useState(false)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(props.url)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className='space-y-2'>
      <Label>{props.label}</Label>
      <div className='flex gap-2'>
        <Input value={props.url} readOnly className='font-mono text-xs' />
        <Button
          type='button'
          variant='outline'
          size='icon'
          onClick={handleCopy}
          aria-label={t('Copy')}
        >
          {copied ? (
            <Check className='h-4 w-4' />
          ) : (
            <Copy className='h-4 w-4' />
          )}
        </Button>
      </div>
      <p className='text-muted-foreground text-sm'>{props.hint}</p>
    </div>
  )
}

/**
 * Official direct-connect gateways: Alipay PC website payment and WeChat Native.
 *
 * They share one tab because operationally they are one decision: the same
 * business licence, enabled and disabled together, unlike the mutually
 * unrelated third parties that each get their own tab.
 *
 * Secrets are write-only. GET /api/option/ strips every key ending in Key or
 * Secret, so a blank field means "leave unchanged" rather than "clear".
 */
export function DirectPaySettingsSection(props: Props) {
  const { t } = useTranslation()
  const origin = props.callbackOrigin.replace(/\/+$/, '')

  return (
    <div className='space-y-6'>
      <div>
        <h3 className='text-lg font-medium'>{t('Official Direct Payment')}</h3>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Collect directly into your own Alipay merchant account and WeChat Pay merchant ID, without an aggregator in between.'
          )}
        </p>
      </div>

      <Alert>
        <ShieldAlert className='h-4 w-4' />
        <AlertTitle>{t('Before enabling')}</AlertTitle>
        <AlertDescription>
          {t(
            'Both gateways require a signed contract for the matching product: Alipay PC website payment, and WeChat Native payment. Enabling them here without that contract will fail at checkout.'
          )}
        </AlertDescription>
      </Alert>

      <div className='space-y-4 rounded-lg border p-4'>
        <div className='flex items-center justify-between'>
          <div>
            <Label className='text-base'>{t('Alipay')}</Label>
            <p className='text-muted-foreground text-sm'>
              {t('PC website payment, redirects to the Alipay checkout page.')}
            </p>
          </div>
          <Switch
            checked={props.values.AlipayEnabled}
            onCheckedChange={(checked) =>
              props.onValueChange('AlipayEnabled', checked)
            }
          />
        </div>

        <div className='grid gap-4 md:grid-cols-2'>
          <div className='space-y-2'>
            <Label>{t('Alipay AppID')}</Label>
            <Input
              value={props.values.AlipayAppID}
              autoComplete='off'
              placeholder='2021000000000000'
              onChange={(event) =>
                props.onValueChange('AlipayAppID', event.target.value)
              }
            />
            <p className='text-muted-foreground text-sm'>
              {t('AppID of the app on the Alipay open platform')}
            </p>
          </div>

          <div className='space-y-2'>
            <Label>{t('Alipay seller ID')}</Label>
            <Input
              value={props.values.AlipaySellerID}
              autoComplete='off'
              placeholder='2088000000000000'
              onChange={(event) =>
                props.onValueChange('AlipaySellerID', event.target.value)
              }
            />
            <p className='text-muted-foreground text-sm'>
              {t(
                'Payee account ID, 16 digits starting with 2088. Alipay requires callbacks to be checked against it.'
              )}
            </p>
          </div>
        </div>

        <div className='space-y-2'>
          <Label>{t('Alipay app private key')}</Label>
          <Textarea
            value={props.values.AlipayPrivateKey}
            rows={3}
            autoComplete='new-password'
            placeholder={t('Leave blank unless rotating the secret')}
            className='font-mono text-xs'
            onChange={(event) =>
              props.onValueChange('AlipayPrivateKey', event.target.value)
            }
          />
          <p className='text-muted-foreground text-sm'>
            {t('RSA2 private key, PKCS#1 or PKCS#8, used to sign requests')}
          </p>
        </div>

        <div className='space-y-2'>
          <Label>{t('Alipay public key')}</Label>
          <Textarea
            value={props.values.AlipayPublicKey}
            rows={3}
            autoComplete='new-password'
            placeholder={t('Leave blank unless rotating the secret')}
            className='font-mono text-xs'
            onChange={(event) =>
              props.onValueChange('AlipayPublicKey', event.target.value)
            }
          />
          <p className='text-muted-foreground text-sm'>
            {t('Used to verify the signature on asynchronous notifications')}
          </p>
        </div>

        <CallbackUrlField
          label={t('Alipay notification URL')}
          url={`${origin}${ALIPAY_NOTIFY_PATH}`}
          hint={t(
            'Paste this into the app gateway field on the Alipay open platform. It is derived from the callback origin in General settings.'
          )}
        />

        <div className='flex items-center justify-between rounded-lg border p-3'>
          <div>
            <Label>{t('Alipay sandbox')}</Label>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Sandbox supports balance payment only, and final behaviour must be verified in production.'
              )}
            </p>
          </div>
          <Switch
            checked={props.values.AlipaySandbox}
            onCheckedChange={(checked) =>
              props.onValueChange('AlipaySandbox', checked)
            }
          />
        </div>
      </div>

      <div className='space-y-4 rounded-lg border p-4'>
        <div className='flex items-center justify-between'>
          <div>
            <Label className='text-base'>{t('WeChat Pay')}</Label>
            <p className='text-muted-foreground text-sm'>
              {t('Native payment, shows a QR code for the user to scan.')}
            </p>
          </div>
          <Switch
            checked={props.values.WechatPayEnabled}
            onCheckedChange={(checked) =>
              props.onValueChange('WechatPayEnabled', checked)
            }
          />
        </div>

        <Alert>
          <ShieldAlert className='h-4 w-4' />
          <AlertTitle>{t('Turn on public key switching first')}</AlertTitle>
          <AlertDescription>
            {t(
              'After requesting the WeChat Pay public key, switching is off by default. Until you turn it on in the merchant platform, WeChat keeps signing with the platform certificate and every verification here will fail.'
            )}
          </AlertDescription>
        </Alert>

        <div className='grid gap-4 md:grid-cols-2'>
          <div className='space-y-2'>
            <Label>{t('WeChat AppID')}</Label>
            <Input
              value={props.values.WechatPayAppID}
              autoComplete='off'
              placeholder='wx0000000000000000'
              onChange={(event) =>
                props.onValueChange('WechatPayAppID', event.target.value)
              }
            />
            <p className='text-muted-foreground text-sm'>
              {t(
                'A verified official account, mini program or mobile app AppID bound to the merchant ID. Website apps cannot be used for payment.'
              )}
            </p>
          </div>

          <div className='space-y-2'>
            <Label>{t('WeChat merchant ID')}</Label>
            <Input
              value={props.values.WechatPayMchID}
              autoComplete='off'
              placeholder='1600000000'
              onChange={(event) =>
                props.onValueChange('WechatPayMchID', event.target.value)
              }
            />
            <p className='text-muted-foreground text-sm'>
              {t('Numeric merchant ID, not the AppID')}
            </p>
          </div>
        </div>

        <div className='grid gap-4 md:grid-cols-2'>
          <div className='space-y-2'>
            <Label>{t('Merchant certificate serial number')}</Label>
            <Input
              value={props.values.WechatPayCertSerialNo}
              autoComplete='off'
              className='font-mono text-xs'
              onChange={(event) =>
                props.onValueChange('WechatPayCertSerialNo', event.target.value)
              }
            />
          </div>

          <div className='space-y-2'>
            <Label>{t('WeChat Pay public key ID')}</Label>
            <Input
              value={props.values.WechatPayPublicKeyID}
              autoComplete='off'
              placeholder='PUB_KEY_ID_...'
              className='font-mono text-xs'
              onChange={(event) =>
                props.onValueChange('WechatPayPublicKeyID', event.target.value)
              }
            />
            <p className='text-muted-foreground text-sm'>
              {t(
                'A short identifier shown next to the key download, not the key contents. Keep the PUB_KEY_ID_ prefix, removing it breaks verification.'
              )}
            </p>
          </div>
        </div>

        <div className='space-y-2'>
          <Label>{t('Merchant API private key')}</Label>
          <Textarea
            value={props.values.WechatPayPrivateKey}
            rows={3}
            autoComplete='new-password'
            placeholder={t('Leave blank unless rotating the secret')}
            className='font-mono text-xs'
            onChange={(event) =>
              props.onValueChange('WechatPayPrivateKey', event.target.value)
            }
          />
          <p className='text-muted-foreground text-sm'>
            {t('Contents of apiclient_key.pem')}
          </p>
        </div>

        <div className='space-y-2'>
          <Label>{t('APIv3 key')}</Label>
          <Input
            type='password'
            value={props.values.WechatPayAPIv3Key}
            autoComplete='new-password'
            placeholder={t('Leave blank unless rotating the secret')}
            onChange={(event) =>
              props.onValueChange('WechatPayAPIv3Key', event.target.value)
            }
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              '32-character key used to decrypt callbacks. WeChat sends no callbacks at all until it is set.'
            )}
          </p>
        </div>

        <div className='space-y-2'>
          <Label>{t('WeChat Pay public key')}</Label>
          <Textarea
            value={props.values.WechatPayPublicKey}
            rows={3}
            autoComplete='new-password'
            placeholder={t('Leave blank unless rotating the secret')}
            className='font-mono text-xs'
            onChange={(event) =>
              props.onValueChange('WechatPayPublicKey', event.target.value)
            }
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Full contents of pub_key.pem, including the BEGIN and END lines. This is the key itself, not the public key ID.'
            )}
          </p>
        </div>

        <CallbackUrlField
          label={t('WeChat notification URL')}
          url={`${origin}${WECHAT_NOTIFY_PATH}`}
          hint={t(
            'Passed on every order request, so there is nothing to configure in the merchant platform. Allow the WeChat Pay callback IP ranges if a firewall is in front.'
          )}
        />
      </div>
    </div>
  )
}
