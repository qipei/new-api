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

For commercial licensing, please contact support@quantumnous.com
*/
// CUSTOM: 限时活动的模型选择器（fork 扩展）。
//
// 只列模型广场上真实存在的模型：给一个没上架的模型配活动没有意义，而站点模型有
// 几十上百个，纯下拉找起来很痛苦，所以带输入过滤。
import { Check, ChevronsUpDown } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

interface PromotionModelPickerProps {
  models: string[]
  value: string
  onChange: (model: string) => void
  disabled?: boolean
}

export function PromotionModelPicker(props: PromotionModelPickerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')

  const filtered = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    if (!keyword) return props.models
    return props.models.filter((model) => model.toLowerCase().includes(keyword))
  }, [props.models, search])

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            role='combobox'
            aria-expanded={open}
            disabled={props.disabled}
            className='w-72 justify-between font-normal'
          >
            <span
              className={cn(
                'truncate',
                !props.value && 'text-muted-foreground'
              )}
            >
              {props.value || t('Select a model')}
            </span>
            <ChevronsUpDown className='ml-2 size-4 shrink-0 opacity-50' />
          </Button>
        }
      />
      <PopoverContent className='w-72 p-0' align='start'>
        {/* 自己过滤而不是交给 Command：模型名里有点号和连字符，内置的模糊匹配
            会把 gpt-4 之类的输入拆开匹配到一堆无关项。 */}
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={t('Search models')}
            value={search}
            onValueChange={setSearch}
          />
          <CommandList className='max-h-72'>
            <CommandEmpty>{t('No model found.')}</CommandEmpty>
            <CommandGroup>
              {filtered.map((model) => (
                <CommandItem
                  key={model}
                  value={model}
                  onSelect={() => {
                    props.onChange(model)
                    setSearch('')
                    setOpen(false)
                  }}
                >
                  <Check
                    className={cn(
                      'mr-2 size-4',
                      props.value === model ? 'opacity-100' : 'opacity-0'
                    )}
                  />
                  <span className='truncate font-mono text-xs'>{model}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
