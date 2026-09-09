import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const state = vi.hoisted(() => ({
  message: undefined as string | undefined,
  navigate: vi.fn(),
  retry: vi.fn(async () => undefined),
  status: 'checking' as 'checking' | 'authenticated' | 'anonymous' | 'unavailable',
}))

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => state.navigate,
}))

vi.mock('../api/session', () => ({
  sessionCoordinator: { retry: state.retry },
  useSession: () => ({ status: state.status, message: state.message }),
}))

vi.mock('./HomePage', () => ({
  HomePage: () => <main>Canonical teaching home</main>,
}))

import { ProtectedHomePage } from './SessionStatePage'

beforeEach(() => {
  state.status = 'checking'
  state.message = undefined
  state.navigate.mockReset()
  state.retry.mockClear()
})

// covers: AC-1, AC-6, AC-7, AC-13
describe('ProtectedHomePage', () => {
  it('announces session checking before protected teaching content opens', () => {
    render(<ProtectedHomePage />)

    expect(screen.getByRole('status')).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByRole('heading', { name: 'Checking your session' })).toBeInTheDocument()
    expect(screen.queryByText('Canonical teaching home')).not.toBeInTheDocument()
    expect(document.title).toBe('Checking session · Vermouth')
  })

  it('offers one named retry when the session service is unavailable', async () => {
    const user = userEvent.setup()
    state.status = 'unavailable'
    state.message = 'The session service did not answer.'
    render(<ProtectedHomePage />)

    expect(
      screen.getByRole('heading', { name: 'Your session needs another try.' }),
    ).toBeInTheDocument()
    expect(screen.getByText('The session service did not answer.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(state.retry).toHaveBeenCalledOnce()
    expect(document.title).toBe('Session unavailable · Vermouth')
  })

  it('renders the canonical home only after authentication', async () => {
    state.status = 'authenticated'
    render(<ProtectedHomePage />)

    expect(await screen.findByText('Canonical teaching home')).toBeInTheDocument()
    expect(screen.queryByText('Checking your session')).not.toBeInTheDocument()
  })

  it('redirects an anonymous session without flashing protected content', () => {
    state.status = 'anonymous'
    render(<ProtectedHomePage />)

    expect(state.navigate).toHaveBeenCalledWith({ to: '/signin', search: { redirect: '/' } })
    expect(screen.queryByText('Canonical teaching home')).not.toBeInTheDocument()
  })
})
