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
// CUSTOM: 用真实限流配置替代随机生成的 RPM/TPM/RPD（fork 扩展）。
//
// 原实现用 seededRandom 按模型名生成三个数字展示给用户，既是假数据、维度也不对：
// 系统真实的限流是「每 N 分钟 M 次请求」，按分组配置，与模型无关。
import { Gauge } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  staticDataTableClassNames as tableStyles,
} from '@/components/data-table'
import { useStatus } from '@/hooks/use-status'

import type { PricingModel } from '../types'

type GroupLimit = {
  group: string
  count: number
  successCount: number
}

/** 分组限流值：[请求数, 成功请求数]，0 表示该项不限。 */
type GroupLimitTuple = [number, number]

export function ModelRateLimitsSection(props: { model: PricingModel }) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const data = status?.data as Record<string, unknown> | undefined

  const enabled = data?.model_rate_limit_enabled === true
  const durationMinutes = Number(data?.model_rate_limit_duration_minutes ?? 0)
  const defaultCount = Number(data?.model_rate_limit_count ?? 0)
  const defaultSuccess = Number(data?.model_rate_limit_success_count ?? 0)
  const groupOverrides = (data?.model_rate_limit_group ?? {}) as Record<
    string,
    GroupLimitTuple
  >

  if (!enabled) {
    return (
      <section>
        <h3 className='mb-3 flex items-center gap-2 text-sm font-semibold'>
          <Gauge className='text-muted-foreground size-4' />
          {t('Rate limits')}
        </h3>
        <p className='text-muted-foreground text-sm'>
          {t('Request rate limiting is not enabled for this site.')}
        </p>
      </section>
    )
  }

  // 限流按分组生效，因此只列该模型实际开放的分组。
  const groups = (props.model.enable_groups ?? []).filter(
    (group) => group && group !== 'auto'
  )
  const rows: GroupLimit[] = (groups.length > 0 ? groups : ['default'])
    .slice()
    .sort((a, b) => a.localeCompare(b))
    .map((group) => {
      const override = groupOverrides[group]
      return {
        group,
        count: override ? override[0] : defaultCount,
        successCount: override ? override[1] : defaultSuccess,
      }
    })

  const formatLimit = (value: number) =>
    value > 0 ? String(value) : t('Unlimited')

  return (
    <section>
      <h3 className='mb-3 flex items-center gap-2 text-sm font-semibold'>
        <Gauge className='text-muted-foreground size-4' />
        {t('Rate limits')}
      </h3>
      <p className='text-muted-foreground mb-2 text-xs'>
        {t('Limits apply per group over a {{minutes}} minute window.', {
          minutes: durationMinutes,
        })}
      </p>
      <StaticDataTable
        className={tableStyles.sectionContainer}
        headerRowClassName={tableStyles.mutedHeaderRow}
        data={rows}
        getRowKey={(row) => row.group}
        getRowClassName={() => 'hover:bg-muted/20'}
        columns={[
          {
            id: 'group',
            header: t('Group'),
            className: 'h-9',
            cellClassName: 'py-2 font-mono',
            cell: (row) => row.group,
          },
          {
            id: 'count',
            header: t('Requests'),
            className: 'h-9 text-right',
            cellClassName: tableStyles.topNumericCell,
            cell: (row) => formatLimit(row.count),
          },
          {
            id: 'success',
            header: t('Successful requests'),
            className: 'h-9 text-right',
            cellClassName: tableStyles.topNumericCell,
            cell: (row) => formatLimit(row.successCount),
          },
        ]}
      />
    </section>
  )
}
