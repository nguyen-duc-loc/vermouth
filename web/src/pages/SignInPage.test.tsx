import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { installMatchMedia } from '../test/setup'
import { SignInPage } from './SignInPage'

const state = vi.hoisted(() => ({
  error: undefined as string | undefined,
  googleEnabled: false,
  language: 'vi' as 'vi' | 'en',
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
}))

describe('SignInPage', () => {
  beforeEach(() => {
    installMatchMedia(true)
    state.error = undefined
    state.googleEnabled = false
    state.language = 'vi'
  })

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
})
