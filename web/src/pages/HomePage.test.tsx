import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Home as HomeData, HomeSession } from '../api/teaching'
import { AppearanceProvider } from '../appearance/appearance'
import { TooltipProvider } from '../components/ui/tooltip'
import { installMatchMedia } from '../test/setup'
import { HomePage } from './HomePage'

const api = vi.hoisted(() => ({
  createClass: vi.fn(),
  createStudent: vi.fn(),
  joinRoster: vi.fn(),
  markAttendance: vi.fn(),
  navigate: vi.fn(),
  readBillingProjection: vi.fn(),
  readHome: vi.fn(),
  signOut: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => api.navigate,
  useRouterState: ({
    select,
  }: {
    select: (state: { location: { pathname: string } }) => unknown
  }) => select({ location: { pathname: '/' } }),
}))

vi.mock('../api/session', () => ({
  signOut: api.signOut,
}))

vi.mock('../api/teaching', async (importOriginal) => {
  const original = await importOriginal<typeof import('../api/teaching')>()
  return {
    ...original,
    createClass: api.createClass,
    createStudent: api.createStudent,
    joinRoster: api.joinRoster,
    markAttendance: api.markAttendance,
    readBillingProjection: api.readBillingProjection,
    readHome: api.readHome,
  }
})

function teachingSession(overrides: Partial<HomeSession> = {}): HomeSession {
  return {
    session_id: 'session-1',
    class_id: 'class-1',
    class_name: 'Maths 9A',
    class_color: 'blue',
    starts_at: '2026-08-30T03:00:00Z',
    ends_at: '2026-08-30T04:00:00Z',
    local_date: '2026-08-30',
    students: [
      {
        student_id: 'student-1',
        name: 'Mai',
        attendance_state: null,
        marked_at: null,
      },
    ],
    ...overrides,
  }
}

function home(overrides: Partial<HomeData> = {}): HomeData {
  return {
    tutor: {
      tutor_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
      email: 'tutor@example.com',
      display_name: 'Tutor',
      timezone: 'Asia/Ho_Chi_Minh',
      language: 'en',
      created_at: '2026-08-01T00:00:00Z',
    },
    request_time_zone: 'Asia/Ho_Chi_Minh',
    local_date: '2026-08-30',
    next_local_midnight_at: '2026-08-30T17:00:00Z',
    setup_defaults: {
      local_date: '2026-08-30',
      start_time: '10:00',
      end_time: '11:00',
    },
    sessions: [],
    next_cursor: null,
    billing_projection: {
      state: 'active',
      class_count: 1,
      session_count: 1,
      student_count: 1,
      open_roster_count: 1,
      attendance_count: 1,
      latest_updated_at: '2026-08-30T03:30:00Z',
    },
    billing_projection_unavailable: null,
    ...overrides,
  }
}

function renderHome(): QueryClient {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
  render(
    <AppearanceProvider>
      <TooltipProvider delayDuration={0}>
        <QueryClientProvider client={queryClient}>
          <HomePage />
        </QueryClientProvider>
      </TooltipProvider>
    </AppearanceProvider>,
  )
  return queryClient
}

beforeEach(() => {
  installMatchMedia(true)
  window.sessionStorage.clear()
  api.navigate.mockReset()
  api.readHome.mockReset()
  api.readBillingProjection.mockReset()
  api.createClass.mockReset()
  api.createStudent.mockReset()
  api.joinRoster.mockReset()
  api.markAttendance.mockReset()
  api.signOut.mockReset()
  api.signOut.mockResolvedValue({ status: 'anonymous' })
  vi.spyOn(crypto, 'randomUUID').mockReturnValue('018f8f7e-91b0-7cc4-bd8c-f4d9030ca422')
})

afterEach(() => {
  vi.useRealTimers()
})

// covers: AC-1, AC-3, AC-5, AC-7, AC-9, AC-13, AC-14, AC-15
describe('HomePage', () => {
  it('renders the local day and opens setup from an honest empty state', async () => {
    const user = userEvent.setup()
    api.readHome.mockResolvedValue(home())
    renderHome()

    expect(await screen.findByRole('heading', { name: "Today's sessions" })).toBeInTheDocument()
    expect(screen.getByText('Sunday, August 30, 2026')).toBeInTheDocument()
    expect(screen.getByText('No sessions for this local date')).toBeInTheDocument()
    expect(screen.getAllByText('Projection active')).not.toHaveLength(0)

    await user.click(screen.getByRole('button', { name: 'Set up the first class' }))

    expect(screen.getByRole('dialog', { name: 'Set up your teaching day' })).toBeInTheDocument()
  })

  it('shows the canonical attendance response instead of inventing local state', async () => {
    const user = userEvent.setup()
    api.readHome.mockResolvedValueOnce(home({ sessions: [teachingSession()] })).mockResolvedValue(
      home({
        sessions: [
          teachingSession({
            students: [
              {
                student_id: 'student-1',
                name: 'Mai',
                attendance_state: 'Present',
                marked_at: '2026-08-30T03:31:00Z',
              },
            ],
          }),
        ],
      }),
    )
    api.markAttendance.mockResolvedValue({
      session_id: 'session-1',
      student_id: 'student-1',
      state: 'Present',
      marked_at: '2026-08-30T03:31:00Z',
    })
    renderHome()

    const present = await screen.findByRole('radio', { name: 'Present' })
    await user.click(present)

    expect(api.markAttendance).toHaveBeenCalledWith('session-1', 'student-1', {
      state: 'Present',
    })
    expect(await screen.findByText('Attendance saved as Present.')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('radio', { name: 'Present' })).toBeChecked())
  })

  it('loads each opaque cursor page without replacing earlier sessions', async () => {
    const user = userEvent.setup()
    api.readHome.mockImplementation(async (cursor: string | undefined) =>
      cursor
        ? home({
            sessions: [
              teachingSession({
                session_id: 'session-2',
                class_id: 'class-2',
                class_name: 'Physics 10B',
              }),
            ],
          })
        : home({ sessions: [teachingSession()], next_cursor: 'cursor-2' }),
    )
    renderHome()

    expect(await screen.findByRole('heading', { name: 'Maths 9A' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Load more sessions' }))

    expect(await screen.findByRole('heading', { name: 'Physics 10B' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Maths 9A' })).toBeInTheDocument()
    expect(api.readHome).toHaveBeenLastCalledWith('cursor-2', expect.any(AbortSignal))
  })

  it('stops projection polling after ten seconds and resumes on a deliberate retry', async () => {
    const user = userEvent.setup()
    let timeoutCallback: (() => void) | undefined
    const nativeSetTimeout = window.setTimeout.bind(window)
    vi.spyOn(window, 'setTimeout').mockImplementation((handler: TimerHandler, timeout?: number) => {
      if (timeout === 10_000 && typeof handler === 'function') {
        timeoutCallback = () => handler()
        return 10
      }
      return nativeSetTimeout(handler, timeout)
    })
    const waiting = {
      state: 'waiting' as const,
      class_count: 1,
      session_count: 1,
      student_count: 1,
      open_roster_count: 1,
      attendance_count: 0,
      latest_updated_at: null,
    }
    api.readHome.mockResolvedValue(home({ billing_projection: waiting }))
    api.readBillingProjection.mockResolvedValue({
      billing_projection: waiting,
      billing_projection_unavailable: null,
    })
    renderHome()

    expect((await screen.findAllByText('Catching up')).length).toBeGreaterThan(0)
    expect(timeoutCallback).toBeDefined()
    act(() => timeoutCallback?.())

    const retries = await screen.findAllByRole('button', { name: 'Check billing again' })
    const callsBeforeRetry = api.readBillingProjection.mock.calls.length
    await user.click(retries[0] as HTMLButtonElement)

    await waitFor(() => {
      expect(api.readBillingProjection.mock.calls.length).toBeGreaterThan(callsBeforeRetry)
    })
  })

  it('resets the cursor chain on focus and at the next local midnight', async () => {
    let midnightCallback: (() => void) | undefined
    const nativeSetTimeout = window.setTimeout.bind(window)
    const midnight = new Date(Date.now() + 60_000).toISOString()
    vi.spyOn(window, 'setTimeout').mockImplementation((handler: TimerHandler, timeout?: number) => {
      if (
        timeout !== undefined &&
        timeout >= 59_000 &&
        timeout <= 60_000 &&
        typeof handler === 'function'
      ) {
        midnightCallback = () => handler()
        return 11
      }
      return nativeSetTimeout(handler, timeout)
    })
    api.readHome.mockResolvedValue(home({ next_local_midnight_at: midnight }))
    renderHome()
    await screen.findByRole('heading', { name: "Today's sessions" })
    expect(midnightCallback).toBeDefined()

    window.dispatchEvent(new Event('focus'))
    await waitFor(() => expect(api.readHome.mock.calls.length).toBeGreaterThanOrEqual(2))
    const callsAfterFocus = api.readHome.mock.calls.length

    act(() => midnightCallback?.())
    await waitFor(() => expect(api.readHome.mock.calls.length).toBeGreaterThan(callsAfterFocus))
  })

  it('clears private setup data before signing out and returning to sign in', async () => {
    const user = userEvent.setup()
    api.readHome.mockResolvedValue(home())
    window.sessionStorage.setItem('vermouth.teaching-setup.v1:tutor-1', 'private phone')
    renderHome()
    await screen.findByRole('heading', { name: "Today's sessions" })

    const accountButtons = screen.getAllByRole('button', { name: 'Account and appearance' })
    await user.click(accountButtons[0] as HTMLButtonElement)
    await user.click(screen.getByRole('button', { name: 'Sign out' }))

    expect(window.sessionStorage.length).toBe(0)
    expect(api.signOut).toHaveBeenCalledOnce()
    expect(api.navigate).toHaveBeenCalledWith({ to: '/signin', search: { redirect: '/' } })
  })
})
