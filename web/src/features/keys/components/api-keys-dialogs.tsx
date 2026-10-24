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
import { getRouteApi } from '@tanstack/react-router'
import { useEffect } from 'react'

import { ApiKeysDeleteDialog } from './api-keys-delete-dialog'
import { ApiKeysMutateDrawer } from './api-keys-mutate-drawer'
import { useApiKeys } from './api-keys-provider'
import { CCSwitchDialog } from './dialogs/cc-switch-dialog'

const route = getRouteApi('/_authenticated/keys/')

export function ApiKeysDialogs() {
  const { open, setOpen, currentRow, resolvedKey } = useApiKeys()
  // CUSTOM: 带 create_group 进入时直接打开创建抽屉，关闭后从地址里去掉该参数（fork 扩展）
  const presetGroup = route.useSearch().create_group
  const navigate = route.useNavigate()
  useEffect(() => {
    if (presetGroup) setOpen('create')
  }, [presetGroup, setOpen])

  return (
    <>
      <ApiKeysMutateDrawer
        open={open === 'create' || open === 'update'}
        onOpenChange={(isOpen) => {
          if (isOpen) return
          setOpen(null)
          if (presetGroup) {
            void navigate({
              search: (prev) => ({ ...prev, create_group: undefined }),
              replace: true,
            })
          }
        }}
        currentRow={open === 'update' ? currentRow || undefined : undefined}
        presetGroup={open === 'create' ? presetGroup : undefined}
      />
      <ApiKeysDeleteDialog />
      <CCSwitchDialog
        open={open === 'cc-switch'}
        onOpenChange={(isOpen) => !isOpen && setOpen(null)}
        tokenKey={resolvedKey}
      />
    </>
  )
}
