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
import { beforeEach, expect, test, vi } from 'vitest'

import { useNotificationStore } from '../notification-store'

beforeEach(() => {
  useNotificationStore.setState({ lastReadNotice: '' })
})

test('storage write failure does not throw or lose in-memory read state', () => {
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('quota exceeded')
  })
  expect(() =>
    useNotificationStore.getState().markNoticeRead('  Notice  ')
  ).not.toThrow()
  expect(useNotificationStore.getState().lastReadNotice).toBe('Notice')
})

test('storage read failure during rehydration keeps read actions usable', async () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('denied')
  })
  await useNotificationStore.persist.rehydrate()
  expect(useNotificationStore.persist.hasHydrated()).toBe(true)
  expect(() =>
    useNotificationStore.getState().markNoticeRead('Notice')
  ).not.toThrow()
})
