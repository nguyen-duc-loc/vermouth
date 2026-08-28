import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AppearanceProvider } from '../appearance/appearance'
import { TooltipProvider } from '../components/ui/tooltip'
import { installMatchMedia } from '../test/setup'
import { ThreadPage } from './ThreadPage'

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(async () => undefined),
  query: {
    data: undefined as unknown,
    error: null as Error | null,
    refetch: vi.fn(async () => undefined),
  },
  signOut: vi.fn(async () => undefined),
}))

vi.mock('@tanstack/react-query', () => ({
  useQuery: () => mocks.query,
}))

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => mocks.navigate,
  useRouterState: ({
    select,
  }: {
    select: (state: { location: { pathname: string } }) => unknown
  }) => select({ location: { pathname: '/' } }),
}))

vi.mock('../api/session', () => ({ signOut: mocks.signOut }))
vi.mock('../api/thread', () => ({ readThread: vi.fn() }))

function renderPage() {
  return render(
    <AppearanceProvider>
      <TooltipProvider>
        <ThreadPage />
      </TooltipProvider>
    </AppearanceProvider>,
  )
}

describe('ThreadPage foundation states', () => {
  beforeEach(() => {
    installMatchMedia(true)
    document.documentElement.dataset.themeChoice = 'system'
    document.documentElement.dataset.accent = 'blue'
    mocks.query.data = undefined
    mocks.query.error = null
  })

  it('AC-4 announces the loading state without an empty screen', () => {
    renderPage()

    expect(screen.getByRole('status', { name: 'Reading tutor' })).toHaveAttribute(
      'aria-busy',
      'true',
    )
    expect(
      screen.getByText(
        'Not recorded yet. The relay wakes on a timer, so this takes about a second. That wait is the design working.',
      ),
    ).toBeInTheDocument()
  })

  it('AC-4 exposes a blocking read error and its retry action', async () => {
    const user = userEvent.setup()
    mocks.query.error = new Error('gateway unavailable')
    renderPage()

    expect(screen.getByRole('alert')).toHaveTextContent('gateway unavailable')
    await user.click(screen.getByRole('button', { name: 'Try the read again' }))
    expect(mocks.query.refetch).toHaveBeenCalledOnce()
  })

  it('renders authoritative and projected facts with their ownership labels', () => {
    mocks.query.data = {
      tutor: {
        tutor_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
        email: 'tutor@example.com',
        display_name: 'Nguyễn Minh Anh',
        timezone: 'Asia/Ho_Chi_Minh',
        language: 'vi',
        created_at: '2026-08-27T00:00:00Z',
      },
      projection: {
        tutor_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
        recorded: true,
        recorded_at: '2026-08-27T00:00:01Z',
      },
    }
    renderPage()

    expect(screen.getAllByText('Authoritative')).not.toHaveLength(0)
    expect(screen.getAllByText('Recorded')).not.toHaveLength(0)
    expect(screen.getByText('Nguyễn Minh Anh')).toBeInTheDocument()
    expect(screen.getByText(/2026-08-27T00:00:01Z/)).toBeInTheDocument()
  })

  it('ends the session from the account panel before navigating to sign in', async () => {
    const user = userEvent.setup()
    renderPage()

    const accountTriggers = screen.getAllByRole('button', { name: 'Account and appearance' })
    await user.click(accountTriggers.at(-1) as HTMLElement)
    await user.click(screen.getByRole('button', { name: 'Sign out' }))

    expect(mocks.signOut).toHaveBeenCalledOnce()
    expect(mocks.navigate).toHaveBeenCalledWith({ to: '/signin' })
  })
})
