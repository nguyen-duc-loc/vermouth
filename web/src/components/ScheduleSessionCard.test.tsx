import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { ScheduleSession } from '../api/teaching'
import { ScheduleSessionCard } from './ScheduleSessionCard'

const session: ScheduleSession = {
  session_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca424',
  class_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca421',
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
  schedule_rule_id: '018f8f7e-91b0-7cc4-bd8c-f4d9030ca422',
  source_time_zone: 'Asia/Ho_Chi_Minh',
  version: 1,
  state: 'active',
  moved_at: null,
  cancelled_at: null,
  superseded_at: null,
  updated_at: '2026-09-19T00:00:00Z',
}

describe('ScheduleSessionCard', () => {
  it('AC-15 passes its focused trigger to the session details flow', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    render(<ScheduleSessionCard session={session} onSelect={onSelect} />)

    const trigger = screen.getByRole('button', {
      name: 'Open Calendar verification session at 14:00',
    })
    trigger.focus()
    await user.keyboard('{Enter}')

    expect(onSelect).toHaveBeenCalledOnce()
    expect(onSelect).toHaveBeenCalledWith(session, trigger)
  })

  it('AC-12 and AC-15 expose the current time, state, and source zone as text', () => {
    render(<ScheduleSessionCard session={session} onSelect={vi.fn()} />)

    expect(screen.getByRole('heading', { name: 'Calendar verification' })).toBeVisible()
    expect(screen.getByText('active')).toBeVisible()
    expect(screen.getByText('14:00')).toHaveAttribute('datetime', session.starts_at)
    expect(screen.getByText('15:00')).toHaveAttribute('datetime', session.ends_at)
    expect(screen.getByText('Weekly rule in Asia/Ho_Chi_Minh')).toBeVisible()
  })
})
