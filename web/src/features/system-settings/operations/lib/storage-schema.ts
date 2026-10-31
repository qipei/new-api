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
import { z } from 'zod'

export const storageSchema = z
  .object({
    enabled: z.boolean(),
    endpoint: z.string().trim(),
    region: z.string().trim(),
    bucket: z.string().trim(),
    access_key_id: z.string(),
    access_key_secret: z.string(),
    public_endpoint: z
      .string()
      .trim()
      .refine((value) => {
        if (!value) return true
        try {
          const url = new URL(value)
          return (
            url.protocol === 'https:' &&
            !url.username &&
            !url.password &&
            url.pathname === '/' &&
            !url.search &&
            !url.hash
          )
        } catch {
          return false
        }
      }),
    prefix: z.string(),
    url_ttl_hours: z.number().int().min(1).max(168),
    temp_retention_hours: z.number().int().min(1).max(168),
    temp_cleanup_interval_minutes: z.number().int().min(5).max(1440),
    max_concurrent_uploads: z.number().int().min(1).max(30),
    max_file_mb: z.number().int().min(1).max(20),
    requests_per_minute: z.number().int().min(1).max(120),
    daily_limit_mb: z.number().int().min(1).max(10240),
  })
  .strict()
  .superRefine((value, ctx) => {
    if (!value.enabled) return
    for (const key of ['endpoint', 'region', 'bucket'] as const) {
      if (!value[key]) {
        ctx.addIssue({ code: 'custom', path: [key], message: 'Required' })
      }
    }
  })

export function parseStorageJSON(text: string, credentialsConfigured: boolean) {
  const raw: unknown = JSON.parse(text)
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    throw new Error('Invalid configuration')
  }
  const value = storageSchema.parse({
    access_key_id: '',
    access_key_secret: '',
    public_endpoint: '',
    ...raw,
  })
  if (
    value.enabled &&
    !credentialsConfigured &&
    (!value.access_key_id.trim() || !value.access_key_secret.trim())
  ) {
    throw new Error('Credentials required')
  }
  return value
}
