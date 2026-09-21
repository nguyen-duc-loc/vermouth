import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ComponentProps } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Schedule, Tutor } from '../api/teaching'
import { AppearanceProvider } from '../appearance/appearance'
import { TooltipProvider } from '../components/ui/tooltip'
import { installMatchMedia } from '../test/setup'
import { SchedulePage } from './SchedulePage'

type MockLinkProps = Omit<ComponentProps<'a'>, 'href'> & { to: string }

const api = vi.hoisted(() => ({
  cancelSession: vi.fn(),
  endSchedule: vi.fn(),
  moveSession: vi.fn(),
  navigate: vi.fn(),
  putSchedule: vi.fn(),
  readSchedule: vi.fn(),
  readTutor: vi.fn(),
  restoreSession: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  Link: ({ to, children, ...props }: MockLinkProps) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
  getRouteApi: () => ({
    useNavigate: () => api.navigate,
    useSearch: () => ({ classes: [], date: '2026-09-20', view: 'week' }),
  }),
  useRouterState: ({
    select,
  }: {
    select: (state: { location: { pathname: string } }) => unknown
  }) => select({ location: { pathname: '/schedule' } }),
}))

vi.mock('../api/teaching', async (importOriginal) => {
  const original = await importOriginal<typeof import('../api/teaching')>()
  return {
    ...original,
    cancelSession: api.cancelSession,
    endSchedule: api.endSchedule,
    moveSession: api.moveSession,
    putSchedule: api.putSchedule,
    readSchedule: api.readSchedule,
    readTutor: api.readTutor,
    restoreSession: api.restoreSession,
  }
})

const classID = '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421'
const ruleID = '018f8f7e-91b0-7cc4-bd8c-f4d9030ca422'

function tutor(): Tutor {
  return {
    tutor_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca423',
    email: 'tutor@example.com',
    display_name: 'Tutor',
    timezone: 'Asia/Ho_Chi_Minh',
    language: 'en',
    created_at: '2026-09-19T00:00:00Z',
  }
}

function schedule(): Schedule {
  return {
    tutor_id: tutor().tutor_id,
    request_time_zone: tutor().timezone,
    from: '2026-09-14',
    through: '2026-09-20',
    classes: [
      {
        class_id: classID,
        name: 'Calendar verification',
        color: 'blue',
        schedule_revision: 1,
      },
    ],
    rules: [
      {
        schedule_rule_id: ruleID,
        class_id: classID,
        revision: 1,
        valid_from: '2026-09-20',
        valid_through: '2026-10-04',
        time_zone: 'Asia/Ho_Chi_Minh',
        state: 'active',
        slots: [{ weekday: 7, start_time: '14:00', end_time: '15:00' }],
        replaced_at: null,
        ended_at: null,
        retired_at: null,
      },
    ],
    sessions: [
      {
        session_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca424',
        class_id: classID,
        class_name: 'Calendar verification',
        class_color: 'blue',
        class_archived: false,
        starts_at: '2026-09-20T07:00:00Z',
        ends_at: '2026-09-20T08:00:00Z',
        display_date: '2026-09-20',
        display_start: '14:00',
        display_end: '15:00',
        start_utc_offset: '+07:00',
        end_utc_offset: '+07:00',
        local_date: '2026-09-20',
        origin_local_date: '2026-09-20',
        schedule_rule_id: ruleID,
        source_time_zone: 'Asia/Ho_Chi_Minh',
        version: 1,
        state: 'active',
        moved_at: null,
        cancelled_at: null,
        superseded_at: null,
        updated_at: '2026-09-19T00:00:00Z',
      },
    ],
    replaced_history: [],
    next_history_cursor: null,
  }
}

function renderSchedule() {
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
          <SchedulePage />
        </QueryClientProvider>
      </TooltipProvider>
    </AppearanceProvider>,
  )
  return queryClient
}

beforeEach(() => {
  installMatchMedia(false)
  api.cancelSession.mockReset()
  api.endSchedule.mockReset()
  api.moveSession.mockReset()
  api.navigate.mockReset()
  api.putSchedule.mockReset()
  api.readSchedule.mockReset()
  api.readTutor.mockReset()
  api.restoreSession.mockReset()
  api.readTutor.mockResolvedValue(tutor())
  api.readSchedule.mockResolvedValue(schedule())
})

describe('SchedulePage', () => {
  it('AC-4 loads only the weekdays stored on the latest rule', async () => {
    const user = userEvent.setup()
    renderSchedule()

    const manageButtons = await screen.findAllByRole('button', { name: 'Manage schedule' })
    await user.click(manageButtons[0] as HTMLButtonElement)

    expect(screen.getByRole('dialog', { name: 'Schedule for Calendar verification' })).toBeVisible()
    expect(screen.getByRole('checkbox', { name: 'Monday' })).not.toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'Sunday' })).toBeChecked()
  })

  it('AC-15 returns focus to every schedule sheet trigger after close', async () => {
    const user = userEvent.setup()
    renderSchedule()

    const addTrigger = await screen.findByRole('button', { name: 'Add schedule' })
    await user.click(addTrigger)
    await user.click(screen.getByRole('button', { name: 'Close schedule form' }))
    expect.soft(addTrigger).toHaveFocus()

    const manageTrigger = (await screen.findAllByRole('button', { name: 'Manage schedule' }))[0]
    expect(manageTrigger).toBeDefined()
    await user.click(manageTrigger as HTMLButtonElement)
    await user.click(screen.getByRole('button', { name: 'Close schedule form' }))
    expect.soft(manageTrigger).toHaveFocus()

    const sessionTrigger = screen.getByRole('button', {
      name: 'Open Calendar verification session at 14:00',
    })
    await user.click(sessionTrigger)
    await user.click(screen.getByRole('button', { name: 'Close session details' }))
    expect.soft(sessionTrigger).toHaveFocus()
  })

  it('AC-15 returns focus to main content when the sheet trigger is gone', async () => {
    const user = userEvent.setup()
    renderSchedule()

    const trigger = await screen.findByRole('button', {
      name: 'Open Calendar verification session at 14:00',
    })
    await user.click(trigger)
    trigger.remove()
    await user.click(screen.getByRole('button', { name: 'Close session details' }))

    expect(screen.getByRole('main')).toHaveFocus()
  })

  it.each([
    { state: 'active', moved: false, move: true, cancel: true, restore: false },
    { state: 'active', moved: true, move: true, cancel: true, restore: false },
    { state: 'cancelled', moved: true, move: false, cancel: false, restore: true },
    { state: 'replaced', moved: false, move: false, cancel: false, restore: false },
  ] as const)(
    'AC-6, AC-7, and AC-15 expose only valid actions for $state with moved=$moved',
    async ({ state, moved, move, cancel, restore }) => {
      const user = userEvent.setup()
      const result = schedule()
      const session = result.sessions[0]
      expect(session).toBeDefined()
      if (!session) return
      session.state = state
      session.moved_at = moved ? '2026-09-19T01:00:00Z' : null
      session.cancelled_at = state === 'cancelled' ? '2026-09-19T02:00:00Z' : null
      session.superseded_at = state === 'replaced' ? '2026-09-19T03:00:00Z' : null
      api.readSchedule.mockResolvedValue(result)
      renderSchedule()

      const trigger = await screen.findByRole('button', {
        name: 'Open Calendar verification session at 14:00',
      })
      await user.click(trigger)

      expect(screen.queryByRole('button', { name: 'Move session' }) !== null).toBe(move)
      expect(screen.queryByRole('button', { name: 'Cancel session' }) !== null).toBe(cancel)
      expect(screen.queryByRole('button', { name: 'Restore session' }) !== null).toBe(restore)
      expect(screen.getByText(new RegExp(`${state}.*${moved ? 'moved.*' : ''}1`))).toBeVisible()
      if (moved) expect(screen.getAllByText('moved').length).toBeGreaterThan(0)
    },
  )

  it('AC-11 and AC-15 retain the move draft and command key for deliberate retry', async () => {
    const user = userEvent.setup()
    api.moveSession
      .mockRejectedValueOnce(new Error('session overlaps another committed session'))
      .mockResolvedValueOnce({})
    const queryClient = renderSchedule()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')

    await user.click(
      await screen.findByRole('button', {
        name: 'Open Calendar verification session at 14:00',
      }),
    )
    const start = screen.getByLabelText('Start time')
    await user.clear(start)
    await user.type(start, '16:00')
    await user.click(screen.getByRole('button', { name: 'Move session' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'session overlaps another committed session Refresh the calendar and review the current details.',
    )
    expect(start).toHaveValue('16:00')
    await user.click(screen.getByRole('button', { name: 'Move session' }))

    await waitFor(() => expect(api.moveSession).toHaveBeenCalledTimes(2))
    expect(api.moveSession.mock.calls[0]?.[2]).toBe(api.moveSession.mock.calls[1]?.[2])
    await waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ['schedule', tutor().tutor_id] }),
    )
    expect(screen.getByRole('status')).toHaveTextContent('Session moved to 2026-09-20 at 16:00.')
  })

  it('AC-1, AC-4, AC-11, and AC-15 retain a failed schedule draft and announce exact counts', async () => {
    const user = userEvent.setup()
    api.putSchedule
      .mockRejectedValueOnce(new Error('the class revision is stale'))
      .mockResolvedValueOnce({
        created_count: 2,
        adopted_count: 1,
        preserved_count: 3,
        superseded_count: 4,
      })
    renderSchedule()

    const manage = (await screen.findAllByRole('button', { name: 'Manage schedule' }))[0]
    expect(manage).toBeDefined()
    await user.click(manage as HTMLButtonElement)
    const through = screen.getByLabelText('Inclusive end date')
    await user.clear(through)
    await user.type(through, '2026-10-11')
    await user.click(screen.getByRole('button', { name: 'Save schedule' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'the class revision is stale Keep this draft, refresh the calendar, then try again with the current revision.',
    )
    expect(through).toHaveValue('2026-10-11')
    await user.click(screen.getByRole('button', { name: 'Save schedule' }))

    await waitFor(() => expect(api.putSchedule).toHaveBeenCalledTimes(2))
    expect(api.putSchedule.mock.calls[0]?.[2]).toBe(api.putSchedule.mock.calls[1]?.[2])
    expect(screen.getByRole('status')).toHaveTextContent(
      'Schedule saved. 2 created, 1 adopted, 3 exceptions preserved, and 4 sessions replaced.',
    )
  })

  it('AC-12 and AC-15 append replaced history pages without losing the first page', async () => {
    const user = userEvent.setup()
    const first = schedule()
    const baseSession = first.sessions[0]
    expect(baseSession).toBeDefined()
    if (!baseSession) return
    const firstHistory = { ...baseSession }
    firstHistory.session_id = '018f8f7e-91b0-7cc4-bd8c-f4d9030ca425'
    firstHistory.state = 'replaced'
    firstHistory.superseded_at = '2026-09-19T03:00:00Z'
    first.replaced_history = [firstHistory]
    first.next_history_cursor = 'next-page'
    const nextHistory = { ...firstHistory }
    nextHistory.session_id = '018f8f7e-91b0-7cc4-bd8c-f4d9030ca426'
    nextHistory.display_start = '16:00'
    api.readSchedule.mockImplementation((input: { historyCursor?: string }) =>
      Promise.resolve(
        input.historyCursor
          ? { ...first, replaced_history: [nextHistory], next_history_cursor: null }
          : first,
      ),
    )
    renderSchedule()

    await user.click(await screen.findByRole('button', { name: 'Load more history' }))

    expect(await screen.findByRole('button', { name: /session at 16:00/ })).toBeVisible()
    expect(screen.getAllByRole('button', { name: /session at 14:00/ })).toHaveLength(2)
    expect(screen.getByRole('status')).toHaveTextContent('1 more replaced sessions loaded.')
    expect(api.readSchedule).toHaveBeenCalledWith(
      expect.objectContaining({ historyCursor: 'next-page' }),
      expect.any(AbortSignal),
    )
  })

  it('AC-15 renders loading and then the empty week with an available next action', async () => {
    let resolveSchedule: ((value: Schedule) => void) | undefined
    api.readSchedule.mockReturnValue(
      new Promise<Schedule>((resolve) => {
        resolveSchedule = resolve
      }),
    )
    renderSchedule()

    expect(await screen.findByRole('status', { name: 'Loading schedule' })).toBeVisible()
    const empty = schedule()
    empty.sessions = []
    resolveSchedule?.(empty)

    expect((await screen.findAllByText('No session')).length).toBe(7)
    expect(screen.getByRole('button', { name: 'Add schedule' })).toBeEnabled()
  })
})
