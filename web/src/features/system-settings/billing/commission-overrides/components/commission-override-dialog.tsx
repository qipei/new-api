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
import { zodResolver } from '@hookform/resolvers/zod'
import * as React from 'react'
import { type Resolver, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { useCommissionOverrideMutations } from '../hooks/use-commission-overrides'
import type { CommissionOverride } from '../types'

// 与后端 normalizeCommissionOverride 保持同一套规则。前端先挡一道是为了即时
// 反馈，真正的把关仍在后端：这些值直接参与佣金金额计算。
const createSchema = (t: (key: string) => string) =>
  z
    .object({
      username: z.string().trim().min(1, t('Username is required')),
      type: z.enum(['fixed', 'percent']),
      value: z.coerce.number().min(0),
      topupCountLimit: z.coerce.number().int().min(0),
      remark: z.string().trim().max(100, t('Remark is too long')),
    })
    .superRefine((data, ctx) => {
      if (data.type === 'percent' && data.value > 100) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['value'],
          message: t(
            'When commission type is percentage, the value must be between 0 and 100'
          ),
        })
      }
    })

type Values = z.infer<ReturnType<typeof createSchema>>

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 非空表示编辑既有记录，此时用户不可更改。 */
  override?: CommissionOverride | null
}

export function CommissionOverrideDialog(props: Props) {
  const { t } = useTranslation()
  const { save } = useCommissionOverrideMutations()
  const isEdit = !!props.override

  const schema = createSchema(t)
  const form = useForm<Values>({
    resolver: zodResolver(schema) as Resolver<Values>,
    defaultValues: {
      username: '',
      type: 'percent',
      value: 0,
      topupCountLimit: 0,
      remark: '',
    },
  })

  // 每次打开都按当前记录重置，否则会残留上一次编辑对象的值。
  React.useEffect(() => {
    if (!props.open) return
    form.reset({
      username: props.override?.username ?? '',
      type: props.override?.type ?? 'percent',
      value: props.override?.value ?? 0,
      topupCountLimit: props.override?.topup_count_limit ?? 0,
      remark: props.override?.remark ?? '',
    })
  }, [props.open, props.override, form])

  const commissionType = form.watch('type')

  const onSubmit = async (values: Values) => {
    const res = await save.mutateAsync({
      user_id: props.override?.user_id,
      username: values.username,
      type: values.type,
      value: values.value,
      topup_count_limit: values.topupCountLimit,
      remark: values.remark,
    })
    if (res.success) {
      props.onOpenChange(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        isEdit ? t('Edit commission override') : t('Add commission override')
      }
      description={t(
        'Applies to this promoter. Their invitees’ top-ups earn commission at these parameters instead of the global ones.'
      )}
      contentClassName='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-[480px]'
      contentHeight='auto'
      footer={
        <>
          <Button
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={save.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={form.handleSubmit(onSubmit)}
            disabled={save.isPending}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className='flex flex-col gap-4 py-2'
        >
          <FormField
            control={form.control}
            name='username'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Promoter username')}</FormLabel>
                <FormControl>
                  <Input {...field} disabled={isEdit} autoComplete='off' />
                </FormControl>
                <FormDescription>
                  {t(
                    'The user who receives the commission, not the one who tops up.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='type'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Commission Type')}</FormLabel>
                <Select
                  items={[
                    { value: 'percent', label: t('Percentage') },
                    { value: 'fixed', label: t('Fixed Amount') },
                  ]}
                  value={field.value}
                  onValueChange={(v) => v !== null && field.onChange(v)}
                >
                  <SelectTrigger>
                    <SelectValue>
                      {field.value === 'percent'
                        ? t('Percentage')
                        : t('Fixed Amount')}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='percent'>{t('Percentage')}</SelectItem>
                      <SelectItem value='fixed'>{t('Fixed Amount')}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='value'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Commission Value')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
                <FormDescription>
                  {commissionType === 'percent'
                    ? t('e.g. 10 means 10%')
                    : t('Quota amount granted per qualifying order')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='topupCountLimit'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Commission Count Limit')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    "0 means every paid top-up earns commission; N means only the invitee's first N paid orders"
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='remark'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Remark')}</FormLabel>
                <FormControl>
                  <Input {...field} autoComplete='off' />
                </FormControl>
                <FormDescription>
                  {t('Why this promoter has special parameters')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
