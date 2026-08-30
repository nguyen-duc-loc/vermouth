import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef, useRef, useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { TeachingSetupSheetProps } from './TeachingSetupSheet'
import { TeachingSetupSheet } from './TeachingSetupSheet'

const defaults = {
  local_date: '2026-08-30',
  start_time: '10:00',
  end_time: '11:00',
}

const classResult = {
  class: {
    class_id: 'class-1',
    name: 'Maths 9A',
    color: 'blue' as const,
    rate_amount: 250_000,
    currency: 'VND' as const,
    rate_effective_from: '2026-08-30',
  },
  first_session: {
    session_id: 'session-1',
    class_id: 'class-1',
    starts_at: '2026-08-30T03:00:00Z',
    ends_at: '2026-08-30T04:00:00Z',
    local_date: '2026-08-30',
  },
}

function sheetProps(overrides: Partial<TeachingSetupSheetProps> = {}): TeachingSetupSheetProps {
  return {
    open: true,
    tutorId: 'tutor-1',
    defaults,
    onOpenChange: vi.fn(),
    onCreateClass: vi.fn(async () => classResult),
    onCreateStudent: vi.fn(async () => ({
      student_id: 'student-1',
      name: 'Mai',
      phone: null,
    })),
    onJoinRoster: vi.fn(async () => ({
      class_id: 'class-1',
      student_id: 'student-1',
      effective_from: '2026-08-30',
      effective_to: null,
    })),
    onComplete: vi.fn(),
    returnFocusRef: createRef<HTMLButtonElement>(),
    ...overrides,
  }
}

beforeEach(() => {
  window.sessionStorage.clear()
  vi.spyOn(crypto, 'randomUUID').mockReturnValue('018f8f7e-91b0-7cc4-bd8c-f4d9030ca421')
})

// covers: AC-1, AC-2, AC-3, AC-4, AC-10, AC-13, AC-14
describe('TeachingSetupSheet', () => {
  it('completes the three saved steps with the original command keys', async () => {
    const user = userEvent.setup()
    const props = sheetProps()
    render(<TeachingSetupSheet {...props} />)

    expect(screen.getByRole('dialog', { name: 'Set up your teaching day' })).toContainElement(
      document.activeElement as HTMLElement,
    )
    expect(screen.getByText('Step 1 of 3')).toBeInTheDocument()
    await user.type(screen.getByLabelText('Class name'), 'Maths 9A')
    await user.type(screen.getByLabelText('Rate per present session'), '250000')
    await user.click(screen.getByRole('button', { name: 'Create class and session' }))

    expect(await screen.findByText('Step 2 of 3')).toBeInTheDocument()
    expect(props.onCreateClass).toHaveBeenCalledWith(
      {
        name: 'Maths 9A',
        color: null,
        rate_amount: 250_000,
        first_session: {
          local_date: '2026-08-30',
          start_time: '10:00',
          end_time: '11:00',
        },
      },
      '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
    )

    await user.type(screen.getByLabelText('Student name'), 'Mai')
    await user.click(screen.getByRole('button', { name: 'Create student' }))

    expect(await screen.findByText('Step 3 of 3')).toBeInTheDocument()
    expect(props.onCreateStudent).toHaveBeenCalledWith(
      { name: 'Mai', phone: null },
      '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
    )

    await user.click(screen.getByRole('button', { name: 'Complete roster' }))

    expect(props.onJoinRoster).toHaveBeenCalledWith('class-1', {
      student_id: 'student-1',
      effective_from: '2026-08-30',
    })
    expect(props.onComplete).toHaveBeenCalledWith('2026-08-30')
    expect(props.onOpenChange).toHaveBeenCalledWith(false)
    expect(window.sessionStorage.getItem('vermouth.teaching-setup.v1:tutor-1')).toBeNull()
  })

  it('links blocking validation messages to the fields that need recovery', async () => {
    const user = userEvent.setup()
    const props = sheetProps()
    render(<TeachingSetupSheet {...props} />)

    await user.click(screen.getByRole('button', { name: 'Create class and session' }))

    const className = screen.getByLabelText('Class name')
    const rate = screen.getByLabelText('Rate per present session')
    expect(className).toHaveAttribute('aria-invalid', 'true')
    expect(className).toHaveAccessibleDescription('Use 1 through 120 characters.')
    expect(rate).toHaveAttribute('aria-invalid', 'true')
    expect(rate).toHaveAccessibleDescription(
      'Whole Vietnamese dong, from 0 through 1,000,000,000. Enter whole dong only.',
    )
    expect(props.onCreateClass).not.toHaveBeenCalled()
  })

  it('keeps the step and command key available after a failed save', async () => {
    const user = userEvent.setup()
    const onCreateClass = vi
      .fn<TeachingSetupSheetProps['onCreateClass']>()
      .mockRejectedValueOnce(new Error('Teaching did not answer. Try again.'))
      .mockResolvedValueOnce(classResult)
    const props = sheetProps({ onCreateClass })
    render(<TeachingSetupSheet {...props} />)
    await user.type(screen.getByLabelText('Class name'), 'Maths 9A')
    await user.type(screen.getByLabelText('Rate per present session'), '250000')

    await user.click(screen.getByRole('button', { name: 'Create class and session' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Teaching did not answer. Try again. Your entries and this step are still saved.',
    )
    expect(screen.getByText('Step 1 of 3')).toBeInTheDocument()
    expect(window.sessionStorage.getItem('vermouth.teaching-setup.v1:tutor-1')).toContain(
      'Maths 9A',
    )

    await user.click(screen.getByRole('button', { name: 'Create class and session' }))

    expect(await screen.findByText('Step 2 of 3')).toBeInTheDocument()
    expect(onCreateClass).toHaveBeenCalledTimes(2)
    expect(onCreateClass.mock.calls[0]?.[1]).toBe(onCreateClass.mock.calls[1]?.[1])
  })

  it('traps focus while open and returns it to the caller trigger on close', async () => {
    const user = userEvent.setup()

    function Harness() {
      const [open, setOpen] = useState(false)
      const triggerRef = useRef<HTMLButtonElement>(null)
      return (
        <>
          <button ref={triggerRef} type="button" onClick={() => setOpen(true)}>
            Open class setup
          </button>
          <TeachingSetupSheet
            {...sheetProps({
              open,
              onOpenChange: setOpen,
              returnFocusRef: triggerRef,
            })}
          />
        </>
      )
    }

    render(<Harness />)
    const trigger = screen.getByRole('button', { name: 'Open class setup' })
    await user.click(trigger)
    const dialog = screen.getByRole('dialog', { name: 'Set up your teaching day' })
    expect(dialog).toContainElement(document.activeElement as HTMLElement)

    await user.click(screen.getByRole('button', { name: 'Close class setup' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })
})
