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
// CUSTOM: getLobeIcon 的按需加载实现，见 lobe-icon.tsx（fork 扩展）。
import { useEffect, useState } from 'react'

type LobeIconRegistry = typeof import('./lobe-icon-registry')

let registryRequest: Promise<LobeIconRegistry> | null = null

export function LazyLobeIcon(props: {
  iconName: string | undefined | null
  size: number
}) {
  const [registry, setRegistry] = useState<LobeIconRegistry | null>(null)

  useEffect(() => {
    let active = true
    registryRequest ??= import('./lobe-icon-registry')
    void registryRequest
      .then((loaded) => {
        if (active) setRegistry(loaded)
      })
      .catch(() => {
        // 加载失败时保留占位；下次挂载时重新尝试。
        registryRequest = null
      })
    return () => {
      active = false
    }
  }, [])

  if (!registry) {
    return (
      <span
        aria-hidden
        className='inline-block shrink-0'
        style={{ width: props.size, height: props.size }}
      />
    )
  }
  return registry.renderLobeIcon(props.iconName, props.size)
}
