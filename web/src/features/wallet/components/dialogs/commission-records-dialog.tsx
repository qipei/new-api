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
import { ChevronLeft, ChevronRight, HandCoins } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'

import { useCommissionRecords } from '../../hooks/use-commission-records'

interface CommissionRecordsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CommissionRecordsDialog({
  open,
  onOpenChange,
}: CommissionRecordsDialogProps) {
  const { t } = useTranslation()
  const { records, total, page, pageSize, loading, handlePageChange } =
    useCommissionRecords({ enabled: open })

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Commission Details')}
      description={t("Earned commissions from your referrals' paid top-ups")}
      contentClassName='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-lg'
      contentHeight='auto'
      bodyClassName='space-y-3'
    >
      {loading ? (
        <div className='space-y-2'>
          {['a', 'b', 'c'].map((key) => (
            <Skeleton key={key} className='h-14 rounded-lg' />
          ))}
        </div>
      ) : records.length === 0 ? (
        <div className='text-muted-foreground flex flex-col items-center gap-2 py-8 text-sm'>
          <HandCoins className='size-8 opacity-40' />
          {t('No commission records yet')}
        </div>
      ) : (
        <div className='space-y-2'>
          {records.map((record) => (
            <div
              key={record.id}
              className='bg-muted/30 flex items-center justify-between rounded-lg p-3'
            >
              <div className='min-w-0'>
                <div className='truncate text-sm font-medium'>
                  {t('Invitee')}: {record.invitee_username}
                </div>
                <div className='text-muted-foreground text-xs'>
                  {new Date(record.created_time * 1000).toLocaleString()} ·{' '}
                  {t('Credited')} {formatQuota(record.credited_quota)}
                </div>
              </div>
              <div className='shrink-0 text-sm font-semibold text-emerald-600 tabular-nums dark:text-emerald-400'>
                +{formatQuota(record.commission_quota)}
              </div>
            </div>
          ))}
        </div>
      )}

      {total > pageSize && (
        <div className='flex items-center justify-between pt-1'>
          <span className='text-muted-foreground text-xs'>
            {t('Page {{page}} of {{totalPages}}', { page, totalPages })}
          </span>
          <div className='flex gap-1'>
            <Button
              variant='outline'
              size='sm'
              className='size-8 p-0'
              disabled={page <= 1 || loading}
              onClick={() => handlePageChange(page - 1)}
              aria-label={t('Previous page')}
            >
              <ChevronLeft className='size-4' />
            </Button>
            <Button
              variant='outline'
              size='sm'
              className='size-8 p-0'
              disabled={page >= totalPages || loading}
              onClick={() => handlePageChange(page + 1)}
              aria-label={t('Next page')}
            >
              <ChevronRight className='size-4' />
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  )
}
