import { beforeEach, describe, expect, it, vi } from 'vitest'

const boundary = vi.hoisted(() => ({
  get: vi.fn(),
  authHeaders: vi.fn(() => ({ Authorization: 'Bearer access-token' })),
  withProtectedRetry: vi.fn(async (request: () => Promise<unknown>) => request()),
}))

vi.mock('./client', () => ({
  api: { GET: boundary.get },
  authHeaders: boundary.authHeaders,
}))

vi.mock('./session', () => ({
  withProtectedRetry: boundary.withProtectedRetry,
}))

beforeEach(() => {
  boundary.get.mockReset()
  boundary.authHeaders.mockClear()
  boundary.withProtectedRetry.mockClear()
})

describe('readThread', () => {
  // covers: AC-3, AC-4
  it('returns the protected thread with the in memory bearer token', async () => {
    const thread = { status: 'ready' }
    boundary.get.mockResolvedValue({ data: thread, response: new Response(null, { status: 200 }) })
    const { readThread } = await import('./thread')

    await expect(readThread()).resolves.toEqual(thread)

    expect(boundary.withProtectedRetry).toHaveBeenCalledOnce()
    expect(boundary.get).toHaveBeenCalledWith('/api/thread', {
      headers: { Authorization: 'Bearer access-token' },
    })
  })

  // covers: AC-16
  it('uses the API error message when the gateway refuses the request', async () => {
    boundary.get.mockResolvedValue({
      error: { error: { code: 'unauthenticated', message: 'this session is over, sign in again' } },
      response: new Response(null, { status: 401 }),
    })
    const { readThread } = await import('./thread')

    await expect(readThread()).rejects.toThrow('this session is over, sign in again')
  })

  // covers: AC-16
  it('uses a safe fallback when the gateway response has no shaped error', async () => {
    boundary.get.mockResolvedValue({ response: new Response(null, { status: 500 }) })
    const { readThread } = await import('./thread')

    await expect(readThread()).rejects.toThrow('the gateway could not read the thread')
  })
})
