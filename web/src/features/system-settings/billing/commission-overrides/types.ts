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
/** 单个推广人的专属返佣参数。 */
export interface CommissionOverride {
  id: number
  user_id: number
  username: string
  type: 'fixed' | 'percent'
  value: number
  topup_count_limit: number
  remark: string
  created_time: number
  updated_time: number
}

/** 新增或更新时提交的内容，按用户名指定对象。 */
export interface CommissionOverridePayload {
  user_id?: number
  username?: string
  type: 'fixed' | 'percent'
  value: number
  topup_count_limit: number
  remark: string
}
