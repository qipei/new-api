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
// CUSTOM: 分组级计费表达式覆盖（fork 扩展）。整块自成一个 section，只在
// section-registry 里挂一行，避免把新字段穿过 ratio-settings-card →
// model-ratio-form → visual-editor → pricing-sheet 那条上游链路。
import { Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'

import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  GROUP_BILLING_EXPR_OPTION_KEY,
  parseJsonRecord,
  serializeGroupBillingExpr,
  validateGroupBillingExpr,
  type GroupBillingExprMap,
} from './group-billing-expr-core'

interface GroupBillingExprSectionProps {
  /** billing_setting.group_billing_expr 的 JSON 字符串 */
  defaultValue: string
  /** billing_setting.billing_mode，用来只列出真正走表达式计价的模型 */
  billingMode: string
  /** billing_setting.billing_expr，作为分组未覆盖时的回落展示 */
  billingExpr: string
  /** GroupRatio 的 JSON，提供可选分组名 */
  groupRatio: string
}

export function GroupBillingExprSection(props: GroupBillingExprSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [savedJson, setSavedJson] = useState(props.defaultValue || '{}')
  const [overrides, setOverrides] = useState<GroupBillingExprMap>(() =>
    parseJsonRecord<Record<string, string>>(props.defaultValue)
  )

  const tieredModels = useMemo(() => {
    const modes = parseJsonRecord<string>(props.billingMode)
    return Object.keys(modes)
      .filter((model) => modes[model] === 'tiered_expr')
      .sort((a, b) => a.localeCompare(b))
  }, [props.billingMode])

  const modelExprs = useMemo(
    () => parseJsonRecord<string>(props.billingExpr) as Record<string, string>,
    [props.billingExpr]
  )

  const groupNames = useMemo(() => {
    const ratios = parseJsonRecord<number>(props.groupRatio) as Record<
      string,
      number
    >
    return Object.keys(ratios)
      .filter((group) => group !== 'auto')
      .sort((a, b) => a.localeCompare(b))
  }, [props.groupRatio])

  const nextJson = useMemo(
    () => serializeGroupBillingExpr(overrides),
    [overrides]
  )
  const isDirty = nextJson !== savedJson

  const setExpr = (model: string, group: string, expr: string) => {
    setOverrides((prev) => ({
      ...prev,
      [model]: { ...prev[model], [group]: expr },
    }))
  }

  const removeGroup = (model: string, group: string) => {
    setOverrides((prev) => {
      const byGroup = { ...prev[model] }
      delete byGroup[group]
      const next = { ...prev }
      if (Object.keys(byGroup).length === 0) {
        delete next[model]
      } else {
        next[model] = byGroup
      }
      return next
    })
  }

  const addGroup = (model: string) => {
    const used = new Set(Object.keys(overrides[model] || {}))
    const candidate = groupNames.find((group) => !used.has(group))
    if (!candidate) {
      toast.info(t('Every group already has an override for this model'))
      return
    }
    setExpr(model, candidate, modelExprs[model] || '')
  }

  const renameGroup = (model: string, from: string, to: string) => {
    if (from === to) return
    if (overrides[model]?.[to] !== undefined) {
      toast.error(t('That group already has an override for this model'))
      return
    }
    setOverrides((prev) => {
      const byGroup = { ...prev[model] }
      byGroup[to] = byGroup[from]
      delete byGroup[from]
      return { ...prev, [model]: byGroup }
    })
  }

  const onSave = async () => {
    if (!isDirty) {
      toast.info(t('No changes to save'))
      return
    }
    const issue = validateGroupBillingExpr(overrides)
    if (issue) {
      toast.error(`${t('Invalid billing expression')}: ${issue}`)
      return
    }
    await updateOption.mutateAsync({
      key: GROUP_BILLING_EXPR_OPTION_KEY,
      value: nextJson,
    })
    setSavedJson(nextJson)
  }

  return (
    <SettingsSection title={t('Per-group Billing Expressions')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Override the billing expression for specific groups of a tiered-pricing model. A group without an override uses the model expression. To make one group ignore time-of-day pricing, give it an expression with no hour() condition — the billing mode itself stays per-model.'
        )}
      </p>
      <SettingsPageFormActions
        onSave={() => void onSave()}
        isSaving={updateOption.isPending}
        isSaveDisabled={!isDirty}
        saveLabel='Save per-group expressions'
      />

      {tieredModels.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No model uses tiered pricing yet')}
        </p>
      ) : (
        <div className='space-y-6'>
          {tieredModels.map((model) => {
            const byGroup = overrides[model] || {}
            const groups = Object.keys(byGroup).sort((a, b) =>
              a.localeCompare(b)
            )
            return (
              <div key={model} className='space-y-3 rounded-lg border p-4'>
                <div className='flex flex-wrap items-center justify-between gap-3'>
                  <span className='font-mono text-sm font-medium'>{model}</span>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => addGroup(model)}
                  >
                    <Plus data-icon='inline-start' />
                    {t('Add group override')}
                  </Button>
                </div>

                <div className='text-muted-foreground space-y-1 text-xs'>
                  <span>{t('Model expression (fallback)')}</span>
                  <pre className='bg-muted/40 overflow-x-auto rounded p-2 font-mono leading-5 whitespace-pre-wrap'>
                    {modelExprs[model] || t('Empty')}
                  </pre>
                </div>

                {groups.length === 0 ? (
                  <p className='text-muted-foreground text-xs'>
                    {t('Every group uses the model expression')}
                  </p>
                ) : (
                  groups.map((group) => (
                    <div
                      key={group}
                      className='space-y-2 rounded-md border p-3'
                    >
                      <div className='flex flex-wrap items-center gap-2'>
                        <Select
                          value={group}
                          onValueChange={(value) => {
                            if (value) renameGroup(model, group, value)
                          }}
                        >
                          <SelectTrigger className='w-56'>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {groupNames.map((name) => (
                              <SelectItem key={name} value={name}>
                                {name}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <Button
                          type='button'
                          variant='ghost'
                          size='sm'
                          onClick={() => removeGroup(model, group)}
                        >
                          <Trash2 data-icon='inline-start' />
                          {t('Remove')}
                        </Button>
                      </div>
                      <Textarea
                        value={byGroup[group]}
                        onChange={(event) =>
                          setExpr(model, group, event.target.value)
                        }
                        className='font-mono text-xs'
                        rows={4}
                        spellCheck={false}
                        placeholder='tier("base", p * 3 + c * 9)'
                      />
                    </div>
                  ))
                )}
              </div>
            )
          })}
        </div>
      )}
    </SettingsSection>
  )
}
