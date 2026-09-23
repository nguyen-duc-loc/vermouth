import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type Student, TeachingApiError } from '../api/teaching'
import type { StudentEditSheetProps } from './StudentEditSheet'
import { StudentEditSheet } from './StudentEditSheet'

const original: Student = {
  student_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
  name: 'Mai',
  phone: '090-123 456',
  created_at: '2026-09-20T00:00:00Z',
  updated_at: '2026-09-20T01:00:00Z',
}

const current: Student = {
  ...original,
  name: 'Mai Anh',
  phone: '090-999 999',
  updated_at: '2026-09-20T02:00:00Z',
}

function sheetProps(overrides: Partial<StudentEditSheetProps> = {}): StudentEditSheetProps {
  return {
    open: true,
    student: original,
    onOpenChange: vi.fn(),
    onUpdate: vi.fn(async () => current),
    onReadCurrent: vi.fn(async () => current),
    onUpdated: vi.fn(),
    returnFocusRef: createRef<HTMLButtonElement>(),
    ...overrides,
  }
}

beforeEach(() => {
  vi.spyOn(crypto, 'randomUUID')
    .mockReturnValueOnce('018f8f7e-91b0-7cc4-bd8c-f4d9030ca422')
    .mockReturnValueOnce('018f8f7e-91b0-7cc4-bd8c-f4d9030ca423')
})

// covers: AC-4, AC-17, AC-19
describe('StudentEditSheet', () => {
  it('keeps a stale local draft separate until the tutor chooses the current record', async () => {
    const user = userEvent.setup()
    const onUpdate = vi
      .fn<StudentEditSheetProps['onUpdate']>()
      .mockRejectedValueOnce(
        new TeachingApiError(
          {
            error: {
              code: 'student_changed',
              message: 'the student changed after it was read',
              request_id: 'request-1',
            },
          },
          'the student could not be updated',
        ),
      )
      .mockResolvedValueOnce(current)
    const props = sheetProps({ onUpdate })
    render(<StudentEditSheet {...props} />)
    const name = screen.getByLabelText(/Student name/)
    await user.clear(name)
    await user.type(name, 'My local edit')

    await user.click(screen.getByRole('button', { name: 'Save changes' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'This record changed elsewhere. Review both versions before trying again.',
    )
    expect(props.onReadCurrent).toHaveBeenCalledOnce()
    expect(name).toHaveValue('My local edit')
    expect(screen.getByText('Current name: Mai Anh')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: 'Use current record' }))
    expect(name).toHaveValue('Mai Anh')
    await user.clear(name)
    await user.type(name, 'Mai Anh Nguyen')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))

    await waitFor(() => expect(onUpdate).toHaveBeenCalledTimes(2))
    expect(onUpdate).toHaveBeenLastCalledWith(
      {
        expected_updated_at: current.updated_at,
        name: 'Mai Anh Nguyen',
        phone: current.phone,
      },
      '018f8f7e-91b0-7cc4-bd8c-f4d9030ca423',
    )
    expect(props.onUpdated).toHaveBeenCalledWith(current)
  })

  it('links an empty name error to the edit field', async () => {
    const user = userEvent.setup()
    const props = sheetProps()
    render(<StudentEditSheet {...props} />)
    const name = screen.getByLabelText(/Student name/)
    await user.clear(name)

    await user.click(screen.getByRole('button', { name: 'Save changes' }))

    expect(name).toHaveAttribute('aria-invalid', 'true')
    expect(name).toHaveAccessibleDescription('Use 1 through 160 characters.')
    expect(props.onUpdate).not.toHaveBeenCalled()
  })
})
