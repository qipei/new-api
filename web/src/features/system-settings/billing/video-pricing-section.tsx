import { Plus, Trash2 } from 'lucide-react'
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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'

import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  editableBucketsFromTemplate,
  IMAGE_RESOLUTION_TEMPLATES,
} from './image-resolution-templates'
import {
  formatVideoPricingJson,
  nextEditableBucketUid,
  nextEditableTierUid,
  parseVideoPricingJsonDraft,
  parseVideoPricingTables,
  serializeVideoPricingTables,
  validateVideoPricingTables,
  type EditableVideoTable,
} from './video-pricing-core'

// CUSTOM: 视频定价矩阵设置卡片（fork 扩展）。

const VIDEO_MODE_LABEL_KEYS: Record<string, string> = {
  '': 'Any mode',
  t2v: 'Text to video',
  i2v: 'Image to video',
  v2v: 'Video to video',
}

const IMAGE_MODE_LABEL_KEYS: Record<string, string> = {
  '': 'Any mode',
  t2i: 'Text to image',
  i2i: 'Image to image',
}

function modeLabelKeysForUnit(unit: string): Record<string, string> {
  return unit === 'per_image' ? IMAGE_MODE_LABEL_KEYS : VIDEO_MODE_LABEL_KEYS
}

const AUDIO_LABEL_KEYS: Record<string, string> = {
  '': 'Audio agnostic',
  on: 'With audio',
  off: 'Without audio',
}

const UNIT_LABEL_KEYS: Record<string, string> = {
  per_second: 'Per second',
  per_million_tokens: 'Per million tokens',
  per_image: 'Per image',
}

const ISSUE_LABEL_KEYS: Record<string, string> = {
  unit: 'invalid pricing unit',
  tier_price: 'tier price must be greater than 0',
  duplicate_tier: 'duplicate tier dimensions',
  missing_default: 'a default tier with no dimensions is required',
  input_price: 'input prices must be greater than 0 when set',
  pixel_range: 'pixel limits must be positive integers in ascending order',
  resolution_bucket: 'resolution bucket names and sizes must be valid',
  duplicate_bucket_size:
    'the same image size cannot belong to multiple resolution buckets',
}

type PricingEditorMode = 'visual' | 'json'

export function VideoPricingSection(props: { defaultValue: string }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const [tables, setTables] = useState<EditableVideoTable[]>(() =>
    parseVideoPricingTables(props.defaultValue)
  )
  const [savedJson, setSavedJson] = useState(() =>
    serializeVideoPricingTables(parseVideoPricingTables(props.defaultValue))
  )
  const [editorMode, setEditorMode] = useState<PricingEditorMode>('visual')
  const [jsonDraft, setJsonDraft] = useState(() =>
    formatVideoPricingJson(props.defaultValue)
  )
  const [newModelName, setNewModelName] = useState('')

  const currentJson = serializeVideoPricingTables(tables)
  const parsedJsonDraft = useMemo(
    () => parseVideoPricingJsonDraft(jsonDraft),
    [jsonDraft]
  )
  const jsonDraftValue =
    parsedJsonDraft.error === null
      ? serializeVideoPricingTables(parsedJsonDraft.tables)
      : null
  const isDirty =
    editorMode === 'visual'
      ? currentJson !== savedJson
      : jsonDraftValue !== savedJson

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

  const patchResolutionBucket = (
    tableIndex: number,
    bucketIndex: number,
    patch: Partial<EditableVideoTable['resolutionBuckets'][number]>
  ): void => {
    setTables((prev) =>
      prev.map((table, i) => {
        if (i !== tableIndex) return table
        return {
          ...table,
          resolutionBuckets: table.resolutionBuckets.map((bucket, j) =>
            j === bucketIndex ? { ...bucket, ...patch } : bucket
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
        inputImagePrice: '',
        inputTokenPrice: '',
        resolutionBuckets: [],
        tiers: [
          {
            uid: nextEditableTierUid(),
            mode: '',
            resolution: '',
            audio: '',
            minPixels: '',
            maxPixels: '',
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
              minPixels: '',
              maxPixels: '',
              price: '',
            },
          ],
        }
      })
    )
  }

  const switchEditorMode = (nextMode: PricingEditorMode): void => {
    if (nextMode === editorMode) return
    if (nextMode === 'json') {
      setJsonDraft(formatVideoPricingJson(currentJson))
      setEditorMode(nextMode)
      return
    }
    if (parsedJsonDraft.error !== null) {
      toast.error(
        t(
          parsedJsonDraft.error === 'json'
            ? 'Invalid JSON'
            : 'Invalid video pricing'
        )
      )
      return
    }
    setTables(parsedJsonDraft.tables)
    setJsonDraft(
      formatVideoPricingJson(
        serializeVideoPricingTables(parsedJsonDraft.tables)
      )
    )
    setEditorMode(nextMode)
  }

  const onSave = async (): Promise<void> => {
    let nextTables = tables
    if (editorMode === 'json') {
      if (parsedJsonDraft.error !== null) {
        toast.error(
          t(
            parsedJsonDraft.error === 'json'
              ? 'Invalid JSON'
              : 'Invalid video pricing'
          )
        )
        return
      }
      nextTables = parsedJsonDraft.tables
    }
    const issues = validateVideoPricingTables(nextTables)
    if (issues.length > 0) {
      const first = issues[0]
      toast.error(
        `${t('Invalid video pricing')}: ${first.model} — ${t(ISSUE_LABEL_KEYS[first.reason])}`
      )
      return
    }

    const nextJson = serializeVideoPricingTables(nextTables)
    if (!isDirty) {
      toast.info(t('No changes to save'))
      return
    }
    await updateOption.mutateAsync({
      key: 'video_billing.price_tables',
      value: nextJson,
    })
    setTables(nextTables)
    setSavedJson(nextJson)
    setJsonDraft(formatVideoPricingJson(nextJson))
  }

  return (
    <SettingsSection title={t('Video and Image Pricing')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Configure original prices per model by generation mode, resolution or pixel range, and audio track for video models. Models configured here bypass Model Pricing entirely; the charge is the matched tier price multiplied by the group ratio.'
        )}
      </p>
      <SettingsPageFormActions
        onSave={() => void onSave()}
        isSaving={updateOption.isPending}
        isSaveDisabled={!isDirty}
        saveLabel='Save video pricing'
      />
      <Tabs
        value={editorMode}
        onValueChange={(value) => switchEditorMode(value as PricingEditorMode)}
      >
        <TabsList>
          <TabsTrigger value='visual'>{t('Visual')}</TabsTrigger>
          <TabsTrigger value='json'>{t('JSON')}</TabsTrigger>
        </TabsList>

        <TabsContent value='visual' className='mt-4'>
          <div className='space-y-6'>
            {tables.length === 0 && (
              <p className='text-muted-foreground text-sm'>
                {t('No video pricing configured yet')}
              </p>
            )}
            {tables.map((table, tableIndex) => (
              <div
                key={table.model}
                className='space-y-3 rounded-lg border p-4'
              >
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
                {table.unit === 'per_image' && (
                  <div className='space-y-4'>
                    <div className='flex flex-wrap items-center gap-4'>
                      <div className='flex items-center gap-2'>
                        <span className='text-muted-foreground text-sm'>
                          {t('Input image price')}
                        </span>
                        <Input
                          className='w-28'
                          inputMode='decimal'
                          placeholder={t('Optional')}
                          value={table.inputImagePrice}
                          aria-label={`${table.model} ${t('Input image price')}`}
                          onChange={(e) =>
                            patchTable(tableIndex, {
                              inputImagePrice: e.target.value,
                            })
                          }
                        />
                      </div>
                      <div className='flex items-center gap-2'>
                        <span className='text-muted-foreground text-sm'>
                          {t('Input token price')}
                        </span>
                        <Input
                          className='w-28'
                          inputMode='decimal'
                          placeholder={t('Optional')}
                          value={table.inputTokenPrice}
                          aria-label={`${table.model} ${t('Input token price')}`}
                          onChange={(e) =>
                            patchTable(tableIndex, {
                              inputTokenPrice: e.target.value,
                            })
                          }
                        />
                      </div>
                      <span className='text-muted-foreground/70 text-xs'>
                        {t(
                          'Optional additive components: per input image, and per million prompt tokens.'
                        )}
                      </span>
                    </div>
                    <div className='space-y-2 rounded-md border p-3'>
                      <div className='flex flex-wrap items-center gap-2'>
                        <div>
                          <p className='text-sm font-medium'>
                            {t('Resolution bucket definitions')}
                          </p>
                          <p className='text-muted-foreground text-xs'>
                            {t(
                              'Exact image sizes match these model-specific buckets before the generic short-edge rule. Width and height order is ignored.'
                            )}
                          </p>
                        </div>
                        <Select
                          onValueChange={(value) => {
                            if (typeof value !== 'string') return
                            const template = IMAGE_RESOLUTION_TEMPLATES[value]
                            if (!template) return
                            patchTable(tableIndex, {
                              resolutionBuckets:
                                editableBucketsFromTemplate(template),
                            })
                          }}
                        >
                          <SelectTrigger
                            className='ml-auto w-72'
                            aria-label={`${table.model} ${t('Apply resolution template')}`}
                          >
                            <SelectValue
                              placeholder={t('Apply resolution template')}
                            />
                          </SelectTrigger>
                          <SelectContent>
                            {Object.entries(IMAGE_RESOLUTION_TEMPLATES).map(
                              ([value, template]) => (
                                <SelectItem key={value} value={value}>
                                  {t(template.labelKey)}
                                </SelectItem>
                              )
                            )}
                          </SelectContent>
                        </Select>
                      </div>
                      {table.resolutionBuckets.map((bucket, bucketIndex) => (
                        <div
                          key={bucket.uid}
                          className='flex items-start gap-2'
                        >
                          <Input
                            className='w-24 shrink-0'
                            placeholder={t('Bucket name')}
                            value={bucket.name}
                            aria-label={t('Bucket name')}
                            onChange={(event) =>
                              patchResolutionBucket(tableIndex, bucketIndex, {
                                name: event.target.value,
                              })
                            }
                          />
                          <Textarea
                            className='min-h-10 flex-1'
                            rows={2}
                            placeholder={t(
                              'Exact sizes separated by commas, e.g. 1024x1024, 1920x1088'
                            )}
                            value={bucket.sizes}
                            aria-label={`${bucket.name || t('Bucket name')} ${t('Exact image sizes')}`}
                            onChange={(event) =>
                              patchResolutionBucket(tableIndex, bucketIndex, {
                                sizes: event.target.value,
                              })
                            }
                          />
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            aria-label={t('Remove resolution bucket')}
                            onClick={() =>
                              patchTable(tableIndex, {
                                resolutionBuckets:
                                  table.resolutionBuckets.filter(
                                    (_, index) => index !== bucketIndex
                                  ),
                              })
                            }
                          >
                            <Trash2 className='size-4' />
                          </Button>
                        </div>
                      ))}
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() =>
                          patchTable(tableIndex, {
                            resolutionBuckets: [
                              ...table.resolutionBuckets,
                              {
                                uid: nextEditableBucketUid(),
                                name: '',
                                sizes: '',
                              },
                            ],
                          })
                        }
                      >
                        <Plus className='mr-1 size-4' />
                        {t('Add resolution bucket')}
                      </Button>
                    </div>
                  </div>
                )}
                <div className='overflow-x-auto'>
                  <table
                    className={
                      table.unit === 'per_image'
                        ? 'w-full min-w-[800px] text-sm'
                        : 'w-full min-w-[560px] text-sm'
                    }
                  >
                    <thead>
                      <tr className='text-muted-foreground text-left'>
                        <th className='py-1 pr-3 font-normal'>
                          {t('Generation mode')}
                        </th>
                        <th className='py-1 pr-3 font-normal'>
                          {t('Resolution')}
                        </th>
                        {table.unit === 'per_image' ? (
                          <>
                            <th className='py-1 pr-3 font-normal'>
                              {t('Minimum pixels')}
                            </th>
                            <th className='py-1 pr-3 font-normal'>
                              {t('Maximum pixels')}
                            </th>
                          </>
                        ) : (
                          <th className='py-1 pr-3 font-normal'>
                            {t('Audio track')}
                          </th>
                        )}
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
                                {Object.entries(
                                  modeLabelKeysForUnit(table.unit)
                                ).map(([value, labelKey]) => (
                                  <SelectItem
                                    key={value || 'any'}
                                    value={value || 'any'}
                                  >
                                    {t(labelKey)}
                                  </SelectItem>
                                ))}
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
                          {table.unit === 'per_image' ? (
                            <>
                              <td className='py-1 pr-3'>
                                <Input
                                  className='w-36'
                                  inputMode='numeric'
                                  placeholder={t('Optional')}
                                  value={tier.minPixels}
                                  aria-label={t('Minimum pixels')}
                                  onChange={(e) =>
                                    patchTier(tableIndex, tierIndex, {
                                      minPixels: e.target.value,
                                    })
                                  }
                                />
                              </td>
                              <td className='py-1 pr-3'>
                                <Input
                                  className='w-36'
                                  inputMode='numeric'
                                  placeholder={t('Optional')}
                                  value={tier.maxPixels}
                                  aria-label={t('Maximum pixels')}
                                  onChange={(e) =>
                                    patchTier(tableIndex, tierIndex, {
                                      maxPixels: e.target.value,
                                    })
                                  }
                                />
                              </td>
                            </>
                          ) : (
                            <td className='py-1 pr-3'>
                              <Select
                                value={tier.audio || 'any'}
                                onValueChange={(value) =>
                                  patchTier(tableIndex, tierIndex, {
                                    audio:
                                      value && value !== 'any' ? value : '',
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
                          )}
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
        </TabsContent>

        <TabsContent value='json' className='mt-4'>
          <JsonCodeEditor
            data-slot='video-pricing-json-editor'
            value={jsonDraft}
            onChange={setJsonDraft}
            heightClassName='h-[32rem] min-h-80 max-h-[70vh]'
            ariaLabel={t('Video and Image Pricing')}
          />
        </TabsContent>
      </Tabs>
    </SettingsSection>
  )
}
