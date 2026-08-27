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
import { Wrench01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import { formatLogQuota } from '@/lib/format'

import { hasToolSurcharge } from '../lib/format'
import type { LogOtherData, UpstreamCostInfo } from '../types'

interface LogCostDisplayProps {
  quota: number
  other: LogOtherData | null
  upstreamCost?: UpstreamCostInfo
  /** Upstream cost and margin are operating data; never render them for end users. */
  isAdmin?: boolean
}

function splitQuotaDisplay(value: string): { prefix: string; amount: string } {
  const match = value.match(/^([^0-9+\-.,\s]+)(.+)$/)
  if (!match) return { prefix: '', amount: value }
  return { prefix: match[1], amount: match[2] }
}

function ToolSurchargeMarker() {
  const { t } = useTranslation()
  const label = t('Includes tool-call surcharge')

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Badge
            variant='warning'
            className='h-5 min-w-5 cursor-help gap-0 rounded-full px-1'
            role='img'
            aria-label={label}
            tabIndex={0}
            data-tool-surcharge-indicator='true'
          >
            <HugeiconsIcon
              icon={Wrench01Icon}
              strokeWidth={2}
              aria-hidden='true'
            />
            <span
              className='text-[9px] leading-none font-bold'
              aria-hidden='true'
            >
              +
            </span>
          </Badge>
        }
      />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

function QuotaBadge(props: { quota: number }) {
  const quotaDisplay = splitQuotaDisplay(formatLogQuota(props.quota))

  return (
    <span className='border-border/80 bg-muted/60 inline-flex h-6 w-fit items-center rounded-md border px-2 [font-family:var(--font-body)] text-sm leading-none font-semibold tabular-nums'>
      {quotaDisplay.prefix ? (
        <span className='mr-1'>{quotaDisplay.prefix}</span>
      ) : null}
      <span>{quotaDisplay.amount}</span>
    </span>
  )
}

function SubscriptionBadge(props: { quota: number }) {
  const { t } = useTranslation()

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <StatusBadge
            label={t('Subscription')}
            variant='success'
            size='sm'
            copyable={false}
            className='cursor-help'
          />
        }
      />
      <TooltipContent>
        <span>
          {t('Deducted by subscription')}: {formatLogQuota(props.quota)}
        </span>
      </TooltipContent>
    </Tooltip>
  )
}

const UPSTREAM_COST_FORMAT = {
  digitsLarge: 4,
  digitsSmall: 6,
  abbreviate: false,
}

// Shown on every log that has upstream cost data, not only on losses: the badge
// is the only place an admin can see what a request actually cost, and hiding it
// when the margin looks fine is how a wrong comparison stays invisible.
function UpstreamCostBadge(props: { cost: UpstreamCostInfo }) {
  const { t } = useTranslation()
  const format = (value: number) =>
    formatBillingCurrencyFromUSD(value, UPSTREAM_COST_FORMAT)
  const margin = props.cost.platform_amount - props.cost.upstream_amount
  const label = `${t('Upstream')} ${format(props.cost.upstream_amount)}`

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Badge
            variant={props.cost.exceeds_platform ? 'destructive' : 'outline'}
            className='h-5 cursor-help px-1.5 tabular-nums'
            aria-label={label}
            tabIndex={0}
            data-upstream-cost-exceeds={String(props.cost.exceeds_platform)}
          >
            {label}
          </Badge>
        }
      />
      <TooltipContent>
        <div className='flex flex-col gap-0.5'>
          <span>
            {t('Upstream cost')}: {format(props.cost.upstream_amount)}
          </span>
          <span>
            {t('Platform charged')}: {format(props.cost.platform_amount)}
          </span>
          <span>
            {t('Margin')}: {format(margin)}
          </span>
          {props.cost.exceeds_platform ? (
            <span>{t('Upstream cost higher')}</span>
          ) : null}
        </div>
      </TooltipContent>
    </Tooltip>
  )
}

export function LogCostDisplay(props: LogCostDisplayProps) {
  const isSubscription = props.other?.billing_source === 'subscription'
  const showToolSurcharge = hasToolSurcharge(props.other)
  // The backend only serves upstream cost to admins; gate here too as defense in
  // depth so operating margins never leak into a user-facing view.
  const upstreamCost = props.isAdmin ? props.upstreamCost : undefined

  if (!isSubscription && !showToolSurcharge && !upstreamCost) {
    return (
      <div className='flex flex-col gap-0.5'>
        <QuotaBadge quota={props.quota} />
      </div>
    )
  }

  return (
    <TooltipProvider>
      <div className='inline-flex items-center gap-1'>
        {isSubscription ? (
          <SubscriptionBadge quota={props.quota} />
        ) : (
          <QuotaBadge quota={props.quota} />
        )}
        {showToolSurcharge ? <ToolSurchargeMarker /> : null}
        {upstreamCost ? <UpstreamCostBadge cost={upstreamCost} /> : null}
      </div>
    </TooltipProvider>
  )
}
