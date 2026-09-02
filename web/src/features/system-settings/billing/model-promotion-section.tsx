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
import { useQuery } from '@tanstack/react-query'
// CUSTOM: 限时活动（fork 扩展）。整块自成一个 section，只在 section-registry 里
// 挂一行，避免把新字段穿过上游的 ratio-settings-card → model-ratio-form 那条链路。
import { Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { getPricing } from '@/features/pricing/api'

import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { parseJsonRecord } from './group-billing-expr-core'
import {
  effectiveDiscounts,
  MODEL_PROMOTIONS_OPTION_KEY,
  parsePromotions,
  PROMOTION_DEFAULT_TZ,
  promotionPhase,
  serializePromotions,
  todayInZone,
  validatePromotion,
  type ModelPromotion,
} from './model-promotion-core'
import { PromotionModelPicker } from './promotion-model-picker'

interface ModelPromotionSectionProps {
  /** billing_setting.model_promotions 的 JSON 字符串 */
  defaultValue: string
  /** ModelRatio 的 JSON，提供可选模型名 */
  modelRatio: string
  /** ModelPrice 的 JSON，按次计费的模型也要能配活动 */
  modelPrice: string
  /** GroupRatio 的 JSON，提供分组名和折扣叠加预览 */
  groupRatio: string
}

const PHASE_TONE = {
  live: 'text-emerald-600 dark:text-emerald-400',
  upcoming: 'text-muted-foreground',
  ended: 'text-muted-foreground line-through',
} as const

const PHASE_LABEL = {
  live: 'In progress',
  upcoming: 'Not started',
  ended: 'Ended',
} as const

/** 活动没有服务端 id，列表里增删又会让下标错位，所以进来就发一个客户端 key。 */
type PromotionRow = ModelPromotion & { uid: string }

let uidCounter = 0
function withUid(promotion: ModelPromotion): PromotionRow {
  uidCounter += 1
  return { ...promotion, uid: `p${uidCounter}` }
}

export function ModelPromotionSection(props: ModelPromotionSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [savedJson, setSavedJson] = useState(props.defaultValue || '{}')
  const [promotions, setPromotions] = useState<Record<string, PromotionRow[]>>(
    () => {
      const parsed = parsePromotions(props.defaultValue)
      return Object.fromEntries(
        Object.entries(parsed).map(([model, list]) => [
          model,
          list.map(withUid),
        ])
      )
    }
  )
  const [pendingModel, setPendingModel] = useState('')

  // 只列模型广场上真实存在的模型：给没上架的模型配活动没有意义。接口取不到时
  // 退回按倍率/价格表配置过的模型，免得整块选择器变空。
  const { data: pricingData } = useQuery({
    queryKey: ['pricing-models-for-promotions'],
    queryFn: getPricing,
    staleTime: 60_000,
  })
  const modelNames = useMemo(() => {
    const listed = (pricingData?.data || [])
      .map((item) => item.model_name)
      .filter(Boolean)
    if (listed.length > 0) {
      return [...new Set(listed)].sort((a, b) => a.localeCompare(b))
    }
    const names = new Set([
      ...Object.keys(parseJsonRecord<number>(props.modelRatio)),
      ...Object.keys(parseJsonRecord<number>(props.modelPrice)),
    ])
    return [...names].sort((a, b) => a.localeCompare(b))
  }, [pricingData, props.modelRatio, props.modelPrice])

  const groupRatios = useMemo(
    () => parseJsonRecord<number>(props.groupRatio) as Record<string, number>,
    [props.groupRatio]
  )
  const groupNames = useMemo(
    () =>
      Object.keys(groupRatios)
        .filter((group) => group !== 'auto')
        .sort((a, b) => a.localeCompare(b)),
    [groupRatios]
  )

  const nextJson = useMemo(() => serializePromotions(promotions), [promotions])
  const isDirty = nextJson !== savedJson

  const models = useMemo(
    () => Object.keys(promotions).sort((a, b) => a.localeCompare(b)),
    [promotions]
  )

  const mutate = (
    model: string,
    index: number,
    patch: Partial<ModelPromotion>
  ) => {
    setPromotions((prev) => {
      const list = [...(prev[model] || [])]
      list[index] = { ...list[index], ...patch }
      return { ...prev, [model]: list }
    })
  }

  const addPromotion = (model: string) => {
    const today = todayInZone(PROMOTION_DEFAULT_TZ)
    setPromotions((prev) => ({
      ...prev,
      [model]: [
        ...(prev[model] || []),
        withUid({
          name: '',
          start: today,
          end: today,
          tz: PROMOTION_DEFAULT_TZ,
          ratio: 0.5,
          groups: [],
        }),
      ],
    }))
  }

  const removePromotion = (model: string, index: number) => {
    setPromotions((prev) => {
      const list = (prev[model] || []).filter((_, i) => i !== index)
      const next = { ...prev }
      if (list.length === 0) delete next[model]
      else next[model] = list
      return next
    })
  }

  const toggleGroup = (model: string, index: number, group: string) => {
    const current = promotions[model]?.[index]?.groups || []
    const groups = current.includes(group)
      ? current.filter((g) => g !== group)
      : [...current, group]
    mutate(model, index, { groups })
  }

  const onSave = async () => {
    if (!isDirty) {
      toast.info(t('No changes to save'))
      return
    }
    for (const [model, list] of Object.entries(promotions)) {
      for (const promotion of list) {
        const issue = validatePromotion(promotion)
        if (issue) {
          toast.error(`${model}: ${t(issue)}`)
          return
        }
      }
    }
    await updateOption.mutateAsync({
      key: MODEL_PROMOTIONS_OPTION_KEY,
      value: nextJson,
    })
    setSavedJson(nextJson)
  }

  return (
    <SettingsSection title={t('Limited-time Promotions')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'A promotion multiplies the final price for the given dates. It works the same for ratio-priced and dynamically-priced models, and expires on its own — nothing has to be restored afterwards.'
        )}
      </p>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Cost protection still applies: if the promoted price falls below the upstream cost, the difference is charged back to break even. A promotion therefore only takes effect while the discounted price stays above what the upstream charges.'
        )}
      </p>
      <SettingsPageFormActions
        onSave={() => void onSave()}
        isSaving={updateOption.isPending}
        isSaveDisabled={!isDirty}
        saveLabel='Save promotions'
      />

      <div className='flex flex-wrap items-center gap-2'>
        <PromotionModelPicker
          models={modelNames}
          value={pendingModel}
          onChange={setPendingModel}
        />
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={!pendingModel}
          onClick={() => {
            addPromotion(pendingModel)
            setPendingModel('')
          }}
        >
          <Plus data-icon='inline-start' />
          {t('Add promotion')}
        </Button>
      </div>

      {models.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No promotion configured yet')}
        </p>
      ) : (
        <div className='space-y-6'>
          {models.map((model) => (
            <div key={model} className='space-y-3 rounded-lg border p-4'>
              <span className='font-mono text-sm font-medium'>{model}</span>

              {(promotions[model] || []).map((promotion, index) => {
                const today = todayInZone(promotion.tz || PROMOTION_DEFAULT_TZ)
                const phase = promotionPhase(promotion, today)
                const issue = validatePromotion(promotion)
                const discounts = effectiveDiscounts(
                  promotion,
                  groupRatios,
                  groupNames
                )
                return (
                  <div
                    key={promotion.uid}
                    className='space-y-3 rounded-md border p-3'
                  >
                    <div className='flex flex-wrap items-center gap-2'>
                      <Input
                        value={promotion.name}
                        placeholder={t('Promotion name')}
                        className='w-48'
                        onChange={(e) =>
                          mutate(model, index, { name: e.target.value })
                        }
                      />
                      <Input
                        type='date'
                        value={promotion.start}
                        className='w-40'
                        onChange={(e) =>
                          mutate(model, index, { start: e.target.value })
                        }
                      />
                      <span className='text-muted-foreground text-sm'>~</span>
                      <Input
                        type='date'
                        value={promotion.end}
                        className='w-40'
                        onChange={(e) =>
                          mutate(model, index, { end: e.target.value })
                        }
                      />
                      <Input
                        type='number'
                        step='0.01'
                        min='0.01'
                        max='1'
                        value={promotion.ratio}
                        className='w-24'
                        onChange={(e) =>
                          mutate(model, index, {
                            ratio: Number(e.target.value),
                          })
                        }
                      />
                      <span className={`text-xs ${PHASE_TONE[phase]}`}>
                        {t(PHASE_LABEL[phase])}
                      </span>
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon'
                        className='ml-auto'
                        onClick={() => removePromotion(model, index)}
                      >
                        <Trash2 />
                      </Button>
                    </div>

                    <div className='space-y-1'>
                      <span className='text-muted-foreground text-xs'>
                        {t(
                          'Participating groups (none selected means every group)'
                        )}
                      </span>
                      <div className='flex flex-wrap gap-1.5'>
                        {groupNames.map((group) => {
                          const active = (promotion.groups || []).includes(
                            group
                          )
                          return (
                            <button
                              key={group}
                              type='button'
                              onClick={() => toggleGroup(model, index, group)}
                              className={`rounded-md border px-2 py-1 text-xs transition-colors ${
                                active
                                  ? 'border-foreground/30 bg-foreground/5 text-foreground'
                                  : 'border-border/70 text-muted-foreground hover:bg-muted/50'
                              }`}
                            >
                              {group}
                            </button>
                          )
                        })}
                      </div>
                    </div>

                    {issue ? (
                      <p className='text-xs text-red-600 dark:text-red-400'>
                        {t(issue)}
                      </p>
                    ) : (
                      discounts.length > 0 && (
                        <div className='space-y-1'>
                          <span className='text-muted-foreground text-xs'>
                            {t(
                              'Effective discount after stacking with the group ratio'
                            )}
                          </span>
                          <div className='flex flex-wrap gap-1.5'>
                            {discounts.map((row, position) => (
                              <Badge
                                key={row.group}
                                variant={position === 0 ? 'default' : 'outline'}
                                className='font-mono text-xs'
                              >
                                {row.group} {row.groupRatio}× ×{promotion.ratio}{' '}
                                = {Number(row.effective.toFixed(4))}×
                              </Badge>
                            ))}
                          </div>
                        </div>
                      )
                    )}
                  </div>
                )
              })}
            </div>
          ))}
        </div>
      )}
    </SettingsSection>
  )
}
