import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ComponentProps } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  clearDrafts: vi.fn(),
  navigate: vi.fn(async () => undefined),
  readProfile: vi.fn(),
  readTutor: vi.fn(),
  signOut: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  Link: ({ to, children, ...props }: ComponentProps<'a'> & { to: string }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
  useNavigate: () => mocks.navigate,
}))
vi.mock('../api/profile', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/profile')>()
  return { ...actual, readInvoiceProfile: mocks.readProfile }
})
vi.mock('../api/teaching', () => ({ readTutor: mocks.readTutor }))
vi.mock('../api/session', () => ({ signOut: mocks.signOut }))
vi.mock('../lib/teaching-draft', () => ({ clearAllTeachingDrafts: mocks.clearDrafts }))
vi.mock('./AppearancePanel', () => ({
  AppearancePanel: () => <section>Appearance controls</section>,
}))

import { AccountPanel } from './AccountPanel'

beforeEach(() => {
  mocks.clearDrafts.mockClear()
  mocks.navigate.mockClear()
  mocks.readProfile.mockReset()
  mocks.readTutor.mockReset()
  mocks.signOut.mockReset()
  mocks.readTutor.mockResolvedValue({ tutor_id: 'tutor-1' })
})

describe('AccountPanel', () => {
  // covers: AC-1, AC-9
  it('shows only the incomplete cue and never exposes private bank values', async () => {
    mocks.readProfile.mockResolvedValue({
      is_complete: false,
      bank_account_number: 'PRIVATE123',
    })
    renderPanel()

    expect(await screen.findByRole('link', { name: /Profile and bank details/ })).toHaveAttribute(
      'href',
      '/profile',
    )
    expect(await screen.findByText('Incomplete')).toBeInTheDocument()
    expect(screen.queryByText('PRIVATE123')).not.toBeInTheDocument()
  })

  // covers: AC-14
  it('clears private client state before completing user initiated sign out', async () => {
    const user = userEvent.setup()
    mocks.readProfile.mockResolvedValue({ is_complete: true })
    mocks.signOut.mockResolvedValue({ status: 'anonymous' })
    const queryClient = renderPanel()
    queryClient.setQueryData(['invoice-profile', 'tutor-1'], { is_complete: true })
    queryClient.setQueryData(['banks', 'tutor-1'], { banks: [] })

    await user.click(await screen.findByRole('button', { name: 'Sign out' }))

    expect(mocks.clearDrafts).toHaveBeenCalledOnce()
    expect(queryClient.getQueryData(['invoice-profile', 'tutor-1'])).toBeUndefined()
    expect(queryClient.getQueryData(['banks', 'tutor-1'])).toBeUndefined()
    expect(mocks.signOut).toHaveBeenCalledOnce()
    expect(mocks.navigate).toHaveBeenCalledWith({ to: '/signin', search: { redirect: '/' } })
  })
})

function renderPanel() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <AccountPanel />
    </QueryClientProvider>,
  )
  return queryClient
}
