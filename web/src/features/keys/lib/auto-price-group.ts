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
// CUSTOM: 比价路由 auto_price（fork 扩展）。
//
// 它和 auto 一样是路由方式而不是真实分组，所以不出现在后端的可用分组表里。
// 这里作为合成选项注入，免得还要管理员去可用分组表里手动加一行才能用。

import type { ApiKeyGroupOption } from '../components/api-key-group-combobox'

export const AUTO_PRICE_GROUP = 'auto_price'

export function isAutoRoutingGroup(group: string | null | undefined): boolean {
  return group === 'auto' || group === AUTO_PRICE_GROUP
}

/** 比价路由的顺序由价格决定，不接受自定义编排，也没有可关的跨组开关。 */
export function supportsCustomAutoGroups(
  group: string | null | undefined
): boolean {
  return group === 'auto'
}

/**
 * 把比价路由加进分组列表，并把两种自动路由固定排在最前：auto_price 第一、auto
 * 第二，其余分组保持后端返回的顺序。后端不下发 auto_price，所以这里合成；将来
 * 后端也下发时沿用后端那一条，不出现两条。
 */
export function withAutoPriceOption(
  groups: ApiKeyGroupOption[],
  label: string,
  desc: string
): ApiKeyGroupOption[] {
  const autoPrice = groups.find(
    (group) => group.value === AUTO_PRICE_GROUP
  ) ?? { value: AUTO_PRICE_GROUP, label, desc, ratio: undefined }
  const auto = groups.filter((group) => group.value === 'auto')
  const rest = groups.filter((group) => !isAutoRoutingGroup(group.value))
  return [autoPrice, ...auto, ...rest]
}
