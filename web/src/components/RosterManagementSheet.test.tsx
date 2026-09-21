import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ClassRoster } from '../api/teaching'
import type { RosterManagementSheetProps } from './RosterManagementSheet'
import { RosterManagementSheet } from './RosterManagementSheet'

const api = vi.hoisted(() => ({
  changeClassRoster: vi.fn(),
  createStudent: vi.fn(),
  readStudents: vi.fn(),
}))

vi.mock('../api/teaching', async (importOriginal) => {
  const original = await importOriginal<typeof import('../api/teaching')>()
  return {
    ...original,
    changeClassRoster: api.changeClassRoster,
    createStudent: api.createStudent,
    readStudents: api.readStudents,
  }
})

const roster: ClassRoster = {
  class: {
    class_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
    name: 'Maths 9A',
    color: 'blue',
  },
  resolved_date: '2026-09-20',
  students: [
    {
      student_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca422',
      name: 'Mai',
      phone: '090-111',
      archived: false,
      effective_from: '2026-09-01',
      effective_to: null,
    },
  ],
}

const lan = {
  student_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca423',
  name: 'Lan',
  phone: null,
  active_class_count: 0,
  updated_at: '2026-09-20T00:00:00Z',
}

function renderSheet(overrides: Partial<RosterManagementSheetProps> = {}) {
  const props: RosterManagementSheetProps = {
    open: true,
    tutorId: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca424',
    roster,
    onOpenChange: vi.fn(),
    onSaved: vi.fn(),
    onConflict: vi.fn(async () => undefined),
    returnFocusRef: createRef<HTMLButtonElement>(),
    ...overrides,
  }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <RosterManagementSheet {...props} />
    </QueryClientProvider>,
  )
  return props
}

beforeEach(() => {
  api.changeClassRoster.mockReset()
  api.createStudent.mockReset()
  api.readStudents.mockReset()
  api.readStudents.mockResolvedValue({ students: [lan], next_cursor: null })
  api.changeClassRoster.mockResolvedValue({ ...roster, students: [] })
  vi.spyOn(crypto, 'randomUUID').mockReturnValue('018f8f7e-91b0-7cc4-bd8c-f4d9030ca425')
})

// covers: AC-7, AC-9, AC-17, AC-19
describe('RosterManagementSheet', () => {
  it('saves staged additions and removals as one dated delta', async () => {
    const user = userEvent.setup()
    const props = renderSheet()
    const mai = await screen.findByRole('checkbox', { name: /Mai/ })
    const lanCheckbox = await screen.findByRole('checkbox', { name: /Lan/ })
    expect(mai).toBeChecked()
    expect(lanCheckbox).not.toBeChecked()

    await user.click(mai)
    await user.click(lanCheckbox)
    await user.click(screen.getByRole('button', { name: 'Save roster' }))

    await waitFor(() => expect(api.changeClassRoster).toHaveBeenCalledOnce())
    expect(api.changeClassRoster).toHaveBeenCalledWith(
      roster.class.class_id,
      {
        change_date: roster.resolved_date,
        additions: [lan.student_id],
        removals: [roster.students[0]?.student_id],
      },
      '018f8f7e-91b0-7cc4-bd8c-f4d9030ca425',
    )
    expect(props.onSaved).toHaveBeenCalledOnce()
    expect(props.onOpenChange).toHaveBeenCalledWith(false)
  })

  it('keeps the staged delta while creating a missing student', async () => {
    const user = userEvent.setup()
    const nhi = {
      student_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca426',
      name: 'Nhi',
      phone: null,
      created_at: '2026-09-20T00:00:00Z',
      updated_at: '2026-09-20T00:00:00Z',
    }
    api.createStudent.mockResolvedValue(nhi)
    api.readStudents
      .mockResolvedValueOnce({ students: [lan], next_cursor: null })
      .mockResolvedValue({ students: [lan, { ...nhi, active_class_count: 0 }], next_cursor: null })
    renderSheet()
    const lanCheckbox = await screen.findByRole('checkbox', { name: /Lan/ })
    await user.click(lanCheckbox)

    await user.click(screen.getByRole('button', { name: 'Create' }))
    await user.type(screen.getByLabelText(/Student name/), 'Nhi')
    await user.click(screen.getByRole('button', { name: 'Create student' }))

    await waitFor(() => expect(api.createStudent).toHaveBeenCalledOnce())
    expect(lanCheckbox).toBeChecked()
    expect(await screen.findByRole('checkbox', { name: /Nhi/ })).toBeChecked()
  })
})
