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
import { useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  nextEditableTierUid,
  parseVideoPricingTables,
  serializeVideoPricingTables,
  validateVideoPricingTables,
  type EditableVideoTable,
} from './video-pricing-core'

// CUSTOM: 视频定价矩阵设置卡片（fork 扩展）。

const MODE_LABEL_KEYS: Record<string, string> = {
  '': 'Any mode',
  t2v: 'Text to video',
  i2v: 'Image to video',
  v2v: 'Video to video',
}

const AUDIO_LABEL_KEYS: Record<string, string> = {
  '': 'Audio agnostic',
  on: 'With audio',
  off: 'Without audio',
}

const UNIT_LABEL_KEYS: Record<string, string> = {
  per_second: 'Per second',
  per_million_tokens: 'Per million tokens',
}

const ISSUE_LABEL_KEYS: Record<string, string> = {
  unit: 'invalid pricing unit',
  tier_price: 'tier price must be greater than 0',
  duplicate_tier: 'duplicate tier dimensions',
  missing_default: 'a default tier with no dimensions is required',
}

export function VideoPricingSection(props: { defaultValue: string }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const [tables, setTables] = useState<EditableVideoTable[]>(() =>
    parseVideoPricingTables(props.defaultValue)
  )
  const [savedJson, setSavedJson] = useState(() =>
    serializeVideoPricingTables(parseVideoPricingTables(props.defaultValue))
  )
  const [newModelName, setNewModelName] = useState('')

  const currentJson = serializeVideoPricingTables(tables)
  const isDirty = currentJson !== savedJson

  const patchTable = (
    index: number,
    patch: Partial<EditableVideoTable>
  ): void => {
    setTables((prev) =>
      prev.map((table, i) => (i === index ? { ...table, ...patch } : table))
    )
  }

  const patchTier = (
    tableIndex: number,
    tierIndex: number,
    patch: Partial<EditableVideoTable['tiers'][number]>
  ): void => {
    setTables((prev) =>
      prev.map((table, i) => {
        if (i !== tableIndex) return table
        return {
          ...table,
          tiers: table.tiers.map((tier, j) =>
            j === tierIndex ? { ...tier, ...patch } : tier
          ),
        }
      })
    )
  }

  const addModel = (): void => {
    const model = newModelName.trim()
    if (!model) return
    if (tables.some((table) => table.model === model)) {
      toast.error(t('Model already exists'))
      return
    }
    setTables((prev) => [
      ...prev,
      {
        model,
        unit: 'per_second',
        tiers: [
          {
            uid: nextEditableTierUid(),
            mode: '',
            resolution: '',
            audio: '',
            price: '',
          },
        ],
      },
    ])
    setNewModelName('')
  }

  const addTier = (tableIndex: number): void => {
    setTables((prev) =>
      prev.map((table, i) => {
        if (i !== tableIndex) return table
        return {
          ...table,
          tiers: [
            ...table.tiers,
            {
              uid: nextEditableTierUid(),
              mode: '',
              resolution: '',
              audio: '',
              price: '',
            },
          ],
        }
      })
    )
  }

  const onSave = async (): Promise<void> => {
    const issues = validateVideoPricingTables(tables)
    if (issues.length > 0) {
      const first = issues[0]
      toast.error(
        `${t('Invalid video pricing')}: ${first.model} — ${t(ISSUE_LABEL_KEYS[first.reason])}`
      )
      return
    }
    if (!isDirty) {
      toast.info(t('No changes to save'))
      return
    }
    await updateOption.mutateAsync({
      key: 'video_billing.price_tables',
      value: currentJson,
    })
    setSavedJson(currentJson)
  }

  return (
    <SettingsSection title={t('Video Pricing')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Configure original prices per model by generation mode, resolution tier and audio track. Models configured here bypass Model Pricing entirely; the charge is the matched tier price multiplied by the group ratio.'
        )}
      </p>
      <SettingsPageFormActions
        onSave={() => void onSave()}
        isSaving={updateOption.isPending}
        isSaveDisabled={!isDirty}
        saveLabel='Save video pricing'
      />
      <div className='space-y-6'>
        {tables.length === 0 && (
          <p className='text-muted-foreground text-sm'>
            {t('No video pricing configured yet')}
          </p>
        )}
        {tables.map((table, tableIndex) => (
          <div key={table.model} className='space-y-3 rounded-lg border p-4'>
            <div className='flex flex-wrap items-center gap-3'>
              <span className='font-mono text-sm font-medium'>
                {table.model}
              </span>
              <div className='ml-auto flex items-center gap-2'>
                <span className='text-muted-foreground text-sm'>
                  {t('Pricing unit')}
                </span>
                <Select
                  value={table.unit}
                  onValueChange={(value) => {
                    if (value) patchTable(tableIndex, { unit: value })
                  }}
                >
                  <SelectTrigger className='w-44'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {Object.entries(UNIT_LABEL_KEYS).map(
                      ([value, labelKey]) => (
                        <SelectItem key={value} value={value}>
                          {t(labelKey)}
                        </SelectItem>
                      )
                    )}
                  </SelectContent>
                </Select>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  aria-label={`${t('Delete')} ${table.model}`}
                  onClick={() =>
                    setTables((prev) =>
                      prev.filter((_, i) => i !== tableIndex)
                    )
                  }
                >
                  <Trash2 className='size-4' />
                </Button>
              </div>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Prices are original prices in USD (same convention as Model Pricing). A tier with no dimensions is the default tier; billing multiplies the matched tier price by the group ratio only.'
              )}
            </p>
            <div className='overflow-x-auto'>
              <table className='w-full min-w-[560px] text-sm'>
                <thead>
                  <tr className='text-muted-foreground text-left'>
                    <th className='py-1 pr-3 font-normal'>
                      {t('Generation mode')}
                    </th>
                    <th className='py-1 pr-3 font-normal'>{t('Resolution')}</th>
                    <th className='py-1 pr-3 font-normal'>{t('Audio track')}</th>
                    <th className='py-1 pr-3 font-normal'>{t('Price')}</th>
                    <th className='w-10 py-1' />
                  </tr>
                </thead>
                <tbody>
                  {table.tiers.map((tier, tierIndex) => (
                    <tr key={tier.uid}>
                      <td className='py-1 pr-3'>
                        <Select
                          value={tier.mode || 'any'}
                          onValueChange={(value) =>
                            patchTier(tableIndex, tierIndex, {
                              mode: value && value !== 'any' ? value : '',
                            })
                          }
                        >
                          <SelectTrigger className='w-36'>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {Object.entries(MODE_LABEL_KEYS).map(
                              ([value, labelKey]) => (
                                <SelectItem
                                  key={value || 'any'}
                                  value={value || 'any'}
                                >
                                  {t(labelKey)}
                                </SelectItem>
                              )
                            )}
                          </SelectContent>
                        </Select>
                      </td>
                      <td className='py-1 pr-3'>
                        <Input
                          className='w-28'
                          placeholder={t('e.g. 1080p')}
                          value={tier.resolution}
                          aria-label={t('Resolution')}
                          onChange={(e) =>
                            patchTier(tableIndex, tierIndex, {
                              resolution: e.target.value,
                            })
                          }
                        />
                      </td>
                      <td className='py-1 pr-3'>
                        <Select
                          value={tier.audio || 'any'}
                          onValueChange={(value) =>
                            patchTier(tableIndex, tierIndex, {
                              audio: value && value !== 'any' ? value : '',
                            })
                          }
                        >
                          <SelectTrigger className='w-36'>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {Object.entries(AUDIO_LABEL_KEYS).map(
                              ([value, labelKey]) => (
                                <SelectItem
                                  key={value || 'any'}
                                  value={value || 'any'}
                                >
                                  {t(labelKey)}
                                </SelectItem>
                              )
                            )}
                          </SelectContent>
                        </Select>
                      </td>
                      <td className='py-1 pr-3'>
                        <Input
                          className='w-28'
                          inputMode='decimal'
                          value={tier.price}
                          aria-label={t('Price')}
                          onChange={(e) =>
                            patchTier(tableIndex, tierIndex, {
                              price: e.target.value,
                            })
                          }
                        />
                      </td>
                      <td className='py-1'>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          aria-label={t('Remove')}
                          onClick={() =>
                            patchTable(tableIndex, {
                              tiers: table.tiers.filter(
                                (_, j) => j !== tierIndex
                              ),
                            })
                          }
                        >
                          <Trash2 className='size-4' />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => addTier(tableIndex)}
            >
              <Plus className='mr-1 size-4' />
              {t('Add tier')}
            </Button>
          </div>
        ))}
        <div className='flex items-center gap-2'>
          <Input
            className='w-72'
            placeholder={t('Model name')}
            value={newModelName}
            aria-label={t('Model name')}
            onChange={(e) => setNewModelName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addModel()
              }
            }}
          />
          <Button type='button' variant='outline' onClick={addModel}>
            <Plus className='mr-1 size-4' />
            {t('Add model')}
          </Button>
        </div>
      </div>
    </SettingsSection>
  )
}
