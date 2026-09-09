import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { installMatchMedia } from '../test/setup'
import { SignInPage } from './SignInPage'

const state = vi.hoisted(() => ({
  error: undefined as string | undefined,
  googleEnabled: false,
  language: 'vi' as 'vi' | 'en',
  rateLimitedUntil: undefined as number | undefined,
  retry: vi.fn(async () => undefined),
}))

vi.mock('@tanstack/react-router', () => ({
  useSearch: () => ({ error: state.error }),
}))

vi.mock('../api/runtime', () => ({
  runtimeConfig: () => ({ googleAuthEnabled: state.googleEnabled }),
}))

vi.mock('../api/session', () => ({
  browserLanguage: () => state.language,
  googleSignInUrl: () => '/api/auth/google/start?redirect_to=%2F',
  sessionCoordinator: { retry: state.retry },
  useSession: () => ({ status: 'anonymous', rateLimitedUntil: state.rateLimitedUntil }),
}))

describe('SignInPage', () => {
  beforeEach(() => {
    installMatchMedia(true)
    state.error = undefined
    state.googleEnabled = false
    state.language = 'vi'
    state.rateLimitedUntil = undefined
    state.retry.mockClear()
  })

  afterEach(() => vi.useRealTimers())

  it('AC-4 explains unavailable auth and keeps the sign in action disabled', () => {
    render(<SignInPage />)

    const button = screen.getByRole('button', { name: 'Continue with Google' })
    expect(button).toBeDisabled()
    expect(button).toHaveAccessibleDescription(
      'Đăng nhập Google chưa được cấu hình cho môi trường cục bộ này. Hãy dùng task dev:token để chạy luồng phát triển.',
    )
  })

  it('renders a known refusal as an alert without exposing unknown search values', () => {
    state.error = 'not_allowed'
    const view = render(<SignInPage />)

    expect(screen.getByRole('alert')).toHaveTextContent('Email này chưa được mời.')

    view.unmount()
    state.error = 'raw provider detail that must not be shown'
    render(<SignInPage />)

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByText(state.error)).not.toBeInTheDocument()
  })

  it('AC-5 renders the Google mark and text as one named link when auth is enabled', () => {
    state.googleEnabled = true
    render(<SignInPage />)

    expect(screen.getByRole('link', { name: 'Continue with Google' })).toHaveAttribute(
      'href',
      '/api/auth/google/start?redirect_to=%2F',
    )
  })

  it('AC-11 renders the fixed rate limit sentence in both supported languages', () => {
    state.error = 'rate_limited'
    state.language = 'en'
    const view = render(<SignInPage />)
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Too many sign in requests. Wait a moment, then try again.',
    )

    view.unmount()
    state.language = 'vi'
    render(<SignInPage />)
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Có quá nhiều yêu cầu đăng nhập. Hãy chờ một lát rồi thử lại.',
    )
  })

  it('AC-12 announces the wait and enables one manual retry without moving focus', async () => {
    const realNow = Date.now()
    vi.useFakeTimers()
    vi.setSystemTime(realNow - 2_000)
    state.language = 'en'
    state.googleEnabled = true
    state.rateLimitedUntil = realNow
    render(<SignInPage />)

    const retry = screen.getByRole('button', { name: 'Try again in 2s' })
    expect(retry).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('Try again in 2s')
    const focusBeforeCountdown = document.activeElement

    await act(() => vi.advanceTimersByTimeAsync(2_000))
    expect(screen.getByRole('button', { name: 'Try again' })).toBeEnabled()
    expect(document.activeElement).toBe(focusBeforeCountdown)
    expect(vi.getTimerCount()).toBe(0)
    vi.useRealTimers()
    const user = userEvent.setup()
    await user.tab()
    await user.tab()
    expect(retry).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(state.retry).toHaveBeenCalledOnce()
  })

  it('AC-12 announces the retry countdown in Vietnamese', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-04T08:00:00Z'))
    state.language = 'vi'
    state.googleEnabled = true
    state.rateLimitedUntil = Date.parse('2026-09-04T08:00:02Z')

    render(<SignInPage />)

    expect(screen.getByRole('button', { name: 'Thử lại sau 2 giây' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('Thử lại sau 2 giây')
  })
})
