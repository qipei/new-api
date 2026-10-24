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
// CUSTOM: 模型详情分组卡片上的线路状态（fork 扩展）。
// 数据来自分组性能统计：同步请求在结束时计入，异步任务在轮询到终态时计入。
import type { TFunction } from 'i18next'

import { getSuccessRateLevel } from '@/features/performance-metrics/lib/format'
import type { PerformanceSeriesPoint } from '@/features/performance-metrics/types'

export type GroupRouteStatus = 'normal' | 'busy' | 'unstable' | 'unknown'

/** 按近 24 小时成功率分级：≥90% 正常，70%–90% 繁忙，低于 70% 不稳定。 */
export function groupRouteStatus(
  successRate: number | undefined
): GroupRouteStatus {
  const level = getSuccessRateLevel(successRate ?? Number.NaN)
  if (level === 'excellent' || level === 'good') return 'normal'
  if (level === 'warning') return 'busy'
  if (level === 'critical') return 'unstable'
  return 'unknown'
}

const HOUR_SECONDS = 3600

/**
 * 近一小时的平均响应时间（毫秒），没有数据返回 null。
 *
 * 统计按时段（默认一小时，也可能是 5 分钟或 1 分钟）汇总，凡是和最近一小时有重叠
 * 的时段都算进来：按小时汇总时，刚过整点的那几分钟里当前时段几乎没有请求，只看
 * 当前时段会频繁显示「暂无」。时段长度从相邻数据点的间隔推出来。
 */
export function recentAvgLatencyMs(
  series: PerformanceSeriesPoint[],
  nowSeconds: number
): number | null {
  const sorted = [...series].sort((a, b) => a.ts - b.ts)
  let bucketSeconds = HOUR_SECONDS
  for (let i = 1; i < sorted.length; i++) {
    const gap = sorted[i].ts - sorted[i - 1].ts
    if (gap > 0 && gap < bucketSeconds) bucketSeconds = gap
  }
  const windowStart = nowSeconds - HOUR_SECONDS
  const values = sorted
    .filter(
      (point) =>
        point.ts + bucketSeconds > windowStart && point.avg_latency_ms > 0
    )
    .map((point) => point.avg_latency_ms)
  if (values.length === 0) return null
  return Math.round(
    values.reduce((sum, value) => sum + value, 0) / values.length
  )
}

/** 响应时间写成「44.8秒」「6分52秒」这样的读法，长任务（视频生成）按分秒展示。 */
export function formatResponseDuration(ms: number, t: TFunction): string {
  if (ms < 1000) return t('{{ms}}ms', { ms: Math.round(ms) })
  const totalSeconds = ms / 1000
  if (totalSeconds < 60) {
    return t('{{seconds}}s', { seconds: Number(totalSeconds.toFixed(1)) })
  }
  const rounded = Math.round(totalSeconds)
  return t('{{minutes}}m {{seconds}}s', {
    minutes: Math.floor(rounded / 60),
    seconds: rounded % 60,
  })
}
