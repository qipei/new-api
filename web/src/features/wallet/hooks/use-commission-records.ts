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
import { useCallback, useEffect, useState } from 'react'

import { getUserCommissionRecords, isApiSuccess } from '../api'
import type { CommissionRecord } from '../types'

interface UseCommissionRecordsOptions {
  /** Only fetch while the dialog is open */
  enabled: boolean
}

export function useCommissionRecords({ enabled }: UseCommissionRecordsOptions) {
  const [records, setRecords] = useState<CommissionRecord[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(10)
  const [loading, setLoading] = useState(false)

  const fetchRecords = useCallback(async () => {
    if (!enabled) return
    setLoading(true)
    try {
      const response = await getUserCommissionRecords(page, pageSize)
      if (isApiSuccess(response) && response.data) {
        setRecords(response.data.items || [])
        setTotal(response.data.total || 0)
      } else {
        setRecords([])
        setTotal(0)
      }
    } finally {
      setLoading(false)
    }
  }, [enabled, page, pageSize])

  useEffect(() => {
    fetchRecords()
  }, [fetchRecords])

  const handlePageChange = useCallback((next: number) => setPage(next), [])

  return { records, total, page, pageSize, loading, handlePageChange }
}
