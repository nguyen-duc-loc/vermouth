import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type AttendanceSheet as AttendanceSheetData, TeachingApiError } from '../api/teaching'
import { AttendanceSheet } from './AttendanceSheet'

vi.mock('@tanstack/react-router', () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}))

const api = vi.hoisted(() => ({
  readAttendance: vi.fn(),
  saveAttendance: vi.fn(),
}))

vi.mock('../api/teaching', async (importOriginal) => {
  const original = await importOriginal<typeof import('../api/teaching')>()
  return {
    ...original,
    readAttendance: api.readAttendance,
    saveAttendance: api.saveAttendance,
  }
})

function sheet(revision: string, students: AttendanceSheetData['students']): AttendanceSheetData {
  return {
    session: {
      session_id: 'session-1',
      class_id: 'class-1',
      class_name: 'Maths 9A',
      class_color: 'blue',
      starts_at: '2026-08-30T03:00:00Z',
      ends_at: '2026-08-30T04:00:00Z',
      local_date: '2026-08-30',
      state: 'active',
    },
    eligible: true,
    ineligible_reason: null,
    revision,
    students,
  }
}

function renderSheet(onSaved = vi.fn(), onOpenChange = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <AttendanceSheet
        open
        tutorId="tutor-1"
        sessionId="session-1"
        onOpenChange={onOpenChange}
        onSaved={onSaved}
        returnFocusRef={createRef<HTMLButtonElement>()}
      />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  api.readAttendance.mockReset()
  api.saveAttendance.mockReset()
  vi.spyOn(crypto, 'randomUUID').mockReturnValue('018f8f7e-91b0-7cc4-bd8c-f4d9030ca421')
})

// covers: AC-12, AC-13, AC-18, AC-19
describe('AttendanceSheet', () => {
  it('keeps matching local marks and requires every fresh row after a stale revision', async () => {
    const user = userEvent.setup()
    const mai = {
      student_id: 'student-1',
      name: 'Mai',
      archived: false,
      state: null,
      marked_at: null,
    } as const
    const lan = {
      student_id: 'student-2',
      name: 'Lan',
      archived: false,
      state: null,
      marked_at: null,
    } as const
    api.readAttendance
      .mockResolvedValueOnce(sheet('revision-1', [mai]))
      .mockResolvedValue(sheet('revision-2', [{ ...mai, name: 'Mai Anh' }, lan]))
    api.saveAttendance
      .mockRejectedValueOnce(
        new TeachingApiError(
          {
            error: {
              code: 'attendance_changed',
              message: 'attendance changed after it was read',
              request_id: 'request-1',
            },
          },
          'attendance could not be saved',
        ),
      )
      .mockResolvedValueOnce({
        session_id: 'session-1',
        marked_at: '2026-08-30T03:30:00Z',
        marks: [
          { student_id: 'student-1', state: 'Present', marked_at: '2026-08-30T03:30:00Z' },
          { student_id: 'student-2', state: 'Absent', marked_at: '2026-08-30T03:30:00Z' },
        ],
      })
    const onSaved = vi.fn()
    const onOpenChange = vi.fn()
    renderSheet(onSaved, onOpenChange)

    const firstGroup = await screen.findByRole('group', { name: 'Mai' })
    await user.click(within(firstGroup).getByRole('radio', { name: 'Present' }))
    await user.click(screen.getByRole('button', { name: 'Save attendance' }))

    expect(
      await screen.findByText(
        'The roster or saved attendance changed. Review every row before saving again.',
      ),
    ).toBeInTheDocument()
    const refreshedMai = screen.getByRole('group', { name: 'Mai Anh' })
    expect(within(refreshedMai).getByRole('radio', { name: 'Present' })).toBeChecked()
    const freshLan = screen.getByRole('group', { name: 'Lan' })
    expect(within(freshLan).getByRole('radio', { name: 'Present' })).not.toBeChecked()
    expect(within(freshLan).getByRole('radio', { name: 'Absent' })).not.toBeChecked()

    await user.click(within(refreshedMai).getByRole('button', { name: 'Confirm Mai Anh' }))
    await user.click(within(freshLan).getByRole('radio', { name: 'Absent' }))
    await user.click(screen.getByRole('button', { name: 'Save attendance' }))

    await waitFor(() => expect(onSaved).toHaveBeenCalledOnce())
    expect(onOpenChange).toHaveBeenCalledWith(false)
    expect(api.saveAttendance).toHaveBeenLastCalledWith(
      'session-1',
      {
        revision: 'revision-2',
        marks: [
          { student_id: 'student-1', state: 'Present' },
          { student_id: 'student-2', state: 'Absent' },
        ],
      },
      '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
    )
  })

  it('links an empty roster to class management without offering a save action', async () => {
    api.readAttendance.mockResolvedValue(sheet('revision-empty', []))
    renderSheet()

    expect(
      await screen.findByText('This session has no students on its dated roster.'),
    ).toBeVisible()
    expect(screen.getByRole('link', { name: 'Manage class roster' })).toHaveAttribute(
      'href',
      '/classes/$classId',
    )
    expect(screen.queryByRole('button', { name: 'Save attendance' })).not.toBeInTheDocument()
  })

  it('keeps one command key for an unchanged network retry and changes it with the marks', async () => {
    const user = userEvent.setup()
    const mai = {
      student_id: 'student-1',
      name: 'Mai',
      archived: false,
      state: null,
      marked_at: null,
    } as const
    vi.mocked(crypto.randomUUID)
      .mockReset()
      .mockReturnValueOnce('018f8f7e-91b0-7cc4-bd8c-f4d9030ca421')
      .mockReturnValueOnce('018f8f7e-91b0-7cc4-bd8c-f4d9030ca422')
    api.readAttendance.mockResolvedValue(sheet('revision-1', [mai]))
    api.saveAttendance
      .mockRejectedValueOnce(new Error('Network unavailable. Try again.'))
      .mockRejectedValueOnce(new Error('Network unavailable. Try again.'))
      .mockResolvedValueOnce({
        session_id: 'session-1',
        marked_at: '2026-08-30T03:30:00Z',
        marks: [{ student_id: 'student-1', state: 'Absent', marked_at: '2026-08-30T03:30:00Z' }],
      })
    renderSheet()
    const group = await screen.findByRole('group', { name: 'Mai' })
    await user.click(within(group).getByRole('radio', { name: 'Present' }))

    await user.click(screen.getByRole('button', { name: 'Save attendance' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Network unavailable. Try again.')
    await user.click(screen.getByRole('button', { name: 'Save attendance' }))
    await waitFor(() => expect(api.saveAttendance).toHaveBeenCalledTimes(2))
    expect(api.saveAttendance.mock.calls[0]?.[2]).toBe(api.saveAttendance.mock.calls[1]?.[2])

    await user.click(within(group).getByRole('radio', { name: 'Absent' }))
    await user.click(screen.getByRole('button', { name: 'Save attendance' }))

    await waitFor(() => expect(api.saveAttendance).toHaveBeenCalledTimes(3))
    expect(api.saveAttendance.mock.calls[2]?.[2]).not.toBe(api.saveAttendance.mock.calls[1]?.[2])
  })
})
