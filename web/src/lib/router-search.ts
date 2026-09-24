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
import {
  defaultParseSearch,
  defaultStringifySearch,
} from '@tanstack/react-router'

// Referral codes are opaque text, including values such as 1e03 or null.
// Keep TanStack's JSON handling for every other search parameter.
export function parseRouterSearch(search: string): Record<string, unknown> {
  const parsed: Record<string, unknown> = defaultParseSearch(search)
  const aff = new URLSearchParams(search).get('aff')
  if (aff !== null) parsed.aff = aff
  return parsed
}

export function stringifyRouterSearch(search: Record<string, unknown>): string {
  const { aff, ...rest } = search
  const serialized = defaultStringifySearch(rest)
  if (typeof aff !== 'string') return serialized
  const separator = serialized ? '&' : '?'
  return `${serialized}${separator}aff=${encodeURIComponent(aff)}`
}
