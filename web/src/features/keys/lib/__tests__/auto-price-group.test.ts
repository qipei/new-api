import { describe, expect, it } from 'vitest'

import { transformFormDataToPayload } from '../api-key-form'
import type { ApiKeyFormValues } from '../api-key-form'
import {
  AUTO_PRICE_GROUP,
  isAutoRoutingGroup,
  supportsCustomAutoGroups,
  withAutoPriceOption,
} from '../auto-price-group'

const base = (over: Partial<ApiKeyFormValues> = {}): ApiKeyFormValues =>
  ({
    name: 'k',
    remain_quota_dollars: 10,
    unlimited_quota: true,
    model_limits: [],
    allow_ips: '',
    group: 'default',
    auto_groups_mode: 'inherit',
    auto_groups: [],
    cross_group_retry: false,
    tokenCount: 1,
    ...over,
  }) as ApiKeyFormValues

describe('auto routing group helpers', () => {
  it.each([
    ['auto', true],
    [AUTO_PRICE_GROUP, true],
    ['default', false],
    ['8.8折', false],
    [undefined, false],
  ])('isAutoRoutingGroup(%s) = %s', (group, expected) => {
    expect(isAutoRoutingGroup(group)).toBe(expected)
  })

  // 比价顺序由价格决定，不接受自定义编排——这是它和 auto 的关键区别。
  it.each([
    ['auto', true],
    [AUTO_PRICE_GROUP, false],
    ['default', false],
  ])('supportsCustomAutoGroups(%s) = %s', (group, expected) => {
    expect(supportsCustomAutoGroups(group)).toBe(expected)
  })
})

describe('withAutoPriceOption', () => {
  const options = [
    { value: 'auto', label: 'auto' },
    { value: 'default', label: 'default' },
  ]

  it('inserts right after auto so both routing modes sit together', () => {
    const result = withAutoPriceOption(options, '低价', '说明')
    expect(result.map((o) => o.value)).toEqual([
      'auto',
      AUTO_PRICE_GROUP,
      'default',
    ])
  })

  it('goes first when the backend does not expose auto', () => {
    const result = withAutoPriceOption(
      [{ value: 'default', label: 'd' }],
      '低价',
      '说明'
    )
    expect(result.map((o) => o.value)).toEqual([AUTO_PRICE_GROUP, 'default'])
  })

  // 后端将来若也开始下发这个分组，不能出现两条。
  it('does not duplicate an option the backend already returned', () => {
    const withExisting = [
      ...options,
      { value: AUTO_PRICE_GROUP, label: '已有' },
    ]
    const result = withAutoPriceOption(withExisting, '低价', '说明')
    expect(result.filter((o) => o.value === AUTO_PRICE_GROUP)).toHaveLength(1)
  })
})

// 跨分组顺延是比价路由的功能本体，界面上不给关，提交时也必须恒为 true。
describe('cross_group_retry in the submitted payload', () => {
  it.each([
    [AUTO_PRICE_GROUP, false, true],
    [AUTO_PRICE_GROUP, true, true],
    ['auto', true, true],
    ['auto', false, false],
    ['default', true, false],
  ])('group=%s form=%s -> %s', (group, formValue, expected) => {
    const payload = transformFormDataToPayload(
      base({ group, cross_group_retry: formValue })
    )
    expect(payload.cross_group_retry).toBe(expected)
  })
})
