import { beforeEach, describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({
  authHeaders: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  withProtectedRetry: vi.fn(),
}))

vi.mock('./client', () => ({
  api: { GET: client.get, POST: client.post, PUT: client.put },
  authHeaders: client.authHeaders,
}))

vi.mock('./session', () => ({
  withProtectedRetry: client.withProtectedRetry,
}))

beforeEach(() => {
  client.authHeaders.mockReturnValue({ Authorization: 'Bearer access-token' })
  client.withProtectedRetry.mockImplementation((request: () => Promise<unknown>) => request())
})

describe('teaching reads', () => {
  // covers: AC-7, AC-15
  it('reads home with the opaque cursor and protected retry', async () => {
    const signal = new AbortController().signal
    const home = {
      request_time_zone: 'Asia/Ho_Chi_Minh',
      local_date: '2026-08-30',
      next_local_midnight_at: '2026-08-30T17:00:00Z',
      setup_defaults: { local_date: '2026-08-30', start_time: '10:00', end_time: '11:00' },
      sessions: [],
      next_cursor: null,
      tutor: {},
      billing_projection: null,
      billing_projection_unavailable: null,
    }
    client.get.mockResolvedValue({ data: home, response: new Response(null, { status: 200 }) })
    const { readHome } = await import('./teaching')

    await expect(readHome('same day cursor', signal)).resolves.toBe(home)

    expect(client.withProtectedRetry).toHaveBeenCalledOnce()
    expect(client.get).toHaveBeenCalledWith('/api/home', {
      headers: { Authorization: 'Bearer access-token' },
      params: { query: { cursor: 'same day cursor' } },
      signal,
    })
  })

  // covers: AC-9, AC-15
  it('polls only billing projection progress', async () => {
    const progress = {
      billing_projection: null,
      billing_projection_unavailable: 'billing projection is temporarily unavailable',
    }
    client.get.mockResolvedValue({ data: progress, response: new Response(null, { status: 200 }) })
    const { readBillingProjection } = await import('./teaching')

    await expect(readBillingProjection()).resolves.toBe(progress)

    expect(client.get).toHaveBeenCalledOnce()
    expect(client.get).toHaveBeenCalledWith('/api/home/billing-projection', {
      headers: { Authorization: 'Bearer access-token' },
      signal: undefined,
    })
  })

  // covers: AC-9, AC-12
  it('uses the server message and a safe fallback for failed reads', async () => {
    const { readBillingProjection, readHome } = await import('./teaching')
    client.get.mockResolvedValueOnce({
      error: { error: { message: 'billing is catching up' } },
      response: new Response(null, { status: 503 }),
    })
    await expect(readBillingProjection()).rejects.toThrow('billing is catching up')

    client.get.mockResolvedValueOnce({ response: new Response(null, { status: 502 }) })
    await expect(readHome(undefined)).rejects.toThrow('the gateway could not read home')
  })
})

describe('teaching commands', () => {
  // covers: AC-2, AC-4, AC-10
  it('keeps one idempotency key on each create request', async () => {
    const classResult = {
      class: { class_id: 'class-1' },
      first_session: { session_id: 'session-1' },
    }
    const student = { student_id: 'student-1', name: 'Mai', phone: null }
    client.post
      .mockResolvedValueOnce({ data: classResult, response: new Response(null, { status: 201 }) })
      .mockResolvedValueOnce({ data: student, response: new Response(null, { status: 201 }) })
    const { createClass, createStudent } = await import('./teaching')
    const classInput = {
      name: 'Maths 9A',
      rate_amount: 250_000,
      first_session: { local_date: '2026-08-30', start_time: '10:00', end_time: '11:00' },
    }

    await expect(createClass(classInput, 'class-command')).resolves.toBe(classResult)
    await expect(createStudent({ name: 'Mai', phone: null }, 'student-command')).resolves.toBe(
      student,
    )

    expect(client.post).toHaveBeenNthCalledWith(1, '/api/classes', {
      headers: { Authorization: 'Bearer access-token' },
      params: { header: { 'Idempotency-Key': 'class-command' } },
      body: classInput,
    })
    expect(client.post).toHaveBeenNthCalledWith(2, '/api/students', {
      headers: { Authorization: 'Bearer access-token' },
      params: { header: { 'Idempotency-Key': 'student-command' } },
      body: { name: 'Mai', phone: null },
    })
  })

  // covers: AC-4, AC-5, AC-12
  it('puts owned identifiers and idempotency keys in atomic resource paths', async () => {
    const roster = {
      class: { class_id: 'class-1', name: 'Maths', color: 'blue' },
      resolved_date: '2026-08-30',
      students: [],
    }
    const attendance = {
      session_id: 'session-1',
      marked_at: '2026-08-30T03:30:00Z',
      marks: [
        {
          student_id: 'student-1',
          state: 'Present',
          marked_at: '2026-08-30T03:30:00Z',
        },
      ],
    }
    client.put
      .mockResolvedValueOnce({ data: roster, response: new Response(null, { status: 200 }) })
      .mockResolvedValueOnce({ data: attendance, response: new Response(null, { status: 200 }) })
    const { changeClassRoster, saveAttendance } = await import('./teaching')

    await expect(
      changeClassRoster(
        'class-1',
        { change_date: '2026-08-30', additions: ['student-1'], removals: [] },
        'roster-command',
      ),
    ).resolves.toBe(roster)
    await expect(
      saveAttendance(
        'session-1',
        {
          revision: 'revision-1',
          marks: [{ student_id: 'student-1', state: 'Present' }],
        },
        'attendance-command',
      ),
    ).resolves.toBe(attendance)

    expect(client.put).toHaveBeenNthCalledWith(1, '/api/classes/{class_id}/roster', {
      headers: { Authorization: 'Bearer access-token' },
      params: {
        path: { class_id: 'class-1' },
        header: { 'Idempotency-Key': 'roster-command' },
      },
      body: { change_date: '2026-08-30', additions: ['student-1'], removals: [] },
    })
    expect(client.put).toHaveBeenNthCalledWith(2, '/api/sessions/{session_id}/attendance', {
      headers: { Authorization: 'Bearer access-token' },
      params: {
        path: { session_id: 'session-1' },
        header: { 'Idempotency-Key': 'attendance-command' },
      },
      body: {
        revision: 'revision-1',
        marks: [{ student_id: 'student-1', state: 'Present' }],
      },
    })
  })

  // covers: AC-10, AC-12
  it('returns safe command errors without losing a server message', async () => {
    const { createStudent, saveAttendance } = await import('./teaching')
    client.post.mockResolvedValueOnce({
      error: { error: { message: 'the idempotency key names another student' } },
      response: new Response(null, { status: 409 }),
    })
    await expect(createStudent({ name: 'Mai' }, 'student-command')).rejects.toThrow(
      'the idempotency key names another student',
    )

    client.put.mockResolvedValueOnce({ response: new Response(null, { status: 502 }) })
    await expect(
      saveAttendance(
        'session-1',
        { revision: 'revision-1', marks: [{ student_id: 'student-1', state: 'Absent' }] },
        'attendance-command',
      ),
    ).rejects.toThrow('attendance could not be saved')
  })
})
