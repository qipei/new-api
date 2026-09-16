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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'

import {
  deleteCommissionOverride,
  getCommissionOverrides,
  saveCommissionOverride,
} from '../api'
import type { CommissionOverride, CommissionOverridePayload } from '../types'

const QUERY_KEY = 'commission-overrides'

export function useCommissionOverrides(
  page: number,
  pageSize: number,
  keyword: string
) {
  return useQuery({
    queryKey: [QUERY_KEY, page, pageSize, keyword],
    queryFn: async () => {
      const res = await getCommissionOverrides(page, pageSize, keyword)
      return {
        items: (res.data?.items ?? []) as CommissionOverride[],
        total: res.data?.total ?? 0,
      }
    },
  })
}

/**
 * 保存与删除共用同一个缓存失效动作：列表是唯一的展示入口，任一写操作之后都要
 * 重新拉取，否则管理员看到的还是旧参数。
 */
export function useCommissionOverrideMutations() {
  const queryClient = useQueryClient()
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: [QUERY_KEY] })

  const save = useMutation({
    mutationFn: (payload: CommissionOverridePayload) =>
      saveCommissionOverride(payload),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(i18next.t('Commission override saved'))
        void invalidate()
        return
      }
      toast.error(
        res.message || i18next.t('Failed to save commission override')
      )
    },
    onError: () => toast.error(i18next.t('Failed to save commission override')),
  })

  const remove = useMutation({
    mutationFn: (id: number) => deleteCommissionOverride(id),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(i18next.t('Commission override removed'))
        void invalidate()
        return
      }
      toast.error(
        res.message || i18next.t('Failed to remove commission override')
      )
    },
    onError: () =>
      toast.error(i18next.t('Failed to remove commission override')),
  })

  return { save, remove }
}
