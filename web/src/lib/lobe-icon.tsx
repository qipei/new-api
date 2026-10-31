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
// CUSTOM: LobeHub 图标按需加载（fork 扩展）。@lobehub/icons 整库约 1 MB（压缩后），按名字
// 动态取图标没法摇树，原先每个用到模型图标的页面（首页、模型广场、排行榜、日志等）都要先
// 下载完整库才能渲染。现在先渲染同尺寸的占位，页面内容立刻可见，图标库加载完再补上图标；
// 图标库只下载一次，之后各页面复用。
import type { ReactNode } from 'react'

import { LazyLobeIcon } from './lobe-icon-lazy'

/**
 * Render a LobeHub icon by name, e.g. "OpenAI", "OpenAI.Color",
 * "Claude.Avatar.type={'platform'}". See lobe-icon-registry.tsx for the syntax.
 */
export function getLobeIcon(
  iconName: string | undefined | null,
  size: number = 20
): ReactNode {
  return <LazyLobeIcon iconName={iconName} size={size} />
}
