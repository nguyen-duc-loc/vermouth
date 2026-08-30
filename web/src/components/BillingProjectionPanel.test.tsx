import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { BillingProjection } from '../api/teaching'
import { BillingProjectionPanel } from './BillingProjectionPanel'

const waitingProjection: BillingProjection = {
  state: 'waiting',
  class_count: 1,
  session_count: 2,
  student_count: 3,
  open_roster_count: 4,
  attendance_count: 0,
  latest_updated_at: null,
}

// covers: AC-8, AC-9, AC-13, AC-15
describe('BillingProjectionPanel', () => {
  it('announces waiting progress and labels every diagnostic count', () => {
    render(
      <BillingProjectionPanel
        projection={waitingProjection}
        timedOut={false}
        refreshing={false}
        onRetry={vi.fn()}
      />,
    )

    expect(screen.getByRole('status')).toHaveTextContent('Billing projection is catching up.')
    expect(screen.getByText('Catching up')).toBeInTheDocument()
    expect(screen.getByText('Classes').nextElementSibling).toHaveTextContent('1')
    expect(screen.getByText('Sessions').nextElementSibling).toHaveTextContent('2')
    expect(screen.getByText('Students').nextElementSibling).toHaveTextContent('3')
    expect(screen.getByText('Open roster periods').nextElementSibling).toHaveTextContent('4')
    expect(screen.getByText('Attendance rows').nextElementSibling).toHaveTextContent('0')
  })

  it('announces an active projection and exposes its last update as time', () => {
    render(
      <BillingProjectionPanel
        projection={{
          ...waitingProjection,
          state: 'active',
          attendance_count: 1,
          latest_updated_at: '2026-08-30T03:30:00Z',
        }}
        timedOut={false}
        refreshing={false}
        onRetry={vi.fn()}
      />,
    )

    expect(screen.getByRole('status')).toHaveTextContent('Billing projection is active.')
    expect(screen.getByText('Projection active')).toBeInTheDocument()
    expect(screen.getByRole('time')).toHaveAttribute('datetime', '2026-08-30T03:30:00Z')
    expect(screen.queryByRole('button', { name: 'Check billing again' })).not.toBeInTheDocument()
  })

  it('keeps teaching available and offers a named retry when billing is unavailable', async () => {
    const user = userEvent.setup()
    const onRetry = vi.fn()
    render(
      <BillingProjectionPanel
        unavailable="billing transport failed"
        timedOut={false}
        refreshing={false}
        onRetry={onRetry}
      />,
    )

    expect(screen.getByRole('status')).toHaveTextContent(
      'Billing projection is unavailable. Teaching work remains available.',
    )
    expect(screen.getByText('Projection unavailable')).toBeInTheDocument()
    expect(screen.queryByText('billing transport failed')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Check billing again' }))

    expect(onRetry).toHaveBeenCalledOnce()
  })

  it('explains the ten second timeout without presenting it as a teaching failure', () => {
    render(
      <BillingProjectionPanel
        projection={waitingProjection}
        timedOut
        refreshing
        onRetry={vi.fn()}
      />,
    )

    expect(screen.getByRole('status')).toHaveTextContent(
      'Billing projection is still catching up. Manual retry is available.',
    )
    expect(screen.getByText(/Billing is still catching up after ten seconds/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Check billing again/ })).toBeDisabled()
  })
})
