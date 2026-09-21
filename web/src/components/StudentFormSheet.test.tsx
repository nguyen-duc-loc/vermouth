import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { StudentFormSheetProps } from './StudentFormSheet'
import { StudentFormSheet } from './StudentFormSheet'

const createdStudent = {
  student_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
  name: 'Mai',
  phone: '090-123 456',
  created_at: '2026-09-20T00:00:00Z',
  updated_at: '2026-09-20T00:00:00Z',
}

function sheetProps(overrides: Partial<StudentFormSheetProps> = {}): StudentFormSheetProps {
  return {
    open: true,
    onOpenChange: vi.fn(),
    onCreate: vi.fn(async () => createdStudent),
    onCreated: vi.fn(),
    returnFocusRef: createRef<HTMLButtonElement>(),
    ...overrides,
  }
}

beforeEach(() => {
  vi.spyOn(crypto, 'randomUUID').mockReturnValue('018f8f7e-91b0-7cc4-bd8c-f4d9030ca422')
})

// covers: AC-1, AC-17, AC-19
describe('StudentFormSheet', () => {
  it('trims the record and announces the created student to its caller', async () => {
    const user = userEvent.setup()
    const props = sheetProps()
    render(<StudentFormSheet {...props} />)

    await user.type(screen.getByLabelText(/Student name/), '  Mai  ')
    await user.type(screen.getByLabelText('Phone'), '  090-123 456  ')
    await user.click(screen.getByRole('button', { name: 'Create student' }))

    await waitFor(() => expect(props.onCreated).toHaveBeenCalledWith(createdStudent))
    expect(props.onCreate).toHaveBeenCalledWith(
      { name: 'Mai', phone: '090-123 456' },
      '018f8f7e-91b0-7cc4-bd8c-f4d9030ca422',
    )
    expect(props.onOpenChange).toHaveBeenCalledWith(false)
  })

  it('links the required name error to the empty field', async () => {
    const user = userEvent.setup()
    const props = sheetProps()
    render(<StudentFormSheet {...props} />)

    await user.type(screen.getByLabelText(/Student name/), '   ')
    await user.click(screen.getByRole('button', { name: 'Create student' }))

    const name = screen.getByLabelText(/Student name/)
    expect(name).toHaveAttribute('aria-invalid', 'true')
    expect(name).toHaveAccessibleDescription('Use 1 through 160 characters.')
    expect(props.onCreate).not.toHaveBeenCalled()
  })

  it('keeps the command key and draft for an unchanged retry', async () => {
    const user = userEvent.setup()
    const onCreate = vi
      .fn<StudentFormSheetProps['onCreate']>()
      .mockRejectedValueOnce(new Error('Teaching did not answer. Try again.'))
      .mockResolvedValueOnce(createdStudent)
    const props = sheetProps({ onCreate })
    render(<StudentFormSheet {...props} />)
    await user.type(screen.getByLabelText(/Student name/), 'Mai')

    await user.click(screen.getByRole('button', { name: 'Create student' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Teaching did not answer. Try again.',
    )
    expect(screen.getByLabelText(/Student name/)).toHaveValue('Mai')
    await user.click(screen.getByRole('button', { name: 'Create student' }))

    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(2))
    expect(onCreate.mock.calls[0]?.[1]).toBe(onCreate.mock.calls[1]?.[1])
  })
})
