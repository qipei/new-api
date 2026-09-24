import { render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test } from 'vitest'

import { MiniAppQR } from '..'

test('opens the full QR image with the keyboard and returns focus after closing', async () => {
  const user = userEvent.setup()
  render(<MiniAppQR />)
  const trigger = screen.getByRole('button', {
    name: 'Enlarge mini program QR code',
  })
  trigger.focus()
  await user.keyboard('{Enter}')
  const dialog = await screen.findByRole('dialog', {
    name: 'WeChat Mini Program',
  })
  expect(
    within(dialog).getByRole('img', { name: 'Mini program QR code' })
  ).toHaveAttribute('src', '/miniapp-code.jpg')
  expect(
    within(dialog).getByRole('link', { name: 'Save QR code' })
  ).toHaveAttribute('download', 'token01-miniapp.jpg')
  await user.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(trigger).toHaveFocus()
})
