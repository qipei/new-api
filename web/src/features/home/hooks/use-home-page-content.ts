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
import i18next from 'i18next'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'

import { isHttpUrl } from '@/lib/content-format'

import { takeHomePageContent } from '../api'
import type { HomePageContentResult } from '../types'

const STORAGE_KEY = 'home_page_content'

// 上次请求到的首页内容；空串表示上次确认过使用默认首页，null 表示从没请求过。
function readCachedContent(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

function writeCachedContent(content: string): void {
  try {
    localStorage.setItem(STORAGE_KEY, content)
  } catch {
    // Storage may be disabled by the browser.
  }
}

/**
 * Hook to load and manage custom home page content
 * Supports both Markdown/HTML content and iframe URLs
 *
 * CUSTOM: 有缓存（包括「默认首页」这个结果）时立即按缓存渲染，后台刷新后再更新；只有
 * 从没请求过时才显示加载中，避免每次打开首页都先空白等这个请求（fork 扩展）。
 */
export function useHomePageContent(): HomePageContentResult {
  const [cached] = useState(readCachedContent)
  const [content, setContent] = useState<string>(cached ?? '')
  const [isLoaded, setIsLoaded] = useState(cached !== null)

  useEffect(() => {
    let mounted = true

    const loadContent = async () => {
      try {
        const { success, data } = await takeHomePageContent()
        if (!mounted) return
        const next = success && data ? data : ''
        setContent(next)
        writeCachedContent(next)
      } catch (error) {
        if (!mounted) return
        // eslint-disable-next-line no-console
        console.error('Failed to load home page content:', error)
        toast.error(i18next.t('Failed to load home page content'))
      } finally {
        if (mounted) setIsLoaded(true)
      }
    }

    void loadContent()

    return () => {
      mounted = false
    }
  }, [])

  const isUrl = isHttpUrl(content)

  return { content, isLoaded, isUrl }
}
