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
// CUSTOM: 分组级表达式覆盖提示（fork 扩展）。
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import { groupsWithBillingExprOverride } from '../lib/group-billing-expr'
import type { PricingModel } from '../types'

/**
 * 没选具体分组时，页面展示的是模型级表达式——如果某些分组另有定价，那条价格对
 * 它们并不成立。这里把这些分组点出来，让用户知道要切到分组才看得到自己的价格。
 */
export function GroupBillingExprNotice({ model }: { model: PricingModel }) {
  const { t } = useTranslation()
  const groups = groupsWithBillingExprOverride(model)
  if (groups.length === 0) return null

  return (
    <div className='rounded-lg border border-blue-200/70 bg-blue-50/70 p-3 dark:border-blue-500/20 dark:bg-blue-500/10'>
      <div className='text-sm font-medium text-blue-900 dark:text-blue-200'>
        {t('These groups are priced differently')}
      </div>
      <p className='text-muted-foreground mt-1 text-xs'>
        {t(
          'Switch the group filter to one of these to see the price that actually applies to it.'
        )}
      </p>
      <div className='mt-2 flex flex-wrap gap-1.5'>
        {groups.map((group) => (
          <Badge key={group} variant='secondary' className='font-mono text-xs'>
            {group}
          </Badge>
        ))}
      </div>
    </div>
  )
}
