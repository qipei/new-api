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
// CUSTOM: 模型详情「按分组定价」的分组卡片（fork 扩展）。
// 左侧是分组信息（状态、倍率、限时活动、成功率、近一小时响应、创建密钥），
// 右侧是该分组折算后的价格；价格数据见 lib/group-price-blocks.ts。
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowUpDown, KeyRound, Tag } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { getPerfMetrics } from '@/features/performance-metrics/api'
import type { PerformanceGroup } from '@/features/performance-metrics/types'
import { cn } from '@/lib/utils'

import {
  buildGroupPriceBlocks,
  groupPriceText,
  groupTierLimitNote,
  type GroupPriceBlock,
} from '../lib/group-price-blocks'
import { groupDisplayCost } from '../lib/group-price-rank'
import {
  formatResponseDuration,
  groupRouteStatus,
  recentAvgLatencyMs,
  type GroupRouteStatus,
} from '../lib/group-route-stats'
import { groupMatrixPriceFactor } from '../lib/matrix-group-price'
import {
  getAvailableGroups,
  getConfiguredGroupRatio,
} from '../lib/model-helpers'
import { activePromotionForGroup } from '../lib/model-promotion'
import type {
  ModelPromotionInfo,
  PricingModel,
  TokenUnit,
  VideoPriceTable,
} from '../types'

type SortKey = 'price' | 'success' | 'latency'

const STATUS_META: Record<
  GroupRouteStatus,
  { labelKey: string; pill: string; dot: string; bar: string }
> = {
  normal: {
    labelKey: 'Normal',
    pill: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
    dot: 'bg-emerald-500',
    bar: 'before:bg-emerald-500',
  },
  busy: {
    labelKey: 'Busy',
    pill: 'bg-amber-500/10 text-amber-700 dark:text-amber-300',
    dot: 'bg-amber-500',
    bar: 'before:bg-amber-500',
  },
  unstable: {
    labelKey: 'Unstable',
    pill: 'bg-red-500/10 text-red-700 dark:text-red-300',
    dot: 'bg-red-500',
    bar: 'before:bg-red-500',
  },
  unknown: {
    labelKey: 'No data',
    pill: 'bg-muted text-muted-foreground',
    dot: 'bg-muted-foreground/60',
    bar: 'before:bg-muted-foreground/30',
  },
}

const STATUS_ORDER: GroupRouteStatus[] = [
  'normal',
  'busy',
  'unstable',
  'unknown',
]

type GroupCard = {
  group: string
  description: string
  /** 该分组下提供本模型的渠道写的对外说明。 */
  channelNotes: string[]
  baseRatio: number
  ratio: number
  promotion: ModelPromotionInfo | null
  status: GroupRouteStatus
  successRate: number | null
  recentLatencyMs: number | null
  priceCost: number
  /** 相对官方价省下的百分比；倍率不低于 1 时为 null。 */
  savingPercent: number | null
  /** 档位对该分组的限制提示，没有限制时为 null。 */
  limitNote: string | null
  blocks: GroupPriceBlock[]
  copyText: string
}

function formatRatio(ratio: number): string {
  return Number(ratio.toFixed(4)).toString()
}

/** 有数据的排前面；值相同按分组名，保证顺序稳定。 */
function compareCards(sortKey: SortKey, a: GroupCard, b: GroupCard): number {
  const byName = a.group < b.group ? -1 : 1
  if (sortKey === 'price') return a.priceCost - b.priceCost || byName
  const pick = (card: GroupCard) =>
    sortKey === 'success' ? card.successRate : card.recentLatencyMs
  const av = pick(a)
  const bv = pick(b)
  if (av === null || bv === null) {
    if (av === bv) return byName
    return av === null ? 1 : -1
  }
  const diff = sortKey === 'success' ? bv - av : av - bv
  return diff || byName
}

export function GroupPriceCards(props: {
  model: PricingModel
  groupRatio: Record<string, number>
  usableGroup: Record<string, { desc: string; ratio: number }>
  matrixTable?: VideoPriceTable
  priceRate: number
  usdExchangeRate: number
  tokenUnit: TokenUnit
  showRechargePrice: boolean
}) {
  const { t } = useTranslation()
  const [sortKey, setSortKey] = useState<SortKey>('price')
  const metricsQuery = useQuery({
    queryKey: ['perf-metrics', props.model.model_name],
    queryFn: () => getPerfMetrics(props.model.model_name, 24),
    staleTime: 60 * 1000,
  })
  const perfGroups = metricsQuery.data?.data.groups

  const cards = useMemo<GroupCard[]>(() => {
    const perfByGroup = new Map<string, PerformanceGroup>(
      (perfGroups ?? []).map((item) => [item.group, item])
    )
    const nowSeconds = Date.now() / 1000
    return getAvailableGroups(props.model, props.usableGroup || {}).map(
      (group) => {
        const baseRatio = getConfiguredGroupRatio(props.groupRatio, group)
        // 后端把活动倍率乘进了实际计费的分组倍率，展示也必须乘进去。
        const promotion = activePromotionForGroup(props.model, group)
        const ratio = promotion ? baseRatio * promotion.ratio : baseRatio
        const perf = perfByGroup.get(group)
        const successRate =
          perf && Number.isFinite(perf.success_rate) ? perf.success_rate : null
        const limitNote = groupTierLimitNote(props.matrixTable, group, t)
        const blocks = buildGroupPriceBlocks({
          model: props.model,
          group,
          ratio,
          matrixTable: props.matrixTable,
          tokenUnit: props.tokenUnit,
          showRechargePrice: props.showRechargePrice,
          priceRate: props.priceRate,
          usdExchangeRate: props.usdExchangeRate,
          t,
        })
        // 价格表配了分组价时，分组价本身也是相对官方价的折扣，节省比例要一起算。
        const effectiveRatio =
          ratio * (groupMatrixPriceFactor(props.model, group) ?? 1)
        return {
          group,
          description: props.usableGroup[group]?.desc ?? '',
          channelNotes: props.model.group_notes?.[group] ?? [],
          baseRatio,
          ratio,
          promotion,
          status: groupRouteStatus(successRate ?? undefined),
          successRate,
          recentLatencyMs: perf
            ? recentAvgLatencyMs(perf.series, nowSeconds)
            : null,
          priceCost: groupDisplayCost(props.model, group),
          savingPercent:
            effectiveRatio < 1 && Math.round((1 - effectiveRatio) * 100) > 0
              ? Math.round((1 - effectiveRatio) * 100)
              : null,
          limitNote,
          blocks,
          copyText: groupPriceText(
            [`${t('Group:')} ${group} (×${formatRatio(ratio)})`, limitNote]
              .filter(Boolean)
              .join('\n'),
            blocks,
            t
          ),
        }
      }
    )
  }, [
    perfGroups,
    props.model,
    props.usableGroup,
    props.groupRatio,
    props.matrixTable,
    props.tokenUnit,
    props.showRechargePrice,
    props.priceRate,
    props.usdExchangeRate,
    t,
  ])

  if (cards.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t(
          'This model is not available in any group, or no group pricing information is configured.'
        )}
      </p>
    )
  }

  const sorted = [...cards].sort((a, b) => compareCards(sortKey, a, b))
  const statusCounts = STATUS_ORDER.map((status) => ({
    status,
    count: cards.filter((card) => card.status === status).length,
  })).filter((item) => item.count > 0)

  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex flex-wrap items-center gap-1.5 text-sm'>
          <span className='font-medium'>
            {t('{{count}} group routes', { count: cards.length })}
          </span>
          {statusCounts.map(({ status, count }) => (
            <span
              key={status}
              className={cn(
                'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
                STATUS_META[status].pill
              )}
            >
              <span
                className={cn('size-1.5 rounded-full', STATUS_META[status].dot)}
              />
              {count} {t(STATUS_META[status].labelKey)}
            </span>
          ))}
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <CopyButton
            value={sorted.map((card) => card.copyText).join('\n\n')}
            variant='outline'
            size='sm'
            className='h-8 gap-1.5 px-2.5 text-xs'
            iconClassName='size-3.5'
            aria-label={t('Copy all prices')}
          >
            {t('Copy all prices')}
          </CopyButton>
          <div className='flex items-center gap-1.5'>
            <ArrowUpDown className='text-muted-foreground size-3.5' />
            <NativeSelect
              size='sm'
              value={sortKey}
              onChange={(event) => setSortKey(event.target.value as SortKey)}
              aria-label={t('Sort')}
            >
              <NativeSelectOption value='price'>
                {t('Price: low to high')}
              </NativeSelectOption>
              <NativeSelectOption value='success'>
                {t('Success rate: high to low')}
              </NativeSelectOption>
              <NativeSelectOption value='latency'>
                {t('Response: fast to slow')}
              </NativeSelectOption>
            </NativeSelect>
          </div>
        </div>
      </div>

      {sorted.map((card) => (
        <GroupPriceCard key={card.group} card={card} />
      ))}

      <p className='text-muted-foreground/70 text-[11px]'>
        {t(
          'Success rate covers the last 24 hours. Status: 90% or above is normal, 70%–90% is busy, below 70% is unstable. Async tasks such as video generation count from submission to completion.'
        )}
      </p>
    </div>
  )
}

function StatPill(props: {
  dotClassName: string
  label: string
  value: string
  valueClassName?: string
}) {
  return (
    <span className='bg-background inline-flex w-fit items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs'>
      <span className={cn('size-1.5 rounded-full', props.dotClassName)} />
      <span className='text-muted-foreground'>{props.label}</span>
      <span className={cn('font-medium tabular-nums', props.valueClassName)}>
        {props.value}
      </span>
    </span>
  )
}

function GroupPriceCard(props: { card: GroupCard }) {
  const { t } = useTranslation()
  const { card } = props
  const status = STATUS_META[card.status]
  let successDot = 'bg-muted-foreground/60'
  let successValue = 'text-foreground'
  if (card.status === 'normal') successDot = 'bg-emerald-500'
  if (card.status === 'busy') {
    successDot = 'bg-amber-500'
    successValue = 'text-amber-700 dark:text-amber-300'
  }
  if (card.status === 'unstable') {
    successDot = 'bg-red-500'
    successValue = 'text-red-700 dark:text-red-300'
  }

  return (
    <div className='bg-card grid overflow-hidden rounded-xl border @xl/details:grid-cols-[15.75rem_minmax(0,1fr)]'>
      <div
        className={cn(
          'bg-foreground/[0.06] relative flex flex-col gap-2 border-b p-3.5 pl-4 before:absolute before:inset-y-0 before:left-0 before:w-1 @xl/details:border-r @xl/details:border-b-0',
          status.bar
        )}
      >
        <div className='flex items-start justify-between gap-2'>
          <div className='min-w-0'>
            <div className='flex min-w-0 items-baseline'>
              <span className='text-muted-foreground shrink-0 text-xs'>
                {t('Group:')}
              </span>
              <span
                className='truncate text-[15px] font-semibold'
                title={card.group}
              >
                {card.group}
              </span>
            </div>
            {card.description && card.description !== card.group && (
              <div className='text-muted-foreground truncate text-xs'>
                {card.description}
              </div>
            )}
          </div>
          <span
            className={cn(
              'inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
              status.pill
            )}
          >
            <span className={cn('size-1.5 rounded-full', status.dot)} />
            {t(status.labelKey)}
          </span>
        </div>

        <span className='bg-background inline-flex w-fit items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs'>
          <span className='text-muted-foreground'>{t('Ratio')}</span>
          <span className='font-mono font-medium'>
            ×{formatRatio(card.ratio)}
          </span>
          {card.promotion && (
            <span className='text-muted-foreground/70 font-mono line-through'>
              ×{formatRatio(card.baseRatio)}
            </span>
          )}
        </span>
        {card.promotion && (
          <span
            className='inline-flex w-fit max-w-full flex-wrap items-center gap-x-1 rounded-md border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:text-amber-300'
            title={`${card.promotion.name} ${card.promotion.start} ~ ${card.promotion.end}`}
          >
            <Tag className='size-3 shrink-0' />
            <span className='break-all'>
              {card.promotion.name || t('Promotion')}
            </span>
            <span className='font-mono opacity-80'>
              {t('Until {{date}}', { date: card.promotion.end })}
            </span>
          </span>
        )}
        <StatPill
          dotClassName={successDot}
          label={t('Success rate')}
          value={
            card.successRate === null
              ? t('No data')
              : `${card.successRate.toFixed(1)}%`
          }
          valueClassName={
            card.successRate === null
              ? 'text-muted-foreground font-normal'
              : successValue
          }
        />
        <StatPill
          dotClassName={
            card.recentLatencyMs === null
              ? 'bg-muted-foreground/60'
              : 'bg-sky-500'
          }
          label={t('Last hour response')}
          value={
            card.recentLatencyMs === null
              ? t('No data')
              : formatResponseDuration(card.recentLatencyMs, t)
          }
          valueClassName={
            card.recentLatencyMs === null
              ? 'text-muted-foreground font-normal'
              : undefined
          }
        />
        {card.limitNote && (
          <span className='bg-background text-muted-foreground w-fit rounded-full border px-2 py-0.5 text-xs'>
            {card.limitNote}
          </span>
        )}
        <Button
          variant='outline'
          size='sm'
          className='bg-background mt-auto h-7 w-fit gap-1.5 px-2.5 text-xs'
          render={<Link to='/keys' search={{ create_group: card.group }} />}
        >
          <KeyRound className='size-3.5' />
          {t('Create API Key')}
        </Button>
      </div>

      <div className='min-w-0 space-y-3 p-3.5'>
        {/* 首个价格块的单位和省钱角标、复制按钮放在同一行，避免右侧顶部空一行 */}
        <div className='flex items-center justify-between gap-2'>
          <span className='text-muted-foreground text-xs'>
            {firstBlockHeader(card.blocks)}
          </span>
          <div className='flex shrink-0 items-center gap-1.5'>
            {card.savingPercent !== null && (
              <span className='rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:text-emerald-300'>
                {t('Save {{percent}}% vs official', {
                  percent: card.savingPercent,
                })}
              </span>
            )}
            <CopyButton
              value={card.copyText}
              className='text-muted-foreground size-7'
              iconClassName='size-3.5'
              tooltip={t('Copy prices')}
            />
          </div>
        </div>
        {card.blocks.map((block, index) => (
          <GroupPriceBlockView
            key={
              block.kind === 'note'
                ? block.text
                : `${block.kind}|${block.title ?? ''}|${block.unit ?? ''}`
            }
            block={block}
            hideHeader={index === 0}
          />
        ))}
        {/* 渠道备注往往较长，放在价格区下方跟价格表同宽，避免在左侧窄栏里折成很多行把卡片撑高 */}
        {card.channelNotes.length > 0 && (
          <div className='text-muted-foreground max-w-2xl space-y-1 border-t pt-3 text-xs leading-relaxed break-words whitespace-pre-line'>
            {card.channelNotes.map((note) => (
              <p key={note}>{note}</p>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function blockHeader(block: GroupPriceBlock): string {
  if (block.kind === 'note') return ''
  return [block.title, block.unit].filter(Boolean).join(' · ')
}

function firstBlockHeader(blocks: GroupPriceBlock[]): string {
  return blocks.length > 0 ? blockHeader(blocks[0]) : ''
}

function GroupPriceBlockView(props: {
  block: GroupPriceBlock
  hideHeader: boolean
}) {
  const { t } = useTranslation()
  const { block } = props
  if (block.kind === 'note') {
    return <p className='text-muted-foreground text-sm'>{block.text}</p>
  }
  const header = props.hideHeader ? '' : blockHeader(block)

  return (
    <div className='min-w-0'>
      {header && (
        <div className='text-muted-foreground mb-1 text-xs'>{header}</div>
      )}
      {block.kind === 'list' ? (
        <div
          className={cn(
            'grid grid-cols-[repeat(auto-fit,minmax(8.5rem,1fr))] gap-2',
            block.rows.length === 1 && 'max-w-xs'
          )}
        >
          {block.rows.map((row) => (
            <div key={row.label} className='bg-muted/60 rounded-lg px-3 py-2'>
              <div className='text-muted-foreground text-xs'>{row.label}</div>
              <div className='mt-0.5 font-mono text-lg font-semibold tabular-nums'>
                {row.value}
                {row.unit && (
                  <span className='text-muted-foreground ml-1 font-sans text-[11px] font-normal'>
                    {row.unit}
                  </span>
                )}
              </div>
              {row.official && row.official !== row.value && (
                <div className='text-muted-foreground text-[11px]'>
                  {t('Official price')}{' '}
                  <span className='font-mono line-through'>{row.official}</span>
                </div>
              )}
            </div>
          ))}
        </div>
      ) : (
        <div className='max-w-2xl overflow-x-auto rounded-lg border'>
          <table className='w-full min-w-[280px] text-sm'>
            <thead className='bg-muted/40'>
              <tr className='text-muted-foreground text-left text-xs'>
                <th className='px-3 py-1.5 font-normal'>{block.corner}</th>
                {block.columns.map((column) => (
                  <th
                    key={column}
                    className='px-3 py-1.5 text-right font-normal'
                  >
                    {column}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row) => (
                <tr key={row.label} className='border-t'>
                  <td className='text-muted-foreground px-3 py-1.5'>
                    {row.label}
                  </td>
                  {row.cells.map((cell, index) => (
                    <td
                      key={block.columns[index]}
                      className={cn(
                        'px-3 py-1.5 text-right tabular-nums',
                        cell === null
                          ? 'text-muted-foreground/70'
                          : 'font-mono font-semibold'
                      )}
                    >
                      {cell ?? t('Not supported')}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
