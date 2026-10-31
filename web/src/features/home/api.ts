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
import { api } from '@/lib/api'

import type { RankingPeriod, RankingsSnapshot } from '../rankings/types'
import type { HomePageContentResponse } from './types'

// ============================================================================
// Home Page APIs
// ============================================================================

/**
 * Get custom home page content
 * Returns Markdown/HTML content or iframe URL
 */
export async function getHomePageContent(): Promise<HomePageContentResponse> {
  const res = await api.get('/api/home_page_content')
  return res.data
}

// CUSTOM: 首页路由的 loader 先发起请求，和首页组件的脚本并行下载（fork 扩展）；组件
// 挂载后取走这个请求复用，不再等脚本加载完才开始请求。
let prefetchedHomePageContent: Promise<HomePageContentResponse> | null = null

export function prefetchHomePageContent(): void {
  if (prefetchedHomePageContent) return
  prefetchedHomePageContent = getHomePageContent()
  // 失败交给组件里复用请求时处理，这里不能留下未处理的 rejection。
  prefetchedHomePageContent.catch(() => {})
}

export function takeHomePageContent(): Promise<HomePageContentResponse> {
  const request = prefetchedHomePageContent ?? getHomePageContent()
  prefetchedHomePageContent = null
  return request
}

export async function getHomeRankings(period: RankingPeriod): Promise<{
  success: boolean
  message?: string
  data: RankingsSnapshot
}> {
  const res = await api.get('/api/home/rankings', { params: { period } })
  return res.data
}
