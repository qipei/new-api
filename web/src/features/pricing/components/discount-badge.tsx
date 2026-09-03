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
// CUSTOM: 折扣角标（fork 扩展）。列表和卡片上展示该模型当前能拿到的最低折扣。
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { formatDiscountLabel, type BestDiscount } from '../lib/model-promotion'

export function DiscountBadge(props: {
  discount: BestDiscount
  className?: string
}) {
  const { t } = useTranslation()
  const { promotion, group } = props.discount

  // 折扣可能来自分组倍率，也可能来自限时活动，鼠标悬停时说清楚是哪一种。
  const title = promotion
    ? `${promotion.name} ${promotion.start} ~ ${promotion.end}（${group}）`
    : `${t('Groups')}: ${group}`

  return (
    <span
      title={title}
      className={cn(
        'inline-flex shrink-0 items-center rounded-full bg-gradient-to-r from-orange-500 to-orange-400 px-2 py-0.5 text-xs font-bold text-white shadow-sm',
        props.className
      )}
    >
      {formatDiscountLabel(props.discount.ratio)}
    </span>
  )
}
