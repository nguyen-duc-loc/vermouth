import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { HomeSession } from '../api/teaching'
import { SessionAttendanceCard } from './SessionAttendanceCard'

function session(students: HomeSession['students'] = []): HomeSession {
  return {
    session_id: 'session-1',
    class_id: 'class-1',
    class_name: 'Maths 9A',
    class_color: 'blue',
    starts_at: '2026-08-30T03:00:00Z',
    ends_at: '2026-08-30T04:00:00Z',
    local_date: '2026-08-30',
    students,
  }
}

const student = {
  student_id: 'student-1',
  name: 'Mai',
  attendance_state: null,
  marked_at: null,
} as const

// covers: AC-3, AC-5, AC-6, AC-7, AC-13, AC-15
describe('SessionAttendanceCard', () => {
  it('pairs class identity with token timezone times and a labelled attendance group', () => {
    render(
      <SessionAttendanceCard
        session={session([student])}
        timeZone="Asia/Ho_Chi_Minh"
        onMark={vi.fn()}
      />,
    )

    expect(screen.getByRole('heading', { name: 'Maths 9A' })).toBeInTheDocument()
    expect(screen.getByText('10:00')).toBeInTheDocument()
    expect(screen.getByText('11:00')).toBeInTheDocument()
    expect(screen.getByText('1 student')).toBeInTheDocument()
    expect(screen.getByRole('radiogroup', { name: 'Mai' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Not marked' })).toBeChecked()
  })

  it('marks attendance through named 44 pixel radio controls', async () => {
    const user = userEvent.setup()
    const onMark = vi.fn()
    render(<SessionAttendanceCard session={session([student])} timeZone="UTC" onMark={onMark} />)

    const present = screen.getByRole('radio', { name: 'Present' })
    await user.click(present)

    expect(onMark).toHaveBeenCalledWith('student-1', 'Present')
  })

  it('disables a pending student and announces saved and failed outcomes', () => {
    render(
      <SessionAttendanceCard
        session={session([{ ...student, attendance_state: 'Present' }])}
        timeZone="UTC"
        pendingStudentId="student-1"
        savedMessage="Attendance saved as Present."
        error="Attendance could not be saved. Try again."
        onMark={vi.fn()}
      />,
    )

    expect(screen.getByRole('radio', { name: 'Present' })).toBeChecked()
    expect(screen.getByRole('radio', { name: 'Present' })).toBeDisabled()
    expect(screen.getByText(/Saving Mai attendance/)).toBeInTheDocument()
    expect(screen.getByText('Attendance saved as Present.')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Attendance could not be saved. Try again.')
  })

  it('shows a calm empty roster instead of rendering unusable controls', () => {
    render(<SessionAttendanceCard session={session()} timeZone="UTC" onMark={vi.fn()} />)

    expect(screen.getByText('No covered students are in this session yet.')).toBeInTheDocument()
    expect(screen.queryByRole('radiogroup')).not.toBeInTheDocument()
  })
})
