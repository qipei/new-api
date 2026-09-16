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
import { Plus, Search } from 'lucide-react'
import * as React from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { BadgeCell } from '@/components/data-table/core/badge-cell'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { formatTimestampToDate } from '@/lib/format'

import { SettingsSection } from '../../components/settings-section'
import { CommissionOverrideDialog } from './components/commission-override-dialog'
import {
  useCommissionOverrideMutations,
  useCommissionOverrides,
} from './hooks/use-commission-overrides'
import type { CommissionOverride } from './types'

const PAGE_SIZE = 10

/**
 * 推广人专属返佣参数列表。
 *
 * 全局参数在上一个区块里配，这里列出所有做了单独设置的推广人。没有记录的推广人
 * 走全局参数，删除一条记录即让该推广人回到全局。
 */
export function CommissionOverridesSection() {
  const { t } = useTranslation()
  const [page, setPage] = React.useState(1)
  const [keywordInput, setKeywordInput] = React.useState('')
  const [keyword, setKeyword] = React.useState('')
  const [dialogOpen, setDialogOpen] = React.useState(false)
  const [editing, setEditing] = React.useState<CommissionOverride | null>(null)
  const [deleteTarget, setDeleteTarget] =
    React.useState<CommissionOverride | null>(null)

  const { data, isLoading } = useCommissionOverrides(page, PAGE_SIZE, keyword)
  const { remove } = useCommissionOverrideMutations()

  const items = data?.items ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const applySearch = () => {
    setKeyword(keywordInput.trim())
    setPage(1)
  }

  const handleCreate = () => {
    setEditing(null)
    setDialogOpen(true)
  }

  const handleEdit = (override: CommissionOverride) => {
    setEditing(override)
    setDialogOpen(true)
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    await remove.mutateAsync(deleteTarget.id)
    setDeleteTarget(null)
  }

  return (
    <SettingsSection title={t('Per-user Commission Overrides')}>
      <div className='flex flex-col gap-4'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Promoters listed here use their own parameters. Everyone else follows the global settings above. The global enable switch still applies to all.'
          )}
        </p>
        <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
          <div className='flex w-full max-w-sm items-center gap-2'>
            <Input
              value={keywordInput}
              onChange={(event) => setKeywordInput(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') applySearch()
              }}
              placeholder={t('Search by promoter username')}
            />
            <Button variant='outline' size='icon' onClick={applySearch}>
              <Search className='h-4 w-4' />
            </Button>
          </div>
          <Button size='sm' onClick={handleCreate}>
            <Plus className='mr-1.5 h-4 w-4' />
            {t('Add commission override')}
          </Button>
        </div>

        <StaticDataTable
          data={items}
          getRowKey={(override) => override.id}
          emptyClassName='text-sm'
          emptyContent={
            isLoading
              ? t('Loading...')
              : t('No promoter has special parameters yet.')
          }
          columns={[
            {
              id: 'username',
              header: t('Promoter'),
              cellClassName: 'font-medium',
              cell: (override) => override.username || `#${override.user_id}`,
            },
            {
              id: 'type',
              header: t('Commission Type'),
              cell: (override) => (
                <BadgeCell>
                  <StatusBadge
                    label={
                      override.type === 'percent'
                        ? t('Percentage')
                        : t('Fixed amount')
                    }
                    variant='neutral'
                    copyable={false}
                  />
                </BadgeCell>
              ),
            },
            {
              id: 'value',
              header: t('Commission Value'),
              cellClassName: 'tabular-nums',
              cell: (override) =>
                override.type === 'percent'
                  ? `${override.value}%`
                  : String(override.value),
            },
            {
              id: 'limit',
              header: t('Commission Count Limit'),
              cellClassName: 'tabular-nums',
              cell: (override) =>
                override.topup_count_limit === 0
                  ? t('Unlimited')
                  : String(override.topup_count_limit),
            },
            {
              id: 'remark',
              header: t('Remark'),
              cellClassName: 'text-muted-foreground max-w-[180px] truncate',
              cell: (override) => override.remark || '--',
            },
            {
              id: 'updated',
              header: t('Updated'),
              cellClassName: 'text-muted-foreground whitespace-nowrap',
              cell: (override) =>
                override.updated_time
                  ? formatTimestampToDate(override.updated_time)
                  : '--',
            },
            {
              id: 'actions',
              header: t('Actions'),
              className: 'text-right',
              cellClassName: 'text-right',
              cell: (override) => (
                <StaticRowActions
                  editLabel={t('Edit')}
                  deleteLabel={t('Delete')}
                  menuLabel={t('Open menu')}
                  onEdit={() => handleEdit(override)}
                  onDelete={() => setDeleteTarget(override)}
                />
              ),
            },
          ]}
        />

        {totalPages > 1 && (
          <div className='flex items-center justify-end gap-2'>
            <span className='text-muted-foreground text-sm tabular-nums'>
              {page} / {totalPages}
            </span>
            <Button
              variant='outline'
              size='sm'
              disabled={page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            >
              {t('Previous')}
            </Button>
            <Button
              variant='outline'
              size='sm'
              disabled={page >= totalPages}
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            >
              {t('Next')}
            </Button>
          </div>
        )}
      </div>

      <CommissionOverrideDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        override={editing}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('Remove commission override')}
        desc={t(
          'Remove the special parameters for "{{name}}"? This promoter will go back to the global settings.',
          { name: deleteTarget?.username || '' }
        )}
        confirmText={t('Delete')}
        destructive
        handleConfirm={handleDelete}
        isLoading={remove.isPending}
      />
    </SettingsSection>
  )
}
