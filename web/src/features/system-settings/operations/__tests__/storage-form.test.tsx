import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { storageSchema } from '../lib/storage-schema'
import { StorageSection } from '../storage-section'

const originalAdapter = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = originalAdapter
})

test('disabled storage can be tested, edits block tests, and saved blank credentials preserve the write-only contract', async () => {
  const requests: {
    method: string | undefined
    url: string | undefined
    body: unknown
  }[] = []
  const config = {
    enabled: false,
    endpoint: 'oss.example.com',
    region: 'cn-hangzhou',
    bucket: 'images',
    credentials_configured: true,
  }
  api.defaults.adapter = async (request) => {
    requests.push({
      method: request.method,
      url: request.url,
      body: request.data ? JSON.parse(request.data) : undefined,
    })
    return {
      data: {
        success: true,
        message: '',
        data: request.url?.endsWith('/cleanup')
          ? { deleted_files: 3, deleted_bytes: 1024, active_files: 1 }
          : config,
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config: request,
    }
  }
  const user = userEvent.setup()
  const actions = document.createElement('div')
  document.body.append(actions)
  const view = render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <SettingsPageProvider actionsContainer={actions}>
        <StorageSection />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  const testButton = await screen.findByRole('button', {
    name: 'Test saved storage',
  })
  await user.click(testButton)
  await waitFor(() =>
    expect(requests.some((r) => r.url?.endsWith('/test'))).toBe(true)
  )
  const cleanupButton = screen.getByRole('button', {
    name: 'Clean expired temporary files',
  })
  const successToast = vi.spyOn(toast, 'success')
  await user.click(cleanupButton)
  await waitFor(() =>
    expect(successToast).toHaveBeenCalledWith(
      'Deleted 3 expired temporary files.'
    )
  )
  expect(requests.find((r) => r.url?.endsWith('/cleanup'))?.method).toBe('post')
  expect(
    screen.getByLabelText('Local temporary file retention (hours)')
  ).toHaveValue(1)
  expect(
    screen.getByLabelText('Temporary file cleanup interval (minutes)')
  ).toHaveValue(30)
  const concurrency = screen.getByLabelText(
    'Maximum concurrent uploads per instance'
  )
  expect(concurrency).toHaveValue(2)
  expect(concurrency).toHaveAttribute('max', '30')
  expect(
    screen.getByLabelText('Local temporary file retention (hours)')
  ).toHaveAttribute('max', '168')
  expect(concurrency).toHaveAccessibleDescription(
    /Extra requests return 429 without queuing/
  )
  await user.clear(concurrency)
  await user.type(concurrency, '30')
  expect(screen.getAllByText('Saved; leave blank to keep')).toHaveLength(2)
  const bucket = screen.getByLabelText('OSS bucket')
  await user.type(bucket, '-new')
  expect(testButton).toBeDisabled()
  expect(cleanupButton).toBeDisabled()
  expect(
    screen.getByRole('button', { name: 'Check bucket lifecycle' })
  ).toBeDisabled()
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(requests.find((r) => r.method === 'put')?.body).toMatchObject({
      access_key_id: '',
      access_key_secret: '',
      bucket: 'images-new',
      url_ttl_hours: 24,
      temp_retention_hours: 1,
      temp_cleanup_interval_minutes: 30,
      max_concurrent_uploads: 30,
    })
  )
  expect(requests.find((r) => r.method === 'put')?.body).not.toHaveProperty(
    'credentials_configured'
  )
  await waitFor(() => expect(testButton).toBeEnabled())
  view.unmount()
  actions.remove()
})

test.each([0, 31, 1.5, Number.NaN])(
  'concurrency %s is rejected rather than sent as an upload limit',
  (maxConcurrent) => {
    const result = storageSchema.safeParse({
      enabled: false,
      endpoint: '',
      region: '',
      bucket: '',
      access_key_id: '',
      access_key_secret: '',
      public_endpoint: '',
      prefix: 'uploads/images/',
      url_ttl_hours: 24,
      temp_retention_hours: 1,
      temp_cleanup_interval_minutes: 30,
      max_file_mb: 20,
      requests_per_minute: 10,
      daily_limit_mb: 1024,
      max_concurrent_uploads: maxConcurrent,
    })
    expect(result.success).toBe(false)
    expect(result.error?.issues[0].path).toEqual(['max_concurrent_uploads'])
  }
)
test.each([1, 2, 30])(
  'concurrency %s is accepted within the supported integer range',
  (maxConcurrent) => {
    expect(
      storageSchema.safeParse({
        enabled: false,
        endpoint: '',
        region: '',
        bucket: '',
        access_key_id: '',
        access_key_secret: '',
        public_endpoint: '',
        prefix: 'uploads/images/',
        url_ttl_hours: 24,
        temp_retention_hours: 1,
        temp_cleanup_interval_minutes: 30,
        max_file_mb: 20,
        requests_per_minute: 10,
        daily_limit_mb: 1024,
        max_concurrent_uploads: maxConcurrent,
      }).success
    ).toBe(true)
  }
)

test.each([
  ['temp_retention_hours', 0, false],
  ['temp_retention_hours', 169, false],
  ['temp_retention_hours', 1.5, false],
  ['temp_retention_hours', 1, true],
  ['temp_retention_hours', 168, true],
  ['temp_cleanup_interval_minutes', 4, false],
  ['temp_cleanup_interval_minutes', 1441, false],
  ['temp_cleanup_interval_minutes', 5.5, false],
  ['temp_cleanup_interval_minutes', 5, true],
  ['temp_cleanup_interval_minutes', 1440, true],
] as const)('%s validates %s with acceptance %s', (name, value, valid) => {
  expect(storageSchema.shape[name].safeParse(value).success).toBe(valid)
})

test('JSON preserves visual edits, blocks invalid input, saves optional omissions and clears new credentials', async () => {
  const requests: Record<string, unknown>[] = []
  const config = {
    enabled: false,
    endpoint: 'oss-cn-hangzhou.aliyuncs.com',
    region: 'cn-hangzhou',
    bucket: 'images',
    credentials_configured: true,
  }
  api.defaults.adapter = async (request) => {
    if (request.method === 'put') requests.push(JSON.parse(request.data))
    return {
      data: { success: true, message: '', data: config },
      status: 200,
      statusText: 'OK',
      headers: {},
      config: request,
    }
  }
  const user = userEvent.setup()
  const actions = document.createElement('div')
  document.body.append(actions)
  const view = render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <SettingsPageProvider actionsContainer={actions}>
        <StorageSection />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  await screen.findByLabelText('OSS bucket')
  await user.type(screen.getByLabelText('OSS bucket'), '-edited')
  expect(screen.getByLabelText('Custom HTTPS domain')).toHaveAttribute(
    'aria-required',
    'false'
  )
  await user.click(screen.getByRole('button', { name: 'JSON configuration' }))
  const editor = screen.getByRole('textbox', {
    name: 'JSON configuration',
  }) as HTMLTextAreaElement
  const draft = JSON.parse(editor.value)
  expect(draft.bucket).toBe('images-edited')
  fireEvent.change(editor, { target: { value: 'not JSON' } })
  await user.click(screen.getByRole('button', { name: 'Visual configuration' }))
  expect(screen.getByRole('alert')).toBeVisible()
  expect(
    screen.getByRole('textbox', { name: 'JSON configuration' })
  ).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  expect(requests).toHaveLength(0)
  delete draft.public_endpoint
  delete draft.access_key_id
  delete draft.access_key_secret
  fireEvent.change(editor, { target: { value: JSON.stringify(draft) } })
  await user.click(screen.getByRole('button', { name: 'Visual configuration' }))
  expect(screen.getByLabelText('OSS bucket')).toHaveValue('images-edited')
  expect(screen.getByLabelText('Custom HTTPS domain')).toHaveValue('')
  await user.click(screen.getByRole('button', { name: 'JSON configuration' }))
  const newEditor = screen.getByRole('textbox', {
    name: 'JSON configuration',
  }) as HTMLTextAreaElement
  const savedDraft = JSON.parse(newEditor.value)
  savedDraft.access_key_id = 'new-draft-id'
  savedDraft.access_key_secret = 'new-draft-secret'
  fireEvent.change(newEditor, { target: { value: JSON.stringify(savedDraft) } })
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(requests).toHaveLength(1))
  expect(requests[0]).toMatchObject({
    bucket: 'images-edited',
    public_endpoint: '',
    access_key_id: 'new-draft-id',
    access_key_secret: 'new-draft-secret',
  })
  await waitFor(() =>
    expect(
      (
        screen.getByRole('textbox', {
          name: 'JSON configuration',
        }) as HTMLTextAreaElement
      ).value
    ).not.toContain('new-draft-secret')
  )
  view.unmount()
  actions.remove()
})
